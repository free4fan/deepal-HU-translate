package app

import (
	"os"
	"strings"
	"time"

	"deepal-hu-translate/internal/opslog"
)

// opResult — исход одной операции.
type opResult int

const (
	resOK   opResult = iota // OK
	resFail                 // adb-команда не удалась
	resWarn                 // не критично (установлен, но не включён)
	resMiss                 // APK отсутствует на хосте
)

// runDynamic — изменяющие режимы для dynamic RRO (manage.bat :DoOne*).
// install: adb install -r apks_rro_min/<Name>_RRO.apk + enable
//   (пауза 1.5с + 1 повтор — PMS-кейс WT_WtSystemUI, 22.09).
//
// enable/disable: cmd overlay enable|disable --user 0.
// uninstall: disable + adb uninstall.
func (a *App) runDynamic(mode Mode, targets []string, ol *opslog.OpsLog, start time.Time) Outcome {
	var out Outcome
	if mode.mutating() {
		out.Root, _, _ = a.ensureRoot(ol)
	}
	for _, name := range targets {
		out.Total++
		switch mode {
		case ModeInstall:
			out = a.inc(out, a.dynInstall(name, ol))
		case ModeEnable:
			out = a.inc(out, a.dynEnable(name, ol))
		case ModeDisable:
			out = a.inc(out, a.dynDisable(name, ol))
		case ModeUninstall:
			out = a.inc(out, a.dynUninstall(name, ol))
		}
	}
	return out
}

// inc — начисляет счётчик по результату операции.
// resWarn (установлен, но оверлей не включён) — как OK (батник: OK_C+=1).
func (a *App) inc(o Outcome, r opResult) Outcome {
	switch r {
	case resOK, resWarn:
		o.Ok++
	case resFail:
		o.Fail++
	case resMiss:
		o.Miss++
	}
	return o
}

// dynInstall — install + enable.
func (a *App) dynInstall(name string, ol *opslog.OpsLog) opResult {
	pkg := a.Cfg.Package(name)
	src := a.Cfg.ApkPath(name)

	if _, err := os.Stat(src); err != nil {
		a.print("[WARN] %s: нет %s\n\n", name, src)
		if ol != nil {
			ol.Begin("[INSTALL] %s [%s] src=%s", name, pkg, src)
			ol.Result("WARN (нет %s)", src)
		}
		return resMiss
	}
	a.print("[INSTALL] %s [%s]\n", name, pkg)
	if ol != nil {
		ol.Begin("[INSTALL] %s [%s] src=%s", name, pkg, src)
	}

	res := a.Adb.InstallR(a.Ctx, src)
	a.echo(res.Out, ol)
	if !installSuccess(res.Out) {
		a.print("  [ERROR] установка не удалась\n\n")
		if ol != nil {
			ol.Result("ERROR (adb install: см. строки \"db:\" выше)")
		}
		return resFail
	}

	// enable видимым (не >nul): сразу после install PMS иногда ещё не
	// просканировал пакет -> enable может не пройти (WT_WtSystemUI, 2026-09-22).
	// Короткая пауза и повтор (один).
	a.pmsSleep()
	enabled, _ := a.shellOp("cmd", "overlay", "enable", "--user", "0", pkg)
	if !enabled {
		a.print("  [INFO] enable не прошёл - повтор\n")
		a.pmsSleep()
		enabled, _ = a.shellOp("cmd", "overlay", "enable", "--user", "0", pkg)
	}
	if enabled {
		a.print("  [OK] установлен и включён\n\n")
		if ol != nil {
			ol.Result("OK (установлен и включён)")
		}
		return resOK
	}
	a.print("  [WARN] установлен, НО оверлей не включён - включите: deepl dyn enable\n")
	a.print("  [WARN] если enable завершается с \"UID2000 is not allowed\" - требуется adb root (см. строки [ROOT] выше)\n\n")
	if ol != nil {
		ol.Result("WARN (установлен, оверлей не включён; SecurityException UID2000 => нужен adb root)")
	}
	return resWarn
}

// dynEnable — cmd overlay enable --user 0.
func (a *App) dynEnable(name string, ol *opslog.OpsLog) opResult {
	pkg := a.Cfg.Package(name)
	a.print("[ENABLE] %s [%s]\n", name, pkg)
	if ol != nil {
		ol.Begin("[ENABLE] %s [%s]", name, pkg)
	}
	ok, _ := a.shellOp("cmd", "overlay", "enable", "--user", "0", pkg)
	return a.conclude(ok, "OK", ol)
}

