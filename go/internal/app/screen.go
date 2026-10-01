package app

import (
	"os"
	"sync"
)

// ansiOnce — VT100 на Windows включается один раз (идемпотентно).
var (
	ansiOnce sync.Once
	ansiOK   bool // true, если VT100 работает (красим ANSI-кодом)
)

// ClearScreen — очистка экрана (аналог `cls` из manage.bat).
// Основной путь: ANSI «очистить экран + курсор в начало» (enableAnsi включает
// VT100 на Windows). Если VT-режим недоступен — fallback: `clear`/`cmd /c cls`.
//
// Чистит ТОЛЬКО если out — консольный stdout. При перенаправлении
// (pipe/файл) ничего не делает — чтобы не засорять вывод.
func ClearScreen(out interface{ Write([]byte) (int, error) }) {
	f, ok := out.(*os.File)
	if !ok || f != os.Stdout {
		return
	}
	if !isConsoleStdout(f) {
		return
	}
	ansiOnce.Do(func() { ansiOK = enableAnsi() })
	if ansiOK {
		out.Write([]byte("\x1b[2J\x1b[H"))
		return
	}
	runClearCmd()
}
