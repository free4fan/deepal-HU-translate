// Package app — оркестрация сценариев: динамические и статические RRO,
// ops-лог, root, статус/диагностика. UI (меню, ввод) отделено в ui.go,
// CLI-разбор — в cmd/deepl. adb доступен через интерфейс Client — можно
// полностью мокать в тестах (не нужен живой adb).
package app

import (
	"context"
	"fmt"
	"io"
	"time"

	"deepal-hu-translate/internal/adb"
	"deepal-hu-translate/internal/groups"
	"deepal-hu-translate/internal/opslog"
)

// Mode — изменяющие/служебные действия (совпадают с CLI и меню).
type Mode string

const (
	ModeInstall   Mode = "install"
	ModeEnable    Mode = "enable"
	ModeDisable   Mode = "disable"
	ModeUninstall Mode = "uninstall"
	ModeStatus    Mode = "status"
	ModeDiag      Mode = "diag"
	ModeReport    Mode = "report"
)

// mutating — изменяющие режимы (требуют root, пишут ops-лог).
func (m Mode) mutating() bool {
	switch m {
	case ModeInstall, ModeEnable, ModeDisable, ModeUninstall:
		return true
	}
	return false
}

// Outcome — счётчики прогона.
type Outcome struct {
	Total int
	Ok    int
	Fail  int
	Miss  int
	Root  bool // root ли удалось включить (для изменяющих dynamic)
}

// App — состояние прогона.
type App struct {
	Ctx context.Context
	Adb adb.Client

	Cfg   *groups.Config
	Logs  string // каталог логов (logs/)
	Out   io.Writer
	In    io.Reader // ввод с клавиатуры (для diag; nil = stdin)
	Now   func() time.Time
	Pause func() // подтверждение в CLI-режиме (pause); nil = no-op

	// PmsDelay — пауза после install перед enable (PMS-кейс WT_WtSystemUI).
	// 0 в тестах.
	PmsDelay time.Duration

	ol *opslog.OpsLog // текущий ops-лог (заполняется на время изменяющего прогона)
}

// New — App со значениями по умолчанию.
func New(ctx context.Context, cfg *groups.Config, adbClient adb.Client) *App {
	return &App{
		Ctx:      ctx,
		Adb:      adbClient,
		Cfg:      cfg,
		Logs:     "logs",
		Out:      io.Discard,
		Now:      time.Now,
		PmsDelay: 1500 * time.Millisecond,
	}
}

// Run — диспетчер одного режима. Возвращает error, если режим не выполнен
// (нет adb, нет root, отсутствует цель) — вызывающий решает, в меню/выход.
func (a *App) Run(mode Mode, preset string) (Outcome, error) {
	// Любое действие (вкл. status/diag/report) требует живого ADB —
	// как manage.bat: call :ADB_CHECK до RUN для любого mode.
	if !a.Adb.Check(a.Ctx) {
		return Outcome{}, fmt.Errorf("ADB недоступен - режим %q не запускается (повторная проверка - [R])", mode)
	}

	if mode == ModeReport {
		_, _, st, err := a.runReport()
		return Outcome{Total: st.Total, Ok: st.Abs, Miss: st.Miss}, err
	}

	switch mode {
	case ModeStatus:
		a.status(false)
		return Outcome{}, nil
	case ModeDiag:
		a.diag()
		return Outcome{}, nil
	}

	// изменяющие + status/diag отделились выше; здесь install/enable/disable/uninstall
	targets, presetLabel, ok := a.Cfg.ResolvePreset(preset)
	if !ok || len(targets) == 0 {
		return Outcome{}, fmt.Errorf("пустая или неверная цель (preset=%q) — выберите группу", preset)
	}

	var ol *opslog.OpsLog
	now := a.Now()
	if mode == ModeInstall || mode == ModeEnable || mode == ModeDisable || mode == ModeUninstall {
		ol, _ = opslog.Open(a.Logs, string(mode), presetLabel, string(a.Cfg.Scheme), now)
	}
	a.ol = ol
	defer func() { a.ol = nil }()

	var out Outcome
	if a.Cfg.Scheme == groups.SchemeStatic {
		out = a.runStatic(mode, targets, ol, now)
	} else {
		out = a.runDynamic(mode, targets, ol, now)
	}

	if ol != nil {
		ol.Close(a.Now(), out.Total, out.Ok, out.Fail, out.Miss)
		fmt.Fprintf(a.Out, "  Операции записаны: %s\n", ol.Path())
	}
	a.printSummary(mode, out)
	if mode != ModeStatus && mode != ModeDiag {
		a.status(true)
	}
	return out, nil
}

// ensureRoot — блок из manage.bat:328-356. Читает uid; если != 0 — выполняет
// `adb root`, ждёт adbd, перечитывает uid. Всегда возвращает true (никогда не
// прерывает операцию) — только логирует WARN. Пишет маркерные строки [ROOT] в
// ops-лог, если он передан.
func (a *App) ensureRoot(ol *opslog.OpsLog) (isRoot bool, prev, cur int) {
	uidNow, _, err := a.Adb.UID(a.Ctx)
	if err != nil {
		fmt.Fprintf(a.Out, "  [WARN] не удалось определить uid: %v\n", err)
		return false, -1, -1
	}
	prev = uidNow
	if uidNow == 0 {
		return true, 0, 0
	}
	fmt.Fprintf(a.Out, "[ROOT] adb shell uid=%d - переключаю на root (adb root, рестарт adbd)...\n", uidNow)
	if ol != nil {
		ol.Root("uid=%d - adb root...", uidNow)
	}
	rootRes := a.Adb.Root(a.Ctx)
	fmt.Fprintf(a.Out, "  db: %s\n", rootRes.Out)
	if ol != nil {
		ol.Db("adb root: " + rootRes.Out)
	}
	// adbd перезапускается — ждём
	_ = a.Adb.WaitDevice(a.Ctx)
	uidNew, _, err := a.Adb.UID(a.Ctx)
	if err != nil {
		fmt.Fprintf(a.Out, "  [WARN] не удалось перечитать uid после root: %v\n", err)
		return false, prev, -1
	}
	cur = uidNew
	if uidNew == 0 {
		fmt.Fprintln(a.Out, "[OK] root активен: uid=0 - операции выполняются от root.")
		if ol != nil {
			ol.Root("uid=0 (было %d)", prev)
		}
		return true, prev, 0
	}
	fmt.Fprintf(a.Out, "[WARN] root НЕ активен: uid=%d (ожидал 0) - adb root не supported? enable целей с <overlayable> (WT_WtSystemUI) упадёт.\n", uidNew)
	if ol != nil {
		ol.Root("uid=%d после adb root - не root (было %d)", uidNew, prev)
	}
	return false, prev, uidNew
}
