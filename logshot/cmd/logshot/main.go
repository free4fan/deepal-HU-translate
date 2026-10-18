// Command logshot — снимок живого logcat ГУ + автоматическая «охота»
// за фразой (подстрокой) с атрибуцией PID → приложение.
//
// Зачем: фразы, которых нет в APK (ресурсы/dex/assets — проверено
// сканом), приходят runtime (шина автомобиля, TBOX, scene.db, TTS-движок).
// logshot ловит лог в момент показа фразы и показывает, кто её напечатал:
// PID из строки logcat + /proc/<pid>/cmdline + `pm map <pid>` → пакет.
//
// Использование:
//
//	logshot                          — снимаёт 120 с, поиск без фраз
//	logshot -q "请检查四周情况"        — снимок + охота за фразой
//	logshot -q "主驾离车提醒" -d 180   — 180 с (показать/погасить экран)
//	logshot -q A -q B                 — несколько фраз
//	logshot -cjk                       — снимок + ВСЕ китайские фрагменты
//	logshot -q "фраза" -cjk            — и фразы, и всё CJK
//	logshot -dir other/logshot        — другая папка снимков
//	logshot -offline <снимок.log> -q "фраза"  — охота по уже снятому
//	logshot -offline <снимок.log> -cjk         — весь CJK из старого снимка
//
// Снимки: <dir>/shot_<ts>.log, .meta.txt, .hits.txt.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"logshot/internal/adb"
	"logshot/internal/hunt"
	"logshot/internal/shot"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("logshot", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	dur := fs.Duration("d", 120*time.Second, "длительность снятия (сек; -1 = до Ctrl+C)")
	var phrases multiFlag
	fs.Var(&phrases, "q", "фраза для охоты (можно несколько)")
	cjk := fs.Bool("cjk", false,
		"охота за ВСЕМИ китайскими фрагментами (неизвестные заранее; "+
			"отдельно от -q, можно вместе)")
	dirP := fs.String("dir", "logs/logshot", "папка снимков (относительно CWD)")
	serialP := fs.String("s", "", "serial устройства (пусто — единственный)")
	bufrs := fs.String("bufrs", "main,system,crash", "буферы logcat через запятую")
	offline := fs.String("offline", "", "режим offline: охота по уже снятому .log")
	help := fs.Bool("h", false, "помощь")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *help {
		usage()
		return nil
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *offline != "" {
		if len(phrases) == 0 && !*cjk {
			usage()
			return fmt.Errorf("режим -offline требует -q (фразы) или -cjk (всё CJK)")
		}
		return offlineHunt(*offline, phrases, *cjk)
	}

	a := adb.New()
	if !deviceOK(a, ctx, *serialP) {
		return fmt.Errorf("устройство недоступно (adb devices)")
	}
	ts := time.Now().Format("20060102_150405")
	dir := *dirP
	os.MkdirAll(dir, 0o755)
	logF := filepath.Join(dir, "shot_"+ts+".log")
	metaF := logF + ".meta.txt"
	hitsF := logF + ".hits.txt"

	bufr := strings.Split(*bufrs, ",")
	var clean []string
	for _, b := range bufr {
		if b = strings.TrimSpace(b); b != "" {
			clean = append(clean, b)
		}
	}

	fmt.Printf("Снятие logcat: %s\n", logF)
	fmt.Printf("Длительность: %s (Ctrl+C — досрочно)\n", dur)
	if len(phrases) > 0 {
		fmt.Printf("Фразы: %s\n", strings.Join(phrases, " | "))
	} else {
		fmt.Println("Фразы: (не заданы — снимок без охоты)")
	}
	fmt.Print("Поток запущен. Показывайте сценарий, когда фраза появится на экране...\n")

	lg := shot.New(a, logF, clean, 60000)
	if err := lg.StartFile(ctx); err != nil {
		return err
	}
	if err := lg.WriteMeta(ctx, metaF, "phrases: "+strings.Join(phrases, " | ")); err != nil {
		fmt.Fprintln(os.Stderr, "WARN: meta:", err)
	}

	// таймер / Ctrl+C
	if *dur < 0 {
		<-ctx.Done()
	} else {
		select {
		case <-time.After(*dur):
		case <-ctx.Done():
		}
	}
	lg.Stop()
	fmt.Printf("\nСнято строк: %d\n", lg.Lines())

	lines := loadRing(lg)
	out := assembleReport(lines, phrases, *cjk, attribute(a, ctx, huntPidsToAttr(lines, phrases, *cjk)))
	if err := os.WriteFile(hitsF, []byte(out), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "WARN: hits:", err)
	}
	fmt.Println()
	fmt.Print(out)
	fmt.Println("==== снимок сохранён ====")
	for _, f := range []string{logF, metaF, hitsF} {
		fmt.Println("  " + f)
	}
	return nil
}

// attribute — pids → {pid: имя/пакет} шелл-командами (cap 80, чтобы не
// долбить ГУ).
func attribute(a *adb.Adb, ctx context.Context, pids []int) map[int]string {
	shell := func(cmd string) string { return a.Shell(ctx, cmd) }
	return hunt.AttrPID(shell, capSlice(pids, 80))
}

