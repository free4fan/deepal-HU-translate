// Package report — сборщик диагностического пакета (zip) для анализа,
// 1:1 аналог collect_report.bat. ТОЛЬКО ЧТЕНИЕ с устройства; ничего не
// меняет, кроме попыток `adb root` (для /vendor/overlay и /data/anr).
//
// Выход:
//
//	logs/report_<TS>/
//	  summary.txt          — читать первым
//	  ops_*.log            — свежие логи операций (макс. 10)
//	  raw/00_device.txt    — getprop + uptime + df
//	  raw/01_adb_host.txt  — timestamp + adb version
//	  raw/02_apk_local.txt — APK на хосте (что планировалось ставить)
//	  raw/10_locale.txt    — локаль
//	  raw/20_packages_ours.txt
//	  raw/25_overlay_all.txt
//	  raw/26_vendor_overlay.txt   (только static)
//	  raw/30_per_package.txt
//	  raw/40_crash.txt  raw/41_fatal.txt  raw/42_anr.txt
//	  raw/43_main.txt   raw/44_overlay_main.txt
//	  raw/45_overlay_dump.txt     raw/46_idmap.txt
//	logs/deepl_<scheme>_report_<TS>.zip
package report

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"deepal-hu-translate/internal/groups"
	"deepal-hu-translate/internal/opslog"
)

type Input struct {
	Cfg     *groups.Config // схема (dynamic/static): префиксы, фильтры, группы
	Adb     adbIf
	Now     time.Time
	LogsDir string
	Out     io.Writer // прогресс в консоль; nil = молча
}

// Pkg — строка таблицы 30_per_package.txt.
type Pkg struct {
	Name      string
	Pkg       string
	Version   string // versionCode или "?"
	Installed bool
	Overlay   string // StateOn | StateOff | StateErr | StateNA
}

// Stats — счётчики для summary.
type Stats struct {
	Total      int
	Abs        int // установлено
	Miss       int // отсутствует
	On         int // [x]
	Off        int // [ ]
	Err        int // ---
	NA         int // не в списке
	Fatal      int
	Anr        int
	VendorDirs int
}

// Overlay-состояния (значения поля Pkg.Overlay).
const (
	StateOn  = "x"     // [x]
	StateOff = "space" // [ ]
	StateErr = "---"
	StateNA  = "?"
)

