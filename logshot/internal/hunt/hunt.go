// Package hunt — «охотник» за фразой в снимке logcat: парсинг строк
// threadtime, поиск (подстрока, CJK-безразлично к регистру), атрибуция
// PID → приложение (через /proc/<pid>/cmdline с фолбэком `pm map <pid>`),
// человеческий отчёт.
package hunt

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ShellFn — одна `adb shell sh -lc <cmd>` (для атрибции pids),
// тип вынесен сюда, чтобы пакет был чистым (без зависимостей) и
// тестировался без подмены adb-объекта.
type ShellFn func(shCmd string) string

// Line — распознанная строка logcat (+ наш host-штамп, если есть).
type Line struct {
	Raw    string // полная строка из снимка (с host-префиксом)
	Day    string // "10-02"
	Time   string // "21:35:12.099"
	PID    int
	TID    int
	Level  string // V/D/I/W/E/F
	Tag    string
	Msg    string
	HostTS string // "2026-10-02 21:35:12.001" (наш штамп)
	OK     bool   // распознан ли threadtime
}

var reThreadtime = regexp.MustCompile(
	`(?:(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\.\d{3}) )?` + // наш host-штамп (опц.)
		`(?:\[([a-z]+)\] )?` + // метка буфера (опц.)
		`(\d{2}-\d{2}) (\d{2}:\d{2}:\d{2}\.\d{3})\s+(\d+)\s+(\d+)\s+([VDIWEF])\s+(?:([^\s:]+): )?`)

// ParseLine — распарсивает одну строку снимка.
func ParseLine(raw string) Line {
	l := Line{Raw: raw, OK: false}
	m := reThreadtime.FindStringSubmatch(raw)
	if m == nil {
		// возможно, метка буфера в кавычках пустая — пробуем без groupId:
		return l
	}
	l.HostTS = m[1]
	l.Day = m[3]
	l.Time = m[4]
	pid, _ := strconv.Atoi(m[5])
	tid, _ := strconv.Atoi(m[6])
	l.PID, l.TID = pid, tid
	l.Level = m[7]
	l.Tag = m[8]
	l.Msg = raw[len(m[0]):]
	l.OK = true
	return l
}

// Hit — одно совпадение фразы.
type Hit struct {
	Phrase  string
	Idx     int    // позиция в снимке (по строке)
	Line    Line   // распознанная строка
	HostTS  string // штамп снимка из строки
	Pkg     string // результат атрибуции (пакет/имя процесса/«не определено»)
	Context []string
}

func (h Hit) Full() string {
	return h.Line.Raw
}

// Report — итог охоты по списку фраз.
type Report struct {
	Phrases    []string
	Hits       []Hit
	TotalLines int
	Pids       map[int]string // pid -> имя (атрибуция всех pids снимка)
}

// Search — ищет все фразы в строках снимка, возвращает отчёт.
func Search(lines []string, phrases []string) Report {
	rep := Report{
		Phrases:    phrases,
		TotalLines: len(lines),
		Pids:       map[int]string{},
	}
	for i, raw := range lines {
		l := ParseLine(raw)
		if l.PID > 0 {
			rep.Pids[l.PID] = "" // запомним pids (заполним attribuциею снаружи)
		}
		for _, p := range phrases {
			if p == "" {
				continue
			}
			if strings.Contains(raw, p) {
				ctx := contextLines(lines, i, 3)
				rep.Hits = append(rep.Hits, Hit{
					Phrase:  p,
					Idx:     i,
					Line:    l,
					HostTS:  l.HostTS,
					Context: ctx,
				})
			}
		}
	}
	return rep
}

// contextLines — окно вокруг строки i: от i-n до i+n (включительно),
// 2n+1 строка (у краёв снимка меньше).
func contextLines(lines []string, i, n int) []string {
	lo := i - n
	if lo < 0 {
		lo = 0
	}
	hi := i + n + 1
	if hi > len(lines) {
		hi = len(lines)
	}
	out := make([]string, 0, hi-lo)
	for k := lo; k < hi; k++ {
		out = append(out, lines[k])
	}
	return out
}

// UniquePids — отсортированный список pids снимка.
func (r Report) UniquePids() []int {
	out := make([]int, 0, len(r.Pids))
	for p := range r.Pids {
		out = append(out, p)
	}
	sort.Ints(out)
	return out
}

// AttrPids — подмешивает результаты атрибуции (pid -> имя) в from report:
// не заменяет карту, а дополняет её (pids без доступа остаются
// «не атрибутирован»).
func (r Report) AttrPids(attr map[int]string) {
	if r.Pids == nil {
		r.Pids = map[int]string{}
	}
	for k, v := range attr {
		r.Pids[k] = v
	}
}

