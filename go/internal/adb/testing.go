package adb

import "context"

// TestSeams — открытые точки подмены исполнения для тестов ДРУГИХ пакетов
// (пакет app не может трогать неэкспортированные execFn/availableFn).
// В проде не используется.
type TestSeams struct {
	// Exec подменяет выполнение команд (nil = реальный exec).
	Exec func(ctx context.Context, name string, args ...string) (string, string, int)
	// Available подменяет проверку наличия adb (nil = реальный Available).
	Available func() bool
}

// ForTest — Adb, работающий через TestSeams (тесты, мок adb).
func ForTest(s *TestSeams) *Adb {
	a := New()
	a.execFn = func(ctx context.Context, name string, args ...string) (string, string, int) {
		if s.Exec != nil {
			return s.Exec(ctx, name, args...)
		}
		return "", "", 0
	}
	a.availableFn = func() bool {
		if s.Available != nil {
			return s.Available()
		}
		return true
	}
	return a
}