// Run — полный прогон всех 9 шагов + zip.
// Возвращает (путь к zip, путь к summary.txt, счётчики, error).
func Run(ctx context.Context, in Input) (string, string, Stats, error) {
	out := in.Out
	if out == nil {
		out = io.Discard
	}
	var stats Stats
	ts := in.Now.Format("2006-01-02_150405")
	rdir := filepath.Join(in.LogsDir, "report_"+ts)
	raw := filepath.Join(rdir, "raw")
	if err := os.MkdirAll(raw, 0o755); err != nil {
		return "", "", stats, err
	}
	fmt.Fprintf(out, "%s\n", strings.Repeat("=", 60))
	fmt.Fprintf(out, "  Deepal HU Translate - Report (%s)\n", in.Cfg.Scheme)
	fmt.Fprintf(out, "  Каталог: %s\n", rdir)
	fmt.Fprintf(out, "%s\n\n", strings.Repeat("=", 60))

	// ---------- [1/9] устройство ----------
	fmt.Fprintln(out, "[1/9] Сведения об устройстве...")
	var dev strings.Builder
	dev.WriteString(in.Adb.ShellOut(ctx, "getprop").Out + "\n")
	dev.WriteString("\n---- uptime ----\n")
	dev.WriteString(in.Adb.ShellOut(ctx, "uptime").Out + "\n")
	dev.WriteString("\n---- df -h ----\n")
	dev.WriteString(in.Adb.ShellOut(ctx, "df", "-h").Out + "\n")
	if err := os.WriteFile(filepath.Join(raw, "00_device.txt"), []byte(dev.String()), 0o644); err != nil {
		return "", "", stats, err
	}
	host := fmt.Sprintf("==== scheme: %s ====\n==== timestamp: %s ====\n\n==== adb version - хост ====\n%s\n",
		in.Cfg.Scheme, ts, in.Adb.Version(ctx).Out)
	writeFile(filepath.Join(raw, "01_adb_host.txt"), host)

	// ---------- [2/9] APK на хосте ----------
	fmt.Fprintf(out, "[2/9] APK-файлы на хосте: %s ...\n", in.Cfg.ApkDir)
	writeFile(filepath.Join(raw, "02_apk_local.txt"), listAPKs(in.Cfg.ApkDir))

	// ---------- [3/9] локаль ----------
	fmt.Fprintln(out, "[3/9] Локаль системы...")
	var loc strings.Builder
	loc.WriteString("---- system_locales ----\n")
	loc.WriteString(in.Adb.ShellOut(ctx, "settings", "get", "system", "system_locales").Out + "\n")
	loc.WriteString("---- ro.product.locale ----\n")
	loc.WriteString(in.Adb.ShellOut(ctx, "getprop ro.product.locale").Out + "\n")
	loc.WriteString("---- persist.sys.locale ----\n")
	loc.WriteString(in.Adb.ShellOut(ctx, "getprop persist.sys.locale").Out + "\n")
	writeFile(filepath.Join(raw, "10_locale.txt"), loc.String())

	// ---------- [4/9] pm list + overlay list ----------
	fmt.Fprintln(out, "[4/9] Установленные пакеты + list оверлеев...")
	pkgsOut := in.Adb.ShellOut(ctx, "pm", "list", "packages", "--show-versioncode", in.Cfg.Filter).Out
	ovlOut := in.Adb.ShellOut(ctx, "cmd", "overlay", "list", "--user", "0").Out
	writeFile(filepath.Join(raw, "20_packages_ours.txt"), pkgsOut)
	writeFile(filepath.Join(raw, "25_overlay_all.txt"), ovlOut)

	// ---------- [5/9] /vendor/overlay (только static) ----------
	if in.Cfg.Scheme == groups.SchemeStatic {
		fmt.Fprintln(out, "[5/9] Static overlays on device (will ask adb root if needed)...")
		stats.VendorDirs = vendorOverlay(ctx, in, raw)
	} else {
		fmt.Fprintln(out, "[5/9] Динамические - /vendor/overlay не используется")
	}

	// ---------- [6/9] построчная сводка ----------
	fmt.Fprintln(out, "[6/9] Построчная сводка по целевым пакетам...")
	pkgs := PerPackage(in.Cfg.All(), in.Cfg.Prefix, pkgsOut, ovlOut)
	for i := range pkgs {
		stats.Total++
		if pkgs[i].Installed {
			stats.Abs++
		} else {
			stats.Miss++
		}
		switch pkgs[i].Overlay {
		case StateOn:
			stats.On++
		case StateOff:
			stats.Off++
		case StateErr:
			stats.Err++
		default:
			stats.NA++
		}
	}
	writeFile(filepath.Join(raw, "30_per_package.txt"), FormatPerPackage(in.Cfg, pkgs))

	// ---------- [7/9] ошибки: crash, FATAL, ANR ----------
	fmt.Fprintln(out, "[7/9] Логи ошибок: crash buffer, FATAL, /data/anr...")
	crash := in.Adb.ShellOut(ctx, "logcat", "-d", "-b", "crash", "-t", "500", "-v", "time").Out
	fatal := in.Adb.ShellOut(ctx, "logcat", "-d", "-s", "AndroidRuntime:E", "System.err:W", "*:S", "-v", "time").Out
	writeFile(filepath.Join(raw, "40_crash.txt"), crash)
	writeFile(filepath.Join(raw, "41_fatal.txt"), fatal)
	stats.Fatal = CountFatal(fatal)

	anrOut, anrCount := AnrReport(ctx, in)
	stats.Anr = anrCount
	writeFile(filepath.Join(raw, "42_anr.txt"), anrOut)

	// ---------- [8/9] main-лог + dump ошибочных оверлеев ----------
	fmt.Fprintln(out, "[8/9] Логи работы APK: logcat main + dump оверлеев в ошибке...")
	main := in.Adb.ShellOut(ctx, "logcat", "-d", "-b", "main", "-t", "2000", "-v", "time").Out
	writeFile(filepath.Join(raw, "43_main.txt"), main)
	writeFile(filepath.Join(raw, "44_overlay_main.txt"), FilterLines(main, []string{"overlay", "idmap", in.Cfg.Prefix}))

	var ovlDump, idmp string
	if stats.Err == 0 {
		ovlDump = "Оверлеев в ошибке (---) нет - dump не требуется\n"
		idmp = "Оверлеев в ошибке нет\n"
	} else {
		for _, p := range ErrorPkgs(pkgs) {
			ovlDump += "\n---------- " + p + " ----------\n"
			ovlDump += in.Adb.ShellOut(ctx, "cmd", "overlay", "dump", p).Out + "\n"
		}
		idmp = "==== idmap: logcat IdmapManager/Idmap, полный -d ====\n"
		idmp += in.Adb.ShellOut(ctx, "logcat", "-d", "-s", "IdmapManager:V", "Idmap:V", "-v", "time").Out + "\n"
		idmp += "==== grep целевых пакетов в 43_main ====\n"
		idmp += FilterLines(main, []string{in.Cfg.Prefix, "idmap"})
	}
	writeFile(filepath.Join(raw, "45_overlay_dump.txt"), ovlDump)
	writeFile(filepath.Join(raw, "46_idmap.txt"), idmp)

	// ---------- [9/9] summary + ops-логи ----------
	fmt.Fprintln(out, "[9/9] Формирую summary.txt + логи операций (logs/ops_*.log)...")
	opsFiles := opslog.LastOpsFiles(in.LogsDir, 10)
	for _, of := range opsFiles {
		if data, rerr := os.ReadFile(of); rerr == nil {
			writeFile(filepath.Join(rdir, filepath.Base(of)), string(data))
		}
	}
	fmt.Fprintf(out, "   логи операций (ops_*.log, последние) в отчёте: %d\n", len(opsFiles))

	locLine := strings.TrimSpace(in.Adb.ShellOut(ctx, "settings", "get", "system", "system_locales").Out)
	if locLine == "" || locLine == "null" {
		locLine = "пусто - см. raw/10_locale.txt"
	}
	props := FindProps(dev.String(), "ro.product.model", "ro.build.version.release", "ro.build.fingerprint")
	model, rel, fp := props[0], props[1], props[2]
	verdict := Verdict(stats)
	scheme := string(in.Cfg.Scheme)

	summaryPath := filepath.Join(rdir, "summary.txt")
	if err := writeFile(summaryPath, FormatSummary(scheme, ts, model, rel, fp, verdict, locLine, stats)); err != nil {
		return "", "", stats, err
	}
	b, _ := os.ReadFile(summaryPath)
	fmt.Fprintln(out)
	fmt.Fprint(out, string(b))
	fmt.Fprintln(out)

	// ---------- zip ----------
	zipName := fmt.Sprintf("deepl_%s_report_%s.zip", strings.ToLower(string(in.Cfg.Scheme)), ts)
	zipPath := filepath.Join(in.LogsDir, zipName)
	if err := ZipDir(rdir, zipPath); err != nil {
		return zipPath, summaryPath, stats, fmt.Errorf("zip: %w (карточка %s готова)", err, rdir)
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, strings.Repeat("=", 60))
	fmt.Fprintln(out, "  ГОТОВО - файл для отправки:")
	fmt.Fprintf(out, "    %s\n", zipPath)
	fmt.Fprintln(out, strings.Repeat("=", 60))
	return zipPath, summaryPath, stats, nil
}

