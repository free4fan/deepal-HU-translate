package app

import (
	"bufio"
	"strings"
)

// diag — режим [6] (батник :Diag). Локаль, целевые оверлеи, интерактивный
// `cmd overlay dump <pkg>`, dumpsys-проверка целевого приложения.
// Ввод — a.In (в меню — stdin; в тестах — заданный строковый поток).
func (a *App) diag() {
	a.print("%s\n", strings.Repeat("=", 40))
	scheme := "динамические"
	if a.isStatic() {
		scheme = "статические"
	}
	a.print("Deepal RRO Diagnostic (%s)\n", scheme)
	a.print("%s\n", strings.Repeat("=", 40))
	a.print("\n1. System language:\n")
	a.shellOp("settings", "get", "system", "system_locales")
	a.print("\n")
	a.status(true)
	a.print("\n")

	if a.isStatic() {
		a.print("2. Содержимое %s (целевые оверлеи):\n", a.Cfg.OverlayBase)
		a.shellOp("ls -d", a.Cfg.OverlayBase+"/"+a.Cfg.Prefix+"*")
		a.print("\n")
	}

	a.print("3. Overlay dump:\n")
	dpkg := a.prompt("Пакет для dump (Enter - пропустить): ")
	if dpkg != "" {
		a.print("\n   dump:\n")
		a.shellOp("cmd", "overlay", "dump", dpkg)
	}
	a.print("\n4. Target app overlays:\n")
	tpkg := a.prompt("Целевое приложение для dumpsys (напр. com.adayo.app.apa, Enter - пропустить): ")
	if tpkg != "" {
		a.print("\n")
		out := a.Adb.Shell(a.Ctx, "dumpsys", "package", tpkg).Out
		a.echoFiltered(out, "overlay")
	}
	a.print("\n")
}

// prompt — вопрос с одной строкой ответа (вход — a.In; nil -> "").
func (a *App) prompt(q string) string {
	a.print(q)
	if a.In == nil {
		return ""
	}
	sc := bufio.NewReader(a.In)
	line, _ := sc.ReadString('\n')
	return strings.TrimSpace(line)
}