// huntPidsToAttr — какие pids атрибуцировать: из хитов (-q) или из
// CJK-издателей (-cjk), иначе все pids снимка.
func huntPidsToAttr(lines []string, phrases []string, cjk bool) []int {
	var pids []int
	if len(phrases) > 0 {
		rep := hunt.Search(lines, phrases)
		pids = hitPids(rep)
	}
	if cjk {
		cr := hunt.CollectCJK(lines)
		for _, p := range cr.UniqueEmitPids() {
			if !contains(pids, p) {
				pids = append(pids, p)
			}
		}
	} else if len(pids) == 0 {
		pids = repPidsIfAny(lines, phrases)
	}
	return pids
}

// assembleReport — общий отчёт: блок -q (фразы), блок -cjk (всё CJK).
func assembleReport(lines []string, phrases []string, cjk bool, attr map[int]string) string {
	var b strings.Builder
	if len(phrases) > 0 {
		rep := hunt.Search(lines, phrases)
		rep.AttrPids(attr)
		for i := range rep.Hits {
			if h := &rep.Hits[i]; h.Line.PID > 0 {
				h.Pkg = rep.Pids[h.Line.PID]
			}
		}
		b.WriteString(rep.Format(strings.Join(phrases, " | ")))
	}
	if cjk {
		cr := hunt.CollectCJK(lines)
		cr.AttrPids(attr)
		b.WriteString(cr.Format("cjk"))
	}
	if b.Len() == 0 {
		b.WriteString("(охота не назначена: ни -q, ни -cjk)\n")
	}
	return b.String()
}

func hitPids(rep hunt.Report) []int {
	seen := map[int]bool{}
	var out []int
	for _, h := range rep.Hits {
		if h.Line.PID > 0 && !seen[h.Line.PID] {
			seen[h.Line.PID] = true
			out = append(out, h.Line.PID)
		}
	}
	if len(out) == 0 {
		out = rep.UniquePids()
	}
	return out
}

func repPidsIfAny(lines []string, phrases []string) []int {
	if len(phrases) == 0 {
		return nil
	}
	return hitPids(hunt.Search(lines, phrases))
}

func contains(s []int, v int) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// deviceOK — есть ли в `adb devices` состояние device (опционально с
// нужным serial; пусто — любой).
func deviceOK(a *adb.Adb, ctx context.Context, wantSerial string) bool {
	for _, d := range a.Devices(ctx) {
		if d.State != "device" {
			continue
		}
		if wantSerial == "" || d.Serial == wantSerial {
			return true
		}
	}
	return false
}

// loadRing — строки снимка из памяти (для hunt без перечитывания файла).
func loadRing(lg *shot.Logger) []string {
	return lg.Exposed()
}

// offlineHunt — охота по уже снятому снимку (без adb). Атрибуция
// pid→пакет недоступна (нет устройства) — только текст/pid/контекст.
func offlineHunt(path string, phrases []string, cjk bool) error {
	lines, err := shot.LoadLogFromFile(path)
	if err != nil {
		return err
	}
	fmt.Printf("Offline-охота по %s (строк: %d)\n", path, len(lines))
	out := assembleReport(lines, phrases, cjk, nil)
	hitsF := strings.TrimSuffix(path, ".log") + ".hits.txt"
	if err := os.WriteFile(hitsF, []byte(out), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "WARN: hits:", err)
	}
	fmt.Println()
	fmt.Print(out)
	fmt.Println("hits: " + hitsF)
	fmt.Println("(offline: атрибуция PID->пакет недоступна, см. .meta.txt и pid-карту)")
	return nil
}

func capSlice(s []int, n int) []int {
	if len(s) > n {
		s = s[:n]
	}
	return s
}

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func usage() {
	fmt.Print(`logshot — снимок logcat ГУ + охота за фразой с атрибуцией PID->приложение

Использование:
  shot -q "请检查四周情况" -d 120
  shot -q A -q B -d 180 -dir logs/logshot
  shot -cjk -d 180                      # ВСЕ китайские фрагменты
  shot -q "请检查四周情况" -cjk          # и фразы, и всё CJK
  shot -offline shot_20261002_213512.log -q "请检查四周情况"
  shot -offline shot_20261002_213512.log -cjk

Флаги:
  -q ФРАЗА        охота за фразой (можно несколько)
  -cjk            охота за ВСЕМИ китайскими фрагментами (неизвестные
                  заранее — приходят runtime: FDBus/TBOX/scene.db/TTS).
                  Агрегирует токены, сортирует по частоте, атрибутит pid.
                  Совместимо с -q (отдельный блок в отчёте).
  -d ДЛ          длительность снятия, напр 120s (по 120s; -1s = до Ctrl+C)
  -dir ПАПКА      папка снимков (по logs/logshot)
  -s SERIAL       serial устройства (пусто — единственный)
  -bufrs СПИСОК   буферы logcat (по main,system,crash)
  -offline ФАЙЛ   охота по уже снятому снимку (без ГУ; атрибуция off)

Снимки пишутся в <dir>:
  shot_<ts>.log       - весь logcat (threadtime + наш timestamp)
  shot_<ts>.meta.txt  - окружение (locale, uid, pids-подозреваемых)
  shot_<ts>.hits.txt  - отчёт: хиты -q + CJK-fragment-отчёт (-cjk)
`)
}
