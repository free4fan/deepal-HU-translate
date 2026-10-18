package shot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"logshot/internal/adb"
)

// mockA — Adb с подменой exec/stream (не трогаем реальный adb).
// lines — «поток» строк, который мок отправляет в lineFn.
func mockA(t *testing.T, lines []string) *adb.Adb {
	t.Helper()
	a := adb.New()
	a.CmdTimeout = time.Second
	a.Logger = &strings.Builder{}
	streamLines := lines
	a.SetExecFn(func(ctx context.Context, name string, args ...string) (string, string, int) {
		return "", "", 0
	})
	var mu sync.Mutex
	a.SetStreamFn(func(ctx context.Context, name string, args []string, lineFn func(string) error) error {
		mu.Lock()
		defer mu.Unlock()
		for _, l := range streamLines {
			if err := lineFn(l); err != nil {
				return nil
			}
		}
		return nil
	})
	return a
}

func TestPushWritesFileAndRing(t *testing.T) {
	dir := t.TempDir()
	logF := filepath.Join(dir, "s.log")
	stream := []string{
		"10-02 21:35:11.998  1234  1240 I System.out: hello",
		"10-02 21:35:12.000  5678  5680 W ActivityManager: world",
	}
	a := mockA(t, stream)
	lg := New(a, logF, []string{"main"}, 10)
	ctx := context.Background()
	if err := lg.StartFile(ctx); err != nil {
		t.Fatal(err)
	}
	// дождёмся, пока обе строки уйдут (поток мок — синхронный под
	// StartFile? Нет: Stream бегут в goroutine. Ждём polling.)
	deadline := time.Now().Add(time.Second)
	for {
		lg.mu.Lock()
		n := lg.lines
		lg.mu.Unlock()
		if n >= len(stream) || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	lg.Stop()
	if lg.Lines() != len(stream) {
		t.Fatalf("lines=%d, want %d", lg.Lines(), len(stream))
	}
	exposed := lg.Exposed()
	if len(exposed) != len(stream) {
		t.Fatalf("ring=%d, want %d", len(exposed), len(stream))
	}
	data, err := os.ReadFile(logF)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "hello") || !strings.Contains(s, "world") {
		t.Errorf("file lacks lines:\n%s", s)
	}
}

func TestStopIdempotentlyCloses(t *testing.T) {
	dir := t.TempDir()
	a := mockA(t, nil)
	lg := New(a, filepath.Join(dir, "s.log"), []string{"main"}, 5)
	ctx := context.Background()
	if err := lg.StartFile(ctx); err != nil {
		t.Fatal(err)
	}
	lg.Stop()
	lg.Stop() // повторно — не падает (sync.Once)
}

func TestWriteMetaUsesExec(t *testing.T) {
	dir := t.TempDir()
	a := adb.New()
	a.CmdTimeout = time.Second
	a.Logger = &strings.Builder{}
	a.SetExecFn(func(ctx context.Context, name string, args ...string) (string, string, int) {
		// имитируем getprop/id
		if len(args) > 1 {
			switch args[1] {
			case "ro.product.model":
				return "Deepal S05", "", 0
			case "id":
				return "0", "", 0
			}
		}
		return "", "", 0
	})
	lg := New(a, filepath.Join(dir, "s.log"), []string{"main"}, 5)
	meta := filepath.Join(dir, "s.meta.txt")
	if err := lg.WriteMeta(context.Background(), meta, "fr1 | fr2"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(meta)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, want := range []string{"phrases: fr1 | fr2", "bufrs: main", "logshot meta"} {
		if !strings.Contains(s, want) {
			t.Errorf("meta lacks %q:\n%s", want, s)
		}
	}
}

func TestLoadLogFromFileAndSortedPaths(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "shot_20200101_000000.log")
	if err := os.WriteFile(old, []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	newf := filepath.Join(dir, "shot_20300101_000000.log")
	if err := os.WriteFile(newf, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// второй файл гарантированно новее (mtime) → идёт первым в sorted.
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(newf, future, future); err != nil {
		t.Fatal(err)
	}
	lines, err := LoadLogFromFile(old)
	if err != nil || len(lines) != 3 {
		t.Fatalf("lines=%v err=%v", lines, err)
	}
	p := SortedPaths(dir)
	if len(p) != 2 || filepath.Base(p[0]) != "shot_20300101_000000.log" {
		t.Errorf("sorted paths wrong: %v", p)
	}
}