func writeFile(path, s string) error { return os.WriteFile(path, []byte(s), 0o644) }

// listAPKs — список *_RRO.apk из каталога (новые первыми), как dir /O-D.
func listAPKs(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Sprintf("Каталог %s на хосте не найден\n", dir)
	}
	type fe struct {
		name string
		m    time.Time
		sz   int64
	}
	var files []fe
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, "_RRO.apk") {
			continue
		}
		info, ierr := e.Info()
		if ierr != nil {
			continue
		}
		files = append(files, fe{n, info.ModTime(), info.Size()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].m.After(files[j].m) })
	var b strings.Builder
	for _, f := range files {
		fmt.Fprintf(&b, "%s  %10d  %s\n", f.m.Format("2006-01-02 15:04"), f.sz, f.name)
	}
	if len(files) == 0 {
		b.WriteString("  (нет *_RRO.apk)\n")
	}
	return b.String()
}

// vendorOverlay — шаг [5/9] для static: ls целевых папок в /vendor/overlay
// с одной попыткой adb root при недоступности. Пишет raw/26_vendor_overlay.txt.
func vendorOverlay(ctx context.Context, in Input, raw string) int {
	cfg := in.Cfg
	lsBase := cfg.OverlayBase + "/" + cfg.Prefix + "*"
	count := len(validDirs(in.Adb.ShellOut(ctx, "ls -d", lsBase).Out))
	if count == 0 {
		fmt.Fprintln(in.Out, "   не найдено без root - пробую adb root...")
		in.Adb.Root(ctx)
		count = len(validDirs(in.Adb.ShellOut(ctx, "ls -d", lsBase).Out))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "==== папки: %d ====\n", count)
	b.WriteString(in.Adb.ShellOut(ctx, "ls -d", lsBase).Out + "\n")
	b.WriteString("\n==== содержимое: ls -l ====\n")
	b.WriteString(in.Adb.ShellOut(ctx, "ls -l", lsBase+"/").Out + "\n")
	b.WriteString("\n==== весь /vendor/overlay ====\n")
	b.WriteString(in.Adb.ShellOut(ctx, "ls", "/vendor/overlay").Out + "\n")
	writeFile(filepath.Join(raw, "26_vendor_overlay.txt"), b.String())
	return count
}

