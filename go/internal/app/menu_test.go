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

// Баннер в unauthorized-состоянии: своё сообщение (ключ adb этого ПК не
// добавлен — adbauth), НЕ общее «устройство не найдено».
func TestMenuBannerUnauthShowsKeyHint(t *testing.T) {
	ma := NewMockADB()
	ma.Seams.Exec = func(_ context.Context, name string, args ...string) (string, string, int) {
		if len(args) > 0 && args[0] == "devices" {
			return "List of devices attached\nFAKE\tunauthorized\n", "", 0
		}
		return "", "", 0
	}
	cfg := groups.Dynamic()
	cfg.Groups = [4][]string{{"Cam"}, nil, nil, nil}
	cfg.Top8 = []string{"Cam"}
	w := newCollectWriter(t)
	a := New(context.Background(), cfg, ma)
	a.Out = io.Discard
	a.In = strings.NewReader("0\n")
	ma.Logger = w
	m := NewMenu(a, cfg.Scheme, w, a.In)
	m.Clear = func(io.Writer) {}

	m.Run()

	if !strings.Contains(w.s, "ADB: [!!]") {
		t.Errorf("баннер unauth должен быть [!!]:\n%s", w.s)
	}
	if !strings.Contains(w.s, "ключ adb этого ПК") {
		t.Errorf("баннер unauth: нет подсказки про ключ adb этого ПК:\n%s", w.s)
	}
	if !strings.Contains(w.s, "adbauth") {
		t.Errorf("баннер unauth: нет подсказки adbauth:\n%s", w.s)
	}
	if strings.Contains(w.s, "Устройство не найдено") {
		t.Errorf("unauth показан как «устройство не найдено» (общее сообщение):\n%s", w.s)
	}
}

// Шапка меню: ровно ОДНА строка ADB-статуса, без двойного префикса «ADB: ADB:»
// и без строки прогресса CheckFast под ней (молчаливая проверка).
func TestMenuBannerSingleAdLine(t *testing.T) {
	ma := NewMockADB()
	cfg := groups.Dynamic()
	cfg.Groups = [4][]string{{"Cam"}, nil, nil, nil}
	cfg.Top8 = []string{"Cam"}
	w := newCollectWriter(t)
	a := New(context.Background(), cfg, ma)
	a.Out = io.Discard
	a.In = strings.NewReader("0\n") // сразу выход — один экран меню
	ma.Logger = w
	m := NewMenu(a, cfg.Scheme, w, a.In)
	m.Clear = func(io.Writer) {}

	m.Run()

	if n := strings.Count(w.s, "ADB:"); n != 1 {
		t.Errorf("строк с 'ADB:' в шапке %d, want 1:\n%s", n, w.s)
	}
	if strings.Contains(w.s, "[OK] ADB connected.") {
		t.Errorf("строка прогресса CheckFast попала в шапку (дубль баннера):\n%s", w.s)
	}
}

// [R] — вывод Check НЕ смахивается мгновенным cls: в конце пауза на ввод.
func TestMenuRShowsResultAndPauses(t *testing.T) {
	ma := NewMockADB()
	cfg := groups.Dynamic()
	cfg.Groups = [4][]string{{"Cam"}, nil, nil, nil}
	cfg.Top8 = []string{"Cam"}
	w := newCollectWriter(t)
	a := New(context.Background(), cfg, ma)
	a.Out = io.Discard
	a.In = strings.NewReader("R\ny\n0\n") // R -> любая клавиша -> выход
	ma.Logger = w                         // прогресс [R] (строки CheckResult) — в тот же writer, что меню
	m := NewMenu(a, cfg.Scheme, w, a.In)
	m.Clear = func(io.Writer) {}

	m.Run()

	if !strings.Contains(w.s, "[OK] ADB connected.") {
		t.Errorf("R: нет результата проверки:\n%s", w.s)
	}
	if !strings.Contains(w.s, "Нажмите любую клавишу") {
		t.Errorf("R: нет паузы перед следующим меню:\n%s", w.s)
	}
}