// dynDisable — cmd overlay disable --user 0.
func (a *App) dynDisable(name string, ol *opslog.OpsLog) opResult {
	pkg := a.Cfg.Package(name)
	a.print("[DISABLE] %s [%s]\n", name, pkg)
	if ol != nil {
		ol.Begin("[DISABLE] %s [%s]", name, pkg)
	}
	ok, _ := a.shellOp("cmd", "overlay", "disable", "--user", "0", pkg)
	return a.conclude(ok, "OK", ol)
}

// dynUninstall — disable + adb uninstall.
func (a *App) dynUninstall(name string, ol *opslog.OpsLog) opResult {
	pkg := a.Cfg.Package(name)
	a.print("[UNINSTALL] %s [%s]\n", name, pkg)
	if ol != nil {
		ol.Begin("[UNINSTALL] %s [%s]", name, pkg)
	}
	// сначала disable, потом uninstall — иначе overlay может остаться активным
	a.Adb.Shell(a.Ctx, "cmd", "overlay", "disable", "--user", "0", pkg)
	res := a.Adb.Uninstall(a.Ctx, pkg)
	a.echo(res.Out, ol)
	if res.OK() {
		a.print("  [OK] удалён\n\n")
		if ol != nil {
			ol.Result("OK (удалён)")
		}
		return resOK
	}
	a.print("  [ERROR] (не установлен?)\n\n")
	if ol != nil {
		ol.Result("ERROR (не установлен?)")
	}
	return resFail
}

// conclude — [OK]/[ERROR] + RESULT.
func (a *App) conclude(ok bool, okMsg string, ol *opslog.OpsLog) opResult {
	if ok {
		a.print("  [OK]\n\n")
		if ol != nil {
			ol.Result(okMsg)
		}
		return resOK
	}
	a.print("  [ERROR] (пакет не установлен?)\n\n")
	if ol != nil {
		ol.Result("ERROR (пакет не установлен?)")
	}
	return resFail
}

// shellOp — `adb shell <args...>`; печатает вывод в консоль и в текущий
// ops-лог (строки "db: <line>"). 1:1 с manage.bat :DoOpAdb.
func (a *App) shellOp(args ...string) (bool, string) {
	res := a.Adb.Shell(a.Ctx, args...)
	a.echo(res.Out, a.ol)
	return res.OK(), res.Out
}

// echo — строки вывода: экран + ops-лог "  db: <line>".
func (a *App) echo(out string, ol *opslog.OpsLog) {
	for _, line := range readLines(out) {
		if strings.TrimSpace(line) == "" {
			continue
		}
		a.print("  %s\n", line)
		if ol != nil {
			ol.Db(line)
		}
	}
}

// installSuccess — маркер "Success" по первым 7 символам строки.
// Регрессия хвостового пробела/CR (батник: "Success " шла в ERROR-ветку).
func installSuccess(out string) bool {
	for _, line := range readLines(out) {
		t := strings.TrimSpace(line)
		if len(t) >= 7 && strings.EqualFold(t[:7], "Success") {
			return true
		}
	}
	return false
}

// printSummary — "Итог: <mode>" (1:1 с bat :PrintSummary).
func (a *App) printSummary(mode Mode, o Outcome) {
	a.print("\n%s\n", strings.Repeat("=", 40))
	a.print("  Итог: %s\n", mode)
	a.print("%s\n", strings.Repeat("=", 40))
	a.print("  Всего:      %d\n", o.Total)
	a.print("  OK:         %d\n", o.Ok)
	a.print("  Ошибок:     %d\n", o.Fail)
	a.print("  Не найдено: %d\n", o.Miss)
	if a.isStatic() {
		a.print("  Перезагрузите для применения:  adb reboot\n")
	}
}

// status — режим [5] (`cmd overlay list --user 0`), полный список или целевые.
func (a *App) status(ours bool) {
	a.print("\n%s\n", strings.Repeat("=", 40))
	list := a.Adb.Shell(a.Ctx, "cmd", "overlay", "list", "--user", "0").Out
	if ours {
		a.print("Overlay status (целевые, %s):\n", a.Cfg.Filter)
		nl := strings.ToLower(a.Cfg.Filter)
		for _, line := range readLines(list) {
			if strings.Contains(strings.ToLower(line), nl) {
				a.print("%s\n", line)
			}
		}
	} else {
		a.print("Overlay status (все):\n")
		for _, line := range readLines(list) {
			a.print("%s\n", line)
		}
	}
	a.print("  [x] - включён  [ ] - отключён  --- - ошибка\n")
}

// echoFiltered — строки, содержащие needle (регистронезависимо, как findstr /I /C).
func (a *App) echoFiltered(out, needle string) {
	nl := strings.ToLower(needle)
	for _, line := range readLines(out) {
		if strings.Contains(strings.ToLower(line), nl) {
			a.print("%s\n", line)
		}
	}
}