// validDirs — непустые строки, без ошибок "ls: cannot access" / "No such file".
func validDirs(out string) []string {
	var r []string
	for _, l := range splitLines(out) {
		t := strings.TrimSpace(l)
		if t == "" || strings.Contains(t, "cannot access") || strings.Contains(t, "No such file") {
			continue
		}
		r = append(r, t)
	}
	return r
}

// FormatPerPackage — 30_per_package.txt (шапка и сводка 1:1 с батником).
func FormatPerPackage(cfg *groups.Config, pkgs []Pkg) string {
	var b strings.Builder
	b.WriteString(strings.Repeat("=", 60) + "\n")
	fmt.Fprintf(&b, "Scheme: %s   Фильтр(pm list): %s\n", cfg.Scheme, cfg.Filter)
	b.WriteString("Формат: package | ver | overlay-state\n")
	b.WriteString(strings.Repeat("=", 60) + "\n")
	on, off, err, na, miss := 0, 0, 0, 0, 0
	for _, p := range pkgs {
		switch p.Overlay {
		case StateOn:
			on++
		case StateOff:
			off++
		case StateErr:
			err++
		default:
			na++
		}
		if p.Installed {
			fmt.Fprintf(&b, "  %s | ver=%s | overlay=%s\n", p.Pkg, p.Version, overlayMark(p.Overlay))
		} else {
			miss++
			fmt.Fprintf(&b, "  %s | НЕТ | overlay=%s <=== НЕ УСТАНОВЛЕН\n", p.Pkg, overlayMark(p.Overlay))
		}
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "Сводка: всего=%d установлено=%d отсутствует=%d\n", len(pkgs), len(pkgs)-miss, miss)
	fmt.Fprintf(&b, "Overlay: [x]=%d [ ]=%d ---=%d в списке нет=%d\n", on, off, err, na)
	return b.String()
}

