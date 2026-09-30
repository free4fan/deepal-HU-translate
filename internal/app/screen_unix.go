//go:build !windows

package app

import (
	"os"
	"os/exec"
)

// isConsoleStdout — unix: char-device = TTY.
func isConsoleStdout(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// enableAnsi — на unix ANSI поддерживается всегда.
func enableAnsi() bool { return true }

// runClearCmd — fallback-очистка: `clear`.
func runClearCmd() { _ = exec.Command("clear").Run() }
