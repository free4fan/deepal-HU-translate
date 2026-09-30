// Command deepl — единая точка управления RRO-оверлеями Deepal HU.
// Заменяет manage.bat / manage_static.bat / collect_report.bat и все
// install_*_/uninstall_*_ обёртки.
//
// Использование:
//
//	deepl                                - интерактивное меню (динамические)
//	deepl dyn [mode] [preset]            - динамические (CLI-режим)
//	deepl stat [mode] [preset]           - статические (без args -> меню)
//	deepl stat-install-8                 - установить TOP-8 (static)
//	deepl stat-install-36                - установить все доступные (static)
//	deepl stat-install-all               - установить все группы (static)
//	deepl stat-uninstall-all             - удалить все группы (static)
//	deepl report [dyn|stat]              - собрать zip-отчёт
//
// modes:   install | enable | disable | uninstall | status | diag | report
// presets: 1 | 2 | 3 | 4 | 8 | A | C | auto | <имена пакетов через пробел> | 0
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"deepal-hu-translate/internal/adb"
	"deepal-hu-translate/internal/app"
	"deepal-hu-translate/internal/groups"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	// Ctrl+C — чистый выход (в батниках такого не было).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if len(args) == 0 {
		return interactive(ctx, groups.Dynamic(), os.Stdout, os.Stdin)
	}

	cmd := args[0]
	rest := args[1:]

	switch cmd {
	case "-h", "--help", "help":
		usage()
		return nil
	case "-v", "--version":
		fmt.Println("deepl version 1.0.0 (deepal-hu-translate)")
		return nil
	case "dyn":
		cfg := groups.Dynamic()
		if len(rest) == 0 {
			return interactive(ctx, cfg, os.Stdout, os.Stdin)
		}
		return cliMode(ctx, cfg, rest, os.Stdout, os.Stdin)
	case "stat":
		cfg := groups.Static()
		if len(rest) == 0 {
			return interactive(ctx, cfg, os.Stdout, os.Stdin)
		}
		return cliMode(ctx, cfg, rest, os.Stdout, os.Stdin)
	case "stat-install-8":
		return cliMode(ctx, groups.Static(), []string{"install", "8"}, os.Stdout, os.Stdin)
	case "stat-install-36":
		return cliMode(ctx, groups.Static(), []string{"install", "auto"}, os.Stdout, os.Stdin)
	case "stat-install-all":
		return cliMode(ctx, groups.Static(), []string{"install", "A"}, os.Stdout, os.Stdin)
	case "stat-uninstall-all":
		return cliMode(ctx, groups.Static(), []string{"uninstall", "A"}, os.Stdout, os.Stdin)
	case "report":
		scheme := groups.SchemeDynamic
		if len(rest) > 0 && (rest[0] == "stat" || rest[0] == "static") {
			scheme = groups.SchemeStatic
		}
		return cliMode(ctx, groupsFor(scheme), []string{"report"}, os.Stdout, os.Stdin)
	default:
		// Совместимость с manage.bat <mode> [preset].
		if isMode(cmd) {
			arg := append([]string{cmd}, rest...)
			return cliMode(ctx, groups.Dynamic(), arg, os.Stdout, os.Stdin)
		}
		fmt.Fprintf(os.Stderr, "неизвестная подкоманда %q\n\n", cmd)
		usage()
		return nil
	}
}

func groupsFor(s groups.Scheme) *groups.Config {
	if s == groups.SchemeStatic {
		return groups.Static()
	}
	return groups.Dynamic()
}

// interactive — интерактивное меню (manage.bat без аргументов).
// Чистка экрана — нативная (cmd /c cls на Windows, clear на Unix), 1:1 с `cls` из bat.
func interactive(ctx context.Context, cfg *groups.Config, out io.Writer, in io.Reader) error {
	a := app.New(ctx, cfg, adb.New())
	a.Out = out
	a.In = in
	m := app.NewMenu(a, cfg.Scheme, out, in)
	m.Run()
	return nil
}

// cliMode — один прогон режима (как CLI=1 в батниках): без меню,
// после выполнения — пауза (если TTY) и exit.
func cliMode(ctx context.Context, cfg *groups.Config, rest []string, out io.Writer, in io.Reader) error {
	if len(rest) == 0 {
		return fmt.Errorf("не указан mode (install|enable|disable|uninstall|status|diag|report)")
	}
	modeStr := rest[0]
	preset := ""
	if len(rest) > 1 {
		preset = rest[1]
	}
	mode, ok := parseMode(modeStr)
	if !ok {
		return fmt.Errorf("неизвестный mode %q (ожидается: install|enable|disable|uninstall|status|diag|report)", modeStr)
	}

	a := app.New(ctx, cfg, adb.New())
	a.Out = out
	a.In = in
	_, err := a.Run(mode, preset)
	pause(out)
	return err
}

func parseMode(s string) (app.Mode, bool) {
	switch strings.ToLower(s) {
	case "install":
		return app.ModeInstall, true
	case "enable":
		return app.ModeEnable, true
	case "disable":
		return app.ModeDisable, true
	case "uninstall":
		return app.ModeUninstall, true
	case "status":
		return app.ModeStatus, true
	case "diag":
		return app.ModeDiag, true
	case "report":
		return app.ModeReport, true
	}
	return "", false
}

func isMode(s string) bool { _, ok := parseMode(s); return ok }

// pause — «Нажмите Enter...» (как pause в батниках), только на TTY.
func pause(out io.Writer) {
	f, ok := out.(*os.File)
	if !ok || !isTTY(f) {
		return
	}
	fmt.Fprint(out, "\nНажмите Enter для выхода...")
	buf := make([]byte, 1)
	_, _ = os.Stdin.Read(buf)
}

func isTTY(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func usage() {
	fmt.Println(`deepl — менеджер RRO-оверлеев Deepal HU (динамические/статические)

Использование:
  deepl                                меню (динамические RRO)
  deepl dyn [mode] [preset]            динамические (CLI)
  deepl stat [mode] [preset]           статические (без args -> меню)
  deepl stat-install-8                 установить TOP-8 (static)
  deepl stat-install-36                установить все доступные (static)
  deepl stat-install-all               установить все группы (static)
  deepl stat-uninstall-all             удалить все группы (static)
  deepl report [dyn|stat]              собрать zip-отчёт

modes:   install | enable | disable | uninstall | status | diag | report
presets: 1 | 2 | 3 | 4 | 8 | A | C | auto | <пакеты через пробел> | 0 (меню)

Любой пакет (не только из групп):
  deepl dyn install Camera              - установить+включить один
  deepl dyn enable Camera WT_Launcher   - включить несколько
  deepl dyn uninstall AdayoDvr          - удалить (меню)

Примеры:
  deepl dyn install 8      - установить+включить TOP-8 динамических
  deepl stat install A     - установить все статические
  deepl stat uninstall A   - удалить все статические
  deepl report dynamic     - отчёт по динамическим`)
}