func overlayMark(s string) string {
	switch s {
	case StateOn:
		return "[x]"
	case StateOff:
		return "[ ]"
	case StateErr:
		return "---"
	default:
		return "?"
	}
}

// FormatSummary — summary.txt (строки 1:1 с collect_report.bat).
func FormatSummary(scheme, ts, model, rel, fp, verdictLine, locale string, s Stats) string {
	var b strings.Builder
	b.WriteString(strings.Repeat("=", 60) + "\n")
	b.WriteString(" Deepal HU Translate - Report (" + scheme + ")\n")
	b.WriteString(" Дата: " + ts + "\n")
	fmt.Fprintf(&b, " Устройство: %s  /  %s\n", model, rel)
	b.WriteString(" " + fp + "\n")
	b.WriteString("\n")
	b.WriteString(" Локаль: " + locale + "\n")
	if scheme == string(groups.SchemeStatic) {
		fmt.Fprintf(&b, " Статических оверлеев в /vendor/overlay: %d\n", s.VendorDirs)
	}
	b.WriteString("\n")
	b.WriteString(" ---- УСТАНОВЛЕНО / НЕ УСТАНОВЛЕНО ----\n")
	fmt.Fprintf(&b, " Всего целевых пакетов:   %d\n", s.Total)
	fmt.Fprintf(&b, " Установлено:             %d\n", s.Abs)
	if s.Miss == 0 {
		b.WriteString(" ОТСУТСТВУЮТ:            0\n")
	} else {
		fmt.Fprintf(&b, " ОТСУТСТВУЮТ:            %d  <=== ПРОБЛЕМА\n", s.Miss)
		if s.Miss == 9 {
			b.WriteString("     (это 9 AOSP-target оверлеев - на этом ГУ они через adb install не ставятся; решение: /vendor/overlay или исключение из групп)\n")
		}
	}
	b.WriteString("\n")
	b.WriteString(" ---- STATE ОВЕРЛЕЕВ ----\n")
	fmt.Fprintf(&b, " Включено [x]:   %d\n", s.On)
	fmt.Fprintf(&b, " Выключено [ ]:  %d\n", s.Off)
	fmt.Fprintf(&b, " Ошибка ---:     %d\n", s.Err)
	fmt.Fprintf(&b, " Не в списке:    %d\n", s.NA)
	b.WriteString("\n")
	b.WriteString(" ---- СБОИ ----\n")
	fmt.Fprintf(&b, " FATAL EXCEPTION в logcat: %d   0 - всё чисто\n", s.Fatal)
	fmt.Fprintf(&b, " ANR-файлов в /data/anr:   %d\n", s.Anr)
	b.WriteString(" Crash buffer: см. raw/40_crash.txt   ANR: raw/42_anr.txt\n")
	b.WriteString("\n")
	b.WriteString(" ---- ЛОГИ ОПЕРАЦИЙ (установка/работа) ----\n")
	b.WriteString(" ops_*.log из logs\\:    см. корень архива\n")
	b.WriteString(" raw/43_main.txt        logcat main -t 2000 (работа APK)\n")
	b.WriteString(" raw/44_overlay_main.txt main: overlay/idmap/our packages\n")
	if s.Err > 0 {
		b.WriteString(" raw/45_overlay_dump.txt dump оверлеев в ошибке\n")
	}
	b.WriteString("\n")
	b.WriteString(" ---- ВЫВОД ----\n")
	b.WriteString(" " + verdictLine + "\n")
	b.WriteString("\n")
	b.WriteString(" ---- СОДЕРЖИМОЕ ----\n")
	b.WriteString(" summary.txt              этот файл\n")
	b.WriteString(" ops_*.log                логи установки/операций (последние)\n")
	b.WriteString(" raw/00_device.txt        getprop + uptime + df\n")
	b.WriteString(" raw/01_adb_host.txt      timestamp + adb version\n")
	b.WriteString(" raw/02_apk_local.txt     APK на хосте, что ставилось\n")
	b.WriteString(" raw/10_locale.txt        локаль\n")
	b.WriteString(" raw/20_packages_ours.txt pm list our packages + versionCode\n")
	b.WriteString(" raw/25_overlay_all.txt   cmd overlay list --user 0 - весь\n")
	if scheme == string(groups.SchemeStatic) {
		b.WriteString(" raw/26_vendor_overlay.txt  /vendor/overlay - ls -l\n")
	}
	b.WriteString(" raw/30_per_package.txt   построчная сводка пакетов\n")
	b.WriteString(" raw/40_crash.txt         logcat -b crash -t 500\n")
	b.WriteString(" raw/41_fatal.txt         AndroidRuntime:E из main\n")
	b.WriteString(" raw/42_anr.txt           /data/anr + trace (root)\n")
	b.WriteString(" raw/43_main.txt          logcat main -t 2000\n")
	b.WriteString(" raw/44_overlay_main.txt  main: overlay/idmap/our packages\n")
	b.WriteString(" raw/45_overlay_dump.txt  cmd overlay dump ошибочных оверлеев\n")
	b.WriteString(" raw/46_idmap.txt         logcat IdmapManager/Idmap (полный)\n")
	b.WriteString(strings.Repeat("=", 60) + "\n")
	return b.String()
}

