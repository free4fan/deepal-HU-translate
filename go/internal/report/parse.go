package report

import (
	"context"
	"strings"
)

// PerPackage строит таблицу построчной сводки (шаг [6/9] collect_report.bat).
//
// Хрупкие форматы из живых репортов ГУ (22.09):
//   - pm list:  "package:PKG versionCode=N"  (ПРОБЕЛ; граница — хвостовой
//     пробел отличает ...rro.fota от ...rro.fotaservice)
//   - pm list:  "package:PKG=versionCode:N" (новый формат платформы)
//   - без версии: "package:PKG " / "package:PKG"
//   - overlay:  "  [x] PKG (base)" — пакет в середине строки (граница пробел)
//   - overlay:  пакет В КОНЦЕ строки — прежние версии Android. Точки в
//     регексе экранируем (иначе "." = любой символ).
//   - маркеры: "---" (ошибка) имеет приоритет, затем "[x]", затем "[ ]".
func PerPackage(targets []string, prefix, pkgsOut, ovlOut string) []Pkg {
	lines := splitLines(pkgsOut)
	var out []Pkg
	for _, name := range targets {
		pkg := prefix + toLower(name)
		ver, found := pkgVersion(lines, pkg)
		o := overlayState(splitLines(ovlOut), pkg)
		out = append(out, Pkg{
			Name:      name,
			Pkg:       pkg,
			Version:   ver,
			Installed: found,
			Overlay:   o,
		})
	}
	return out
}

// pkgVersion — versionCode пакета; ("?", false) если не найден.
// Ищет "package:PKG " (граница пробел, приоритет — fota/fotaservice),
// затем "package:PKG=" (новый формат), затем "package:PKG" в хвосте.
func pkgVersion(lines []string, pkg string) (string, bool) {
	for _, line := range lines {
		if !strings.Contains(line, "package:"+pkg) {
			continue
		}
		rest := line[strings.Index(line, "package:"+pkg)+len("package:")+len(pkg):]
		switch {
		case strings.HasPrefix(rest, " "):
			// классический: "package:X versionCode:N"
			return versionFromTail(rest[1:]), true
		case strings.HasPrefix(rest, "="):
			// новый: "package:X=versionCode:N"
			return versionFromTail(rest[1:]), true
		case rest == "":
			// версия не показана
			return "?", true
		}
	}
	return "?", false
}

// versionFromTail — "versionCode:5" или "versionCode:5 " -> "5"; нет версии -> "?".
func versionFromTail(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "versionCode:"); i >= 0 {
		s = s[i+len("versionCode:"):]
	}
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return "?"
	}
	return s
}

// overlayState — state оверлея пакета по строкам `cmd overlay list`.
// Возвращает "x" | "space" | "---" | "?".
func overlayState(lines []string, pkg string) string {
	line := findOverlayLine(lines, pkg)
	if line == "" {
		return StateNA
	}
	if strings.Contains(line, "---") {
		return StateErr
	}
	if strings.Contains(line, "[x]") {
		return StateOn
	}
	if strings.Contains(line, "[ ]") {
		return StateOff
	}
	return StateNA
}

// findOverlayLine — строка с пакетом.
//   - сначала: "PKG " (хвостовой пробел) — новый формат "  [x] PKG (base)";
//   - затем: пакет в конце строки — прежние версии Android.
func findOverlayLine(lines []string, pkg string) string {
	for _, l := range lines {
		if strings.Contains(l, pkg+" ") {
			return l
		}
	}
	for _, l := range lines {
		t := strings.TrimRight(l, " \t")
		if strings.HasSuffix(t, pkg) {
			return l
		}
	}
	return ""
}

// ErrorPkgs — пакеты с state --- (для dump в шаге [8/9]).
func ErrorPkgs(pkgs []Pkg) []string {
	var out []string
	for _, p := range pkgs {
		if p.Overlay == StateErr {
			out = append(out, p.Pkg)
		}
	}
	return out
}

// CountFatal — количество строк с "FATAL EXCEPTION" (по строкам, не find /C —
// тот выводил локализованное "НАЙДЕНО" и токен рвался; см. REM collect_report.bat).
func CountFatal(out string) int {
	n := 0
	for _, l := range splitLines(out) {
		if strings.Contains(l, "FATAL EXCEPTION") {
			n++
		}
	}
	return n
}

// FilterLines — строки, содержащие хотя бы один из needle (регистронезависимо),
// как findstr /I /C:....
func FilterLines(out string, needles []string) string {
	var b strings.Builder
	for _, l := range splitLines(out) {
		ll := strings.ToLower(l)
		for _, n := range needles {
			if n == "" {
				continue
			}
			if strings.Contains(ll, strings.ToLower(n)) {
				b.WriteString(l + "\n")
				break
			}
		}
	}
	return b.String()
}

// FindProps — строки getprop по ключам (model / release / fingerprint).
// Возвращает ровно len(keys) строк; не найденное → "?".
func FindProps(devProps string, keys ...string) []string {
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		if line := findProp(devProps, k); line == "" {
			out = append(out, "?")
		} else {
			out = append(out, line)
		}
	}
	return out
}

func findProp(devProps, key string) string {
	for _, l := range splitLines(devProps) {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, key+"=") {
			return t
		}
	}
	return ""
}

// AnrReport — /data/anr: ls, и при наличности — adb root + head -150 последних 3.
// Возвращает (текст файла 42_anr.txt, число ANR-файлов).
func AnrReport(ctx context.Context, in Input) (string, int) {
	var b strings.Builder
	ls := in.Adb.ShellOut(ctx, "ls", "-l", "/data/anr").Out
	b.WriteString(ls)
	n := countAnr(in, ctx, ls)
	if n == 0 {
		b.WriteString("\n  /data/anr пусто или недоступен - пробую adb root...\n")
		in.Adb.Root(ctx)
		ls2 := in.Adb.ShellOut(ctx, "ls", "-l", "/data/anr").Out
		b.WriteString(ls2)
		n = countAnr(in, ctx, ls2)
	}
	if n > 0 {
		b.WriteString("\n==== содержимое последних 3 ANR (head -150), adb root запрошен ====\n")
		in.Adb.Root(ctx)
		ls3 := in.Adb.ShellOut(ctx, "ls", "-t", "/data/anr").Out
		files := splitLines(ls3)
		if len(files) > 3 {
			files = files[:3]
		}
		for _, f := range files {
			f = strings.TrimSpace(f)
			if f == "" {
				continue
			}
			b.WriteString("---- " + f + " ----\n")
			h := in.Adb.ShellOut(ctx, "head", "-150", "/data/anr/"+f).Out
			if h == "" {
				b.WriteString("   [не читается: Permission denied?]\n")
			} else {
				b.WriteString(h + "\n")
			}
		}
	}
	return b.String(), n
}

// countAnr — число непустых строк "ls /data/anr".
func countAnr(in Input, ctx context.Context, ls string) int {
	ns := splitLines(ls)
	n := 0
	for _, l := range ns {
		l = strings.TrimSpace(l)
		if l == "" || strings.Contains(l, "cannot access") || strings.Contains(l, "No such file") {
			continue
		}
		n++
	}
	return n
}

func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimRight(l, "\r") != "" {
			out = append(out, strings.TrimRight(l, "\r"))
		}
	}
	return out
}

func toLower(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += ('a' - 'A')
		}
		b.WriteByte(c)
	}
	return b.String()
}
