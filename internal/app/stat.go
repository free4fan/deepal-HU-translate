package app

import (
	"os"
	"time"

	"deepal-hu-translate/internal/opslog"
)

// runStatic — изменяющие режимы для static RRO (manage_static.bat :DoOne*).
// install: root+remount; mkdir -p <pkg-каталог>; push APK.
// uninstall: rm -rf <pkg-каталог>.
// Обе требуют `adb reboot` для применения (подсказка в summary).
func (a *App) runStatic(mode Mode, targets []string, ol *opslog.OpsLog, start time.Time) Outcome {
	if mode.mutating() && !a.rootRemount() {
		return Outcome{}
	}
	var out Outcome
	for _, name := range targets {
		out.Total++
		switch mode {
		case ModeInstall:
			out = a.inc(out, a.staticInstall(name, ol))
		case ModeUninstall:
			out = a.inc(out, a.staticUninstall(name, ol))
		}
	}
	return out
}

// rootRemount — из manage_static.bat :ROOT_CHECK.
// adb root -> shell живёт? -> mount -o rw,remount /vendor (fallback: adb remount).
// false -> операция не выполняется (CLI: пауза+exit/1; меню: возврат).
func (a *App) rootRemount() bool {
	a.print("Obtaining root access...\n")
	a.Adb.Root(a.Ctx)
	a.rootSleep()
	if res := a.Adb.Shell(a.Ctx, "getprop", "ro.vendor.build.fingerprint"); !res.OK() {
		a.print("ERROR: Device not rooted or adb shell not accessible.\n")
		return false
	}
	a.print("Remounting /vendor as read-write...\n")
	if res := a.Adb.Shell(a.Ctx, "mount", "-o", "rw,remount", "/vendor"); !res.OK() {
		if !a.remount() {
			a.print("ERROR: Cannot remount /vendor as read-write.\n")
			return false
		}
	}
	a.print("[OK] /vendor is writable.\n\n")
	return true
}

// remount — `adb remount` (не shell: отдельная команда adb на хосте).
func (a *App) remount() bool {
	a.print("  adb remount...\n")
	return a.Adb.Remount(a.Ctx).OK()
}

// staticInstall — mkdir -p <base>/<pkg> + push apk.
func (a *App) staticInstall(name string, ol *opslog.OpsLog) opResult {
	pkg := a.Cfg.Package(name)
	src := a.Cfg.ApkPath(name)
	dst := a.Cfg.OverlayBase + "/" + pkg
	if _, err := os.Stat(src); err != nil {
		a.print("[WARN] %s: нет %s\n\n", name, src)
		if ol != nil {
			ol.Begin("[INSTALL] %s [%s] src=%s dst=%s", name, pkg, src, dst)
			ol.Result("WARN (нет %s)", src)
		}
		return resMiss
	}
	a.print("[INSTALL] %s [%s]\n", name, pkg)
	if ol != nil {
		ol.Begin("[INSTALL] %s [%s] src=%s dst=%s", name, pkg, src, dst)
	}
	if res := a.Adb.Shell(a.Ctx, "mkdir -p", dst); !res.OK() {
		a.print("  [ERROR] не удалось создать папку %s\n\n", dst)
		if ol != nil {
			ol.Result("ERROR (mkdir %s не удался / нет root?)", dst)
		}
		return resFail
	}
	res := a.Adb.Push(a.Ctx, src, dst+"/")
	if res.OK() {
		a.print("  [OK] push в %s\n\n", dst)
		if ol != nil {
			ol.Result("OK (push в %s; применить - adb reboot)", dst)
		}
		return resOK
	}
	a.print("  [ERROR] push не удался\n\n")
	if ol != nil {
		ol.Result("ERROR (adb push не удался)")
	}
	return resFail
}

// staticUninstall — rm -rf <base>/<pkg>.
func (a *App) staticUninstall(name string, ol *opslog.OpsLog) opResult {
	pkg := a.Cfg.Package(name)
	dst := a.Cfg.OverlayBase + "/" + pkg
	a.print("[UNINSTALL] %s [%s]\n", name, pkg)
	if ol != nil {
		ol.Begin("[UNINSTALL] %s [%s] dst=%s", name, pkg, dst)
	}
	res := a.Adb.Shell(a.Ctx, "rm -rf", dst)
	if res.OK() {
		a.print("  [OK] удалён\n\n")
		if ol != nil {
			ol.Result("OK (удалён %s; применить - adb reboot)", dst)
		}
		return resOK
	}
	a.print("  [WARN] не удалён (папки не было или нет прав)\n\n")
	if ol != nil {
		ol.Result("WARN (не удалён: папки не было или нет прав)")
	}
	return resFail
}
