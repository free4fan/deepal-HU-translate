package app

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"deepal-hu-translate/internal/groups"
)

// newMenuTest — Menu с заданным списком пакетов (группа 1) и stdin/piped-out.
func newMenuTest(t *testing.T, names []string, stdin string) *Menu {
	t.Helper()
	cfg := groups.Dynamic()
	cfg.Groups = [4][]string{names, nil, nil, nil}
	if len(names) > 0 {
		cfg.Top8 = names[:1]
	}
	mc := &mockClient{uid: 0, deviceOK: true}
	a := New(context.Background(), cfg, mc)
	a.Now = func() time.Time { return time.Now() }
	m := NewMenu(a, cfg.Scheme, newCollectWriter(t), strings.NewReader(stdin))
	m.Clear = func(io.Writer) {} // без cls (pipe)
	return m
}

// ---- parsePicks ----

func TestParsePicks(t *testing.T) {
	names := []string{"Cam", "B", "C", "D", "E", "F", "G"}
	m := newMenuTest(t, names, "0\n")

	got, ok := m.parsePicks("5", names)
	if !ok || len(got) != 1 || got[0] != "E" {
		t.Errorf("«5» -> %v %v, want [E]", got, ok)
	}
	got, ok = m.parsePicks("5,3 1", names)
	// порядок — как выбрал пользователь (1-е вхождение дубля)
	if !ok || len(got) != 3 || got[0] != "E" || got[1] != "C" || got[2] != "Cam" {
		t.Errorf("«5,3 1» -> %v %v, want [E C Cam]", got, ok)
	}
	got, ok = m.parsePicks("2 2 3", names) // дубли
	if !ok || len(got) != 2 || got[0] != "B" || got[1] != "C" {
		t.Errorf("дубли: %v %v", got, ok)
	}
	if _, ok := m.parsePicks("99 100", names); ok {
		t.Error("вне диапазона -> ok=false")
	}
	if _, ok := m.parsePicks("x y", names); ok {
		t.Error("не числа -> ok=false")
	}
	if _, ok := m.parsePicks("", names); ok {
		t.Error("пусто -> ok=false")
	}
	got, ok = m.parsePicks("1;7,2", names)
	if !ok || len(got) != 3 || got[0] != "Cam" || got[1] != "G" || got[2] != "B" {
		t.Errorf("«1;7,2» -> %v %v", got, ok)
	}
}

// ---- packageList (сквозной, через stdin) ----

func TestPackageListSinglePick(t *testing.T) {
	names := []string{"Cam", "B", "C", "D", "E"}
	m := newMenuTest(t, names, "L\n3\n0\n")
	preset := m.groupMenu("enable")
	if preset != "C" {
		t.Errorf("groupMenu -> %q, want \"C\"", preset)
	}
	out := m.captured()
	if !strings.Contains(out, "Пакеты: 5") {
		t.Errorf("не показан список пакетов:\n%s", out)
	}
	if !strings.Contains(out, "  3  C") {
		t.Errorf("нет строки пакета 3:\n%s", out)
	}
}

func TestPackageListMultiPick(t *testing.T) {
	names := []string{"Cam", "B", "C", "D", "E", "F"}
	m := newMenuTest(t, names, "L\n2,4 6\n")
	preset := m.groupMenu("enable")
	if preset != "B D F" {
		t.Errorf("groupMenu -> %q, want \"B D F\"", preset)
	}
}

func TestPackageListBadThenBack(t *testing.T) {
	names := []string{"Cam", "B"}
	m := newMenuTest(t, names, "L\n99\n0\n")
	preset := m.groupMenu("enable")
	if preset != "0" {
		t.Errorf("groupMenu -> %q, want \"0\" (99 невалиден, 0 назад)", preset)
	}
	if !strings.Contains(m.captured(), "Неверный номер") {
		t.Error("нет сообщения о неверном номере")
	}
}

// captured — накопленный вывод (для pipe-writer).
func (m *Menu) captured() string {
	if w, ok := m.out.(*captureWriter); ok {
		return w.s
	}
	return ""
}

// Сценарий: без ADB выбрали действие и «0» (назад к действию) —
// ADB-check (3×ожидание+kill-server) и Y/N быть не должно.
func TestMenuBackWithoutAdbCheck(t *testing.T) {
	names := []string{"Cam", "B"}
	mc := &mockClient{uid: 0, deviceOK: false}
	cfg := groups.Dynamic()
	cfg.Groups = [4][]string{names, nil, nil, nil}
	cfg.Top8 = names[:1]
	a := New(context.Background(), cfg, mc)
	a.Now = func() time.Time { return time.Now() }
	// stdin: действие 2 (enable) -> цель 0 (назад) -> действие 0 (выход)
	w := newCollectWriter(t)
	m := NewMenu(a, cfg.Scheme, w, strings.NewReader("2\n0\n0\n"))
	m.Clear = func(io.Writer) {}

	m.Run()

	for _, c := range mc.calls {
		if c == "check" {
			t.Error("полный ADB-check вызван при выборе цели 0 (назад)")
		}
	}
	if strings.Contains(w.s, "Вернуться в меню?") {
		t.Error("Y/N выведен, хотя пользователь только вернулся назад")
	}
	if strings.Count(w.s, "Режим: enable") != 1 {
		t.Errorf("group-меню показано %d раз, want 1:\n%s", strings.Count(w.s, "Режим: enable"), w.s)
	}
}

// Сценарий: несуществующая цель "10" — не прогон и не ADB-check,
// а «Неверный выбор» и повтор group-меню.
func TestMenuInvalidPresetNoAdb(t *testing.T) {
	names := []string{"Cam", "B"}
	mc := &mockClient{uid: 0, deviceOK: false}
	cfg := groups.Dynamic()
	cfg.Groups = [4][]string{names, nil, nil, nil}
	cfg.Top8 = names[:1]
	a := New(context.Background(), cfg, mc)
	a.Now = func() time.Time { return time.Now() }
	a.PmsDelay = 0
	// stdin: действие 2 -> цель 10 (неверно) -> цель 0 (назад) -> 0 (выход)
	w := newCollectWriter(t)
	m := NewMenu(a, cfg.Scheme, w, strings.NewReader("2\n10\n0\n0\n"))
	m.Clear = func(io.Writer) {}

	m.Run()

	for _, c := range mc.calls {
		if c == "check" {
			t.Error("ADB-check вызван при вводе несуществующей цели 10")
		}
	}
	if n := strings.Count(w.s, "Неверный выбор."); n != 1 {
		t.Errorf("«Неверный выбор» выведен %d раз, want 1:\n%s", n, w.s)
	}
	if n := strings.Count(w.s, "Режим: enable"); n != 2 {
		t.Errorf("group-меню показано %d раз, want 2 (повтор после 10):\n%s", n, w.s)
	}
	if strings.Contains(w.s, "Вернуться в меню?") {
		t.Error("Y/N не должен выводиться")
	}
}

// captureWriter — io.Writer, коллекция.
type captureWriter struct{ s string }

func (w *captureWriter) Write(p []byte) (int, error) {
	w.s += string(p)
	return len(p), nil
}

// newCollectWriter — captureWriter + noop-Clear.
func newCollectWriter(t *testing.T) *captureWriter {
	return &captureWriter{}
}
