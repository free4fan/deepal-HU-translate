package report

import (
	"context"
	"deepal-hu-translate/internal/adb"
)

// adbIf — минимальный срез adb.Client, необходимый сборщику.
// adb.Client удовлетворяет ему; это позволяет мокать в тестах.
type adbIf interface {
	ShellOut(ctx context.Context, args ...string) adb.Result
	Root(ctx context.Context) adb.Result
	Version(ctx context.Context) adb.Result
}