// ByPhrase — хиты одной фразы (порядок снимка).
func (r Report) ByPhrase(p string) []Hit {
	var out []Hit
	for _, h := range r.Hits {
		if h.Phrase == p {
			out = append(out, h)
		}
	}
	return out
}

// AttrPID — атрибутуёт пид шелл-командами:
//  1. /proc/<pid>/cmdline — имя процесса (для zygote-приложений там
//     прямиком пакет, для system/native — имя исполняемого файла);
//  2. если пусто — `pm map <pid>` (пакеты, мапится uid-у пользователя).
//
// Кэш — пиды не спрашиваем повторно.
func AttrPID(sh ShellFn, pids []int) map[int]string {
	res := map[int]string{}
	for _, p := range pids {
		if p <= 0 {
			continue
		}
		name := strings.TrimSpace(sh(fmt.Sprintf("tr '\\0' ' ' < /proc/%d/cmdline 2>/dev/null", p)))
		name = strings.TrimRight(name, " ")
		if name == "" {
			pm := strings.TrimSpace(sh(fmt.Sprintf("pm map %d 2>/dev/null", p)))
			pkgs := packagesFromMap(pm)
			if len(pkgs) > 0 {
				name = strings.Join(pkgs, ",")
			} else {
				name = "не определён: " + strconv.Itoa(p) + " (нет доступа к /proc или процесс живёт в другом domain)"
			}
		}
		res[p] = name
	}
	return res
}

// packagesFromMap — парсит вывод `pm map`:
//
//	User 0: pid1234
//	com.android.systemui,com.tinnove.camera
//
// (пакеты — строкой после «User N: pid...»; поддерживаем несколько
// пидов в одной строке "pid1,pid2").
func packagesFromMap(out string) []string {
	var pkgs []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "User ") {
			continue
		}
		for _, p := range strings.Split(line, ",") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			pkgs = append(pkgs, p)
		}
	}
	return pkgs
}

// IsPackage — похоже ли имя на Android-пакет (ком.точка.имя).
func IsPackage(s string) bool {
	if !strings.Contains(s, ".") || strings.ContainsAny(s, " \t/") {
		return false
	}
	return len(s) > 4
}

// Format — человек-читаемый отчёт (hit + PID-карта).
func (r Report) Format(label string) string {
	var b strings.Builder
	b.WriteString("==== logshot hits ====\n")
	b.WriteString("label: " + label + "\n")
	b.WriteString("строк в снимке: " + strconv.Itoa(r.TotalLines) + "\n\n")
	byP := map[string]int{}
	for _, h := range r.Hits {
		byP[h.Phrase]++
	}
	for _, p := range r.Phrases {
		n := byP[p]
		b.WriteString("== «" + p + "» — совпадений: " + strconv.Itoa(n) + " ==\n")
		if n == 0 {
			b.WriteString("  (не найдено в снимке)\n")
			continue
		}
		hits := r.ByPhrase(p)
		for k, h := range hits {
			b.WriteString("\n[" + strconv.Itoa(k+1) + "]")
			if h.HostTS != "" {
				b.WriteString(" host=" + h.HostTS)
			}
			b.WriteString("\n    orig: " + h.Line.Raw + "\n")
			if h.Line.OK {
				b.WriteString("    pid=" + strconv.Itoa(h.Line.PID) + " tid=" + strconv.Itoa(h.Line.TID) +
					" lvl=" + h.Line.Level + " tag=" + h.Line.Tag)
				if h.Pkg != "" {
					b.WriteString(" -> " + h.Pkg)
				}
				b.WriteString("\n")
			}
			if len(h.Context) > 0 {
				mark := 0
				for k2, c := range h.Context {
					_ = k2
					if c == h.Line.Raw {
						mark = k2
					}
				}
				b.WriteString("    контекст:\n")
				for k2, c := range h.Context {
					arrow := "    "
					if k2 == mark {
						arrow = "  >>"
					}
					b.WriteString(arrow + " " + c + "\n")
				}
			}
		}
	}
	b.WriteString("\n== PID-карта (уникальные pids снимка) ==\n")
	for _, p := range r.UniquePids() {
		name := r.Pids[p]
		if name == "" {
			name = "(не атрибутирован)"
		}
		b.WriteString("  " + strconv.Itoa(p) + "  " + name + "\n")
	}
	return b.String()
}
