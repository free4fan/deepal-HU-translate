package app

import (
	"fmt"
	"io"
	"strings"
	"time"

	"deepal-hu-translate/internal/groups"
)

func (a *App) print(format string, args ...any) {
	if a.Out != nil {
		io.WriteString(a.Out, fmt.Sprintf(format, args...))
	}
}

func (a *App) printline() { a.print("\n") }

// readLines — строки без CR.
func readLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	var out []string
	for _, l := range strings.Split(s, "\n") {
		out = append(out, strings.TrimRight(l, "\r"))
	}
	return out
}

// isStatic — схема static.
func (a *App) isStatic() bool { return a.Cfg.Scheme == groups.SchemeStatic }

// pmsSleep — пауза после install перед enable (PMS-кейс WT_WtSystemUI).
// Пустая при a.PmsDelay <= 0 (тесты).
func (a *App) pmsSleep() {
	if d := a.PmsDelay; d > 0 {
		time.Sleep(d)
	}
}

// rootSleep — пауза после `adb root` (перезапуск adbd); 0 в тестах.
func (a *App) rootSleep() {
	if d := a.PmsDelay; d > 0 {
		time.Sleep(time.Second)
	}
}
