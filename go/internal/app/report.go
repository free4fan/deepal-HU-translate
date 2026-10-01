package app

import (
	"context"

	"deepal-hu-translate/internal/adb"
	"deepal-hu-translate/internal/report"
)

// reportStats — report.Stats (используется в Outcome для report-режима).
type reportStats = report.Stats

// reportAdapter — реализация adb.If-среза, который требует report.Input.Adb.
type reportAdapter struct{ a *App }

func (r reportAdapter) ShellOut(ctx context.Context, args ...string) adb.Result {
	return r.a.Adb.ShellOut(ctx, args...)
}
func (r reportAdapter) Root(ctx context.Context) adb.Result    { return r.a.Adb.Root(ctx) }
func (r reportAdapter) Version(ctx context.Context) adb.Result { return r.a.Adb.Version(ctx) }

// runReport — вызов internal/report.Run (шаги 1..9 + zip).
func (a *App) runReport() (string, string, reportStats, error) {
	in := report.Input{
		Cfg:     a.Cfg,
		Adb:     reportAdapter{a},
		Now:     a.Now(),
		LogsDir: a.Logs,
		Out:     a.Out,
	}
	return report.Run(a.Ctx, in)
}