// [R] с недоступным adb — тоже пауза (иначе видно ничего, симптом «R не работает»).
func TestMenuRUnavailableShowsErrorAndPauses(t *testing.T) {
	ma := NewMockADB()
	ma.Seams.Available = func() bool { return false }
	cfg := groups.Dynamic()
	cfg.Groups = [4][]string{{"Cam"}, nil, nil, nil}
	cfg.Top8 = []string{"Cam"}
	w := newCollectWriter(t)
	a := New(context.Background(), cfg, ma)
	a.Out = io.Discard
	a.In = strings.NewReader("R\ny\n0\n")
	ma.Logger = w // прогресс [R] — в тот же writer, что меню
	m := NewMenu(a, cfg.Scheme, w, a.In)
	m.Clear = func(io.Writer) {}

	m.Run()

	if !strings.Contains(w.s, "adb not found") {
		t.Errorf("R: нет ошибки про adb:\n%s", w.s)
	}
	if !strings.Contains(w.s, "DEEPL_ADB") {
		t.Errorf("R: нет подсказки DEEPL_ADB:\n%s", w.s)
	}
	if !strings.Contains(w.s, "Нажмите любую клавишу") {
		t.Errorf("R: нет паузы:\n%s", w.s)
	}
}

// Меню: пункт [9] — восстановление оригинальной локали. Вывод в dyn-меню,
// без group-меню (цель не нужна); прогон ставит zh-CN и спрашивает Y/N.
func TestMenuLocaleRestoreDyn(t *testing.T) {
	cfg := groups.Dynamic()
	cfg.Groups = [4][]string{{"Cam"}, nil, nil, nil}
	cfg.Top8 = []string{"Cam"}
	mc := &mockClient{uid: 0, deviceOK: true, locale: wantLocales}
	a := New(context.Background(), cfg, mc)
	a.Now = func() time.Time { return time.Now() }
	a.PmsDelay = 0
	w := newCollectWriter(t)
	m := NewMenu(a, cfg.Scheme, w, strings.NewReader("9\nN\n")) // 9 -> N (выход)
	m.Clear = func(io.Writer) {}

	m.Run()

	if !strings.Contains(w.s, "Восстановить оригинальную локаль") {
		t.Errorf("dyn-меню без пункта [9]:\n%s", w.s)
	}
	if strings.Contains(w.s, "Режим: ") {
		t.Errorf("locale-restore ушёл в group-меню (не должен):%s", w.s)
	}
	if !strings.Contains(w.s, "Вернуться в меню?") {
		t.Errorf("после locale-restore нет Y/N:\n%s", w.s)
	}
	if !containsCall(mc.calls, "shell settings put system system_locales "+originalLocales) {
		t.Errorf("пункт [9] не выставил оригинальную локаль: %v", mc.calls)
	}
}

// Статическое меню: тот же пункт [9] (единое поведение обеих схем).
func TestMenuLocaleRestoreStatic(t *testing.T) {
	cfg := groups.Static()
	cfg.Groups = [4][]string{{"Cam"}, nil, nil, nil}
	cfg.Top8 = []string{"Cam"}
	mc := &mockClient{uid: 0, deviceOK: true, locale: wantLocales}
	a := New(context.Background(), cfg, mc)
	a.Now = func() time.Time { return time.Now() }
	a.PmsDelay = 0
	w := newCollectWriter(t)
	m := NewMenu(a, cfg.Scheme, w, strings.NewReader("9\nN\n"))
	m.Clear = func(io.Writer) {}

	m.Run()

	if !strings.Contains(w.s, "Восстановить оригинальную локаль") {
		t.Errorf("static-меню без пункта [9]:\n%s", w.s)
	}
	if !containsCall(mc.calls, "shell settings put system system_locales "+originalLocales) {
		t.Errorf("статика: пункт [9] не выставил оригинальную локаль: %v", mc.calls)
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
