//go:build windows

package app

import (
	"os"
	"os/exec"
	"syscall"
	"unsafe"
)

var (
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	procGetStdHandle   = kernel32.NewProc("GetStdHandle")
	procGetConsoleMode = kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode = kernel32.NewProc("SetConsoleMode")
)

const enableVTMode = 0x0004 // ENABLE_VIRTUAL_TERMINAL_PROCESSING

// stdOutHandle — STD_OUTPUT_HANDLE = (HANDLE)-11 (two's-complement uintptr).
func stdOutHandle() uintptr {
	return ^uintptr(10) // -11
}

// isConsoleStdout — на Windows надёжнее по console-mode, чем по ModeCharDevice.
func isConsoleStdout(f *os.File) bool {
	handle, _, _ := procGetStdHandle.Call(stdOutHandle())
	if handle == 0 || handle == 0xFFFFFFFF {
		return false
	}
	var mode uint32
	r, _, _ := procGetConsoleMode.Call(handle, uintptr(unsafe.Pointer(&mode)))
	return r != 0
}

// enableAnsi — включает VT100 (ANSI-escape). false = legacy-консоль без VT.
func enableAnsi() bool {
	handle, _, _ := procGetStdHandle.Call(stdOutHandle())
	if handle == 0 || handle == 0xFFFFFFFF {
		return false
	}
	var mode uint32
	r, _, _ := procGetConsoleMode.Call(handle, uintptr(unsafe.Pointer(&mode)))
	if r == 0 {
		return false
	}
	r, _, _ = procSetConsoleMode.Call(handle, uintptr(mode|enableVTMode))
	return r != 0 // BOOL: non-zero = success
}

// runClearCmd — fallback (legacy-консоль без VT): `cmd /c cls`.
func runClearCmd() { _ = exec.Command("cmd", "/c", "cls").Run() }