// Verdict — строка «ВЫВОД» summary. Приоритет: 9AOSP > miss > off > err > FATAL > ANR.
func Verdict(s Stats) string {
	switch {
	case s.Miss == 9:
		return "ОЖИДАЕМО: 9 AOSP-target оверлеев не ставятся через adb install (NetworkStack/Providers/…); остальные установлены"
	case s.Miss > 0:
		return fmt.Sprintf("ПРОБЛЕМА: %d пакет(ов) НЕ УСТАНОВЛЕНО, см. raw/30_per_package.txt", s.Miss)
	case s.Off > 0:
		return fmt.Sprintf("ПРОБЛЕМА: %d оверлей(ей) выключен, см. raw/30_per_package.txt", s.Off)
	case s.Err > 0:
		return fmt.Sprintf("ПРОБЛЕМА: %d оверлей(ей) в ошибке ---, см. raw/45_overlay_dump.txt", s.Err)
	case s.Fatal > 0:
		return fmt.Sprintf("ПРОБЛЕМА: найдено FATAL EXCEPTION: %d, см. raw/40_crash.txt", s.Fatal)
	case s.Anr > 0:
		return fmt.Sprintf("ПРОБЛЕМА: найдено ANR: %d, см. raw/42_anr.txt", s.Anr)
	default:
		return "ВСЕ В ПОРЯДКЕ: все пакеты установлены, оверлеи включены, FATAL-сбоев нет."
	}
}

// ZipDir — каталог (с вложенными) -> zip; заменяет powershell/tar-fallback.
func ZipDir(srcDir, zipPath string) error {
	zf, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer zf.Close()
	zw := zip.NewWriter(zf)
	defer zw.Close()

	abs, err := filepath.Abs(srcDir)
	if err != nil {
		return err
	}
	base := filepath.Base(abs)
	return filepath.WalkDir(abs, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(filepath.Dir(abs), path)
		if rerr != nil {
			return rerr
		}
		name := filepath.Join(base, rel)
		w, werr := zw.Create(name)
		if werr != nil {
			return werr
		}
		f, ferr := os.Open(path)
		if ferr != nil {
			return ferr
		}
		defer f.Close()
		_, cerr := io.Copy(w, f)
		return cerr
	})
}
