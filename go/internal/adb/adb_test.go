package adb

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// mock — единая точка подмены: execFn отвечает на все команды,
// availableFn управляет наличием adb, devices — вывод `adb devices`,
// logBuf — буфер прогресса (a.Logger), где ищут строки.
type mock struct {
	*Adb
	calls     []string // "devices", "kill-server", "shell id -u", ...
	devices   string   // текущий вывод adb devices
	available bool
	logBuf    *strings.Builder
}

// keyOf — строка команды без имени исполняемого файла: "devices", "kill-server", ...
func keyOf(name string, args ...string) string {
	full := append([]string{name}, args...)
	if len(full) > 1 {
		return strings.Join(full[1:], " ")
	}
	return full[0]
}

func newMock() *mock {
	a := New()
	a.CmdTimeout = time.Second
	a.UnauthWait = time.Millisecond
	a.RetryDelay = time.Millisecond
	a.WaitDevPoll = time.Millisecond
	var b strings.Builder // тихий логгер (не реальный stdout)
	a.Logger = &b
	m := &mock{Adb: a, devices: "List of devices attached\n", available: true, logBuf: &b}
	a.execFn = func(_ context.Context, name string, args ...string) (string, string, int) {
		key := keyOf(name, args...)
		m.calls = append(m.calls, key)
		if key == "devices" {
			return m.devices, "", 0
		}
		return "", "", 0
	}
	a.availableFn = func() bool { return m.available }
	return m
}

func devDevice() string { return "List of devices attached\nA\tdevice\n" }
func devUnauth() string { return "List of devices attached\nA\tunauthorized\n" }

func TestDevicesState(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want State
	}{
		{"device first", "List of devices attached\nemulator-5554\tdevice\n\n", StateDevice},
		{"unauthorized only", devUnauth(), StateUnauth},
		{"device wins", "List of devices attached\nA\tunauthorized\nB\tdevice\n", StateDevice},
		{"offline only", "List of devices attached\nA\toffline\n", StateNone},
		{"empty", "List of devices attached\n", StateNone},
	}
	for _, tt := range tests {
		if got := devicesState(tt.out); got != tt.want {
			t.Errorf("%s: devicesState=%v want %v", tt.name, got, tt.want)
		}
	}
}

func TestCheckFast(t *testing.T) {
	ctx := context.Background()

	m := newMock()
	m.devices = devDevice()
	if got := m.CheckFast(ctx); got != true {
		t.Errorf("CheckFast(device) = %v, want true", got)
	}
	m.devices = devUnauth()
	if got := m.CheckFast(ctx); got != false {
		t.Errorf("CheckFast(unauthorized) = %v, want false", got)
	}
	m.devices = "List of devices attached\n"
	if got := m.CheckFast(ctx); got != false {
		t.Errorf("CheckFast(none) = %v, want false", got)
	}
	m.available = false
	if got := m.CheckFast(ctx); got != false {
		t.Errorf("CheckFast(no adb) = %v, want false", got)
	}
}

func TestCheckUnavailable(t *testing.T) {
	m := newMock()
	m.available = false
	if got := m.Check(context.Background()); got {
		t.Error("Check(no adb) = true, want false")
	}
	if !strings.Contains(m.logBuf.String(), "adb not found") {
		t.Errorf("лог не содержит 'adb not found': %q", m.logBuf.String())
	}
}

func TestCheckImmediateDevice(t *testing.T) {
	m := newMock()
	m.devices = devDevice()
	if got := m.Check(context.Background()); !got {
		t.Error("Check(device сразу) = false, want true")
	}
	if !strings.Contains(m.logBuf.String(), "[OK] ADB connected.") {
		t.Error("нет '[OK] ADB connected.' в логе")
	}
	if n := countCalls(m.calls, "devices"); n != 1 {
		t.Errorf("devices вызван %d раз, want 1", n)
	}
}

func TestCheckRetryAfterRestart(t *testing.T) {
	m := newMock()
	m.devices = "List of devices attached\n"
	// мутация: после kill-server устройство появляется
	wrap := m.execFn
	m.execFn = func(ctx context.Context, name string, args ...string) (string, string, int) {
		key := keyOf(name, args...)
		m.calls = append(m.calls, key)
		o, e, c := wrap(ctx, name, args...)
		if key == "kill-server" {
			m.devices = devDevice()
		}
		return o, e, c
	}
	if got := m.Check(context.Background()); !got {
		t.Fatal("Check(none -> kill/start -> device) = false, want true")
	}
	if !contains(m.calls, "kill-server") || !contains(m.calls, "start-server") {
		t.Errorf("не вызван рестарт демона: %v", m.calls)
	}
}

func TestCheckUnauthLimit(t *testing.T) {
	m := newMock()
	m.devices = devUnauth()
	if got := m.Check(context.Background()); got {
		t.Error("Check(перманентный unauthorized) = true, want false")
	}
	log := m.logBuf.String()
	if !strings.Contains(log, "ключ НЕ АВТОРИЗОВАН") {
		t.Errorf("нет сообщения о ключе (unauthorized): %q", log)
	}
	if !strings.Contains(log, "публичный ключ") {
		t.Errorf("нет подсказки про публичный ключ: %q", log)
	}
	if !strings.Contains(log, "adbauth") {
		t.Errorf("нет подсказки про adbauth: %q", log)
	}
	if !strings.Contains(log, "остановка.") {
		t.Errorf("нет 'остановка.': %q", log)
	}
	for _, bad := range []string{"АВТОРИЗОВАТЬ", "Для разработчиков", "НЕ АКТИВИРОВАН"} {
		if strings.Contains(log, bad) {
			t.Errorf("unauth-цикл: устаревшая формулировка %q:\n%s", bad, log)
		}
	}
	// devices должен быть запрошен 4 раза: 3 ожидания + финальные? Проверим >= 3
	if n := countCalls(m.calls, "devices"); n < 3 {
		t.Errorf("devices вызван %d раз, want >= 3", n)
	}
}

func TestCheckCancelledContext(t *testing.T) {
	m := newMock()
	m.devices = devUnauth()
	m.UnauthWait = time.Second // длинное ожидание, чтобы успеть отменить
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_ = m.Check(ctx) // любой исход — главное, не зависнуть навсегда
}

func TestUID(t *testing.T) {
	m := newMock()
	m.execFn = func(_ context.Context, name string, args ...string) (string, string, int) {
		key := keyOf(name, args...)
		switch key {
		case "shell id -u":
			return "0\n", "", 0
		case "devices":
			return devDevice(), "", 0
		}
		return "", "", 0
	}
	uid, raw, err := m.UID(context.Background())
	if err != nil {
		t.Fatalf("UID: %v", err)
	}
	if uid != 0 || raw != "0" {
		t.Errorf("UID = %d (%q), want 0", uid, raw)
	}
}

func TestResultOK(t *testing.T) {
	tests := []struct {
		r    Result
		want bool
	}{
		{Result{Code: 0}, true},
		{Result{Code: 1}, false},
		{Result{Code: 0, Err: context.Canceled}, false},
		{Result{Code: 255}, false},
	}
	for i, tt := range tests {
		if got := tt.r.OK(); got != tt.want {
			t.Errorf("test %d: OK()=%v want %v", i, got, tt.want)
		}
	}
}

func TestShellAndShellOut(t *testing.T) {
	m := newMock()
	m.execFn = func(_ context.Context, name string, args ...string) (string, string, int) {
		key := keyOf(name, args...)
		switch key {
		case "shell getprop ro.build.version.release":
			return "13\n", "", 0
		case "shell cmd overlay list --user 0":
			return "  [x] com.deepal.translate.rro.camera\n", "", 0
		default:
			return "", "", 0
		}
	}
	rel := m.ShellOut(context.Background(), "getprop ro.build.version.release")
	if rel.Out != "13" || !rel.OK() {
		t.Errorf("ShellOut(release) = %q rc=%d, want \"13\" ok", rel.Out, rel.Code)
	}
	list := m.Shell(context.Background(), "cmd overlay list --user 0")
	if !strings.Contains(list.Out, "com.deepal.translate.rro.camera") {
		t.Errorf("Shell(overlay list) = %q", list.Out)
	}
}

func countCalls(calls []string, what string) int {
	n := 0
	for _, c := range calls {
		if c == what {
			n++
		}
	}
	return n
}

func contains(calls []string, what string) bool {
	for _, c := range calls {
		if c == what {
			return true
		}
	}
	return false
}

// ---- поиск бинарника adb: env DEEPL_ADB -> PATH -> рядом с бинарником ----

// resolveBinTest — Adb с точкой подмены selfBin (тесты).
func resolveBinTest(t *testing.T, self string) *Adb {
	t.Helper()
	a := New()
	a.selfBin = func() (string, error) {
		if self == "" {
			return "", os.ErrNotExist
		}
		return self, nil
	}
	return a
}

func TestResolveBinExplicitPath(t *testing.T) {
	t.Setenv("DEEPL_ADB", "/usr/bin/adb")
	a := resolveBinTest(t, "")
	if err := a.ResolveBin(); err != nil {
		t.Fatalf("ResolveBin: %v", err)
	}
	if a.Bin != "/usr/bin/adb" {
		t.Errorf("Bin=%q, want /usr/bin/adb (env DEEPL_ADB приоритетен)", a.Bin)
	}
}

func TestResolveBinExplicitPathMissing(t *testing.T) {
	t.Setenv("DEEPL_ADB", "/нет/такого/adb")
	a := resolveBinTest(t, "")
	if err := a.ResolveBin(); err == nil {
		t.Fatal("ResolveBin: env DEEPL_ADB указывает на несуществующий файл -> ожидаю ошибку")
	}
}

// fakeRoot — фейковый корень: self=.../fake/deepl, рядом adb; переменная
// PATH в тесте очищена (adb из PATH не подменяется — проверяется статикой).
func TestResolveBinNextToBinary(t *testing.T) {
	root := t.TempDir()
	self := filepath.Join(root, "deepl")
	os.WriteFile(self, []byte("x"), 0o700)
	adbName := "adb"
	if runtime.GOOS == "windows" {
		adbName = "adb.exe"
	}
	adbPath := filepath.Join(root, adbName)
	os.WriteFile(adbPath, []byte("x"), 0o700)

	t.Setenv("PATH", filepath.Join(root, "empty")) // adb из PATH недоступен
	a := resolveBinTest(t, self)
	t.Setenv("DEEPL_ADB", "")
	if err := a.ResolveBin(); err != nil {
		t.Fatalf("ResolveBin: %v", err)
	}
	if a.Bin != adbPath {
		t.Errorf("Bin=%q, want %q (adb рядом с бинарником)", a.Bin, adbPath)
	}
}

func TestResolveBinNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())                // пустой PATH
	a := resolveBinTest(t, t.TempDir()+"/deepl") // каталог без adb рядом
	if err := a.ResolveBin(); err == nil {
		t.Fatal("ResolveBin: adb нет нигде -> ожидаю ошибку с подсказкой")
	}
}

// ---- checkResult: общий старт проверки (баннер и [R]) ----

func TestCheckResultFast(t *testing.T) {
	ctx := context.Background()

	m := newMock()
	m.devices = devDevice()
	res := m.CheckResult(ctx, true)
	if !res.OK {
		t.Error("CheckResult(fast, device).OK=false, want true")
	}
	if !strings.Contains(m.logBuf.String(), "[OK] ADB connected.") {
		t.Errorf("нет строки '[OK] ADB connected.': %q", m.logBuf.String())
	}
	m.devices = "List of devices attached\n"
	res = m.CheckResult(ctx, true)
	if res.OK {
		t.Error("CheckResult(fast, none).OK=true, want false")
	}
	// ГУ автомобиля: советы «Для разработчиков»/«АВТОРИЗОВАТЬ» неактуальны
	// (нет доступа к настройкам) — подсказка: кабель + инженерное меню/adbauth.
	for _, bad := range []string{"Для разработчиков", "АВТОРИЗОВАТЬ", "USB-отладка"} {
		if strings.Contains(m.logBuf.String(), bad) {
			t.Errorf("fast(none) советует недоступное на ГУ: %q:\n%s", bad, m.logBuf.String())
		}
	}
	if !strings.Contains(m.logBuf.String(), "инженерное меню") {
		t.Errorf("fast(none): нет подсказки про инженерное меню:\n%s", m.logBuf.String())
	}
	if !strings.Contains(m.logBuf.String(), "adbauth") {
		t.Errorf("fast(none): нет подсказки про adbauth:\n%s", m.logBuf.String())
	}

	// unauth: ADB на ГУ уже активно, но публичный ключ adb этого ПК не
	// добавлен — совет: adbauth (добавить ключ ноутбука), а не «включите ADB».
	m.devices = devUnauth()
	res = m.CheckResult(ctx, true)
	if res.OK {
		t.Error("CheckResult(fast, unauth).OK=true, want false")
	}
	if !strings.Contains(m.logBuf.String(), "публичный ключ adb") {
		t.Errorf("fast(unauth): нет подсказки про публичный ключ adb:\n%s", m.logBuf.String())
	}
	if !strings.Contains(m.logBuf.String(), "adbauth") {
		t.Errorf("fast(unauth): нет подсказки про adbauth:\n%s", m.logBuf.String())
	}

	m.available = false
	res = m.CheckResult(ctx, true)
	if res.OK {
		t.Error("CheckResult(no adb).OK=true, want false")
	}
	if res.Err == nil || !strings.Contains(res.Err.Error(), "не найден") {
		t.Errorf("Err без подсказки о поиске: %v", res.Err)
	}
	if !strings.Contains(m.logBuf.String(), "DEEPL_ADB") {
		t.Errorf("в логе нет подсказки про DEEPL_ADB: %q", m.logBuf.String())
	}
}

func TestCheckResultFullUsesLog(t *testing.T) {
	m := newMock()
	m.devices = devDevice()
	res := m.CheckResult(context.Background(), false)
	if !res.OK {
		t.Error("CheckResult(full, device).OK=false")
	}
	// полная проверка пишет про «попытку» (в отличие от fast)
	if !strings.Contains(m.logBuf.String(), "попытка 1/3") {
		t.Errorf("нет строки 'попытка 1/3' (full): %q", m.logBuf.String())
	}
}

// CheckFast — баннер меню: МОЛЧАЛИВ (строки прогресса дублировали бы
// строку баннера "ADB: [OK] ..."; симптом 27.10: «[OK] ADB connected.
// печатался под каждым меню»).
func TestCheckFastSilent(t *testing.T) {
	ctx := context.Background()
	m := newMock()
	m.devices = devDevice()
	if got := m.CheckFast(ctx); got != true {
		t.Fatalf("CheckFast(device)=%v", got)
	}
	m.devices = "List of devices attached\n"
	_ = m.CheckFast(ctx)
	m.devices = devUnauth()
	_ = m.CheckFast(ctx)
	m.available = false
	_ = m.CheckFast(ctx)
	if m.logBuf.Len() != 0 {
		t.Errorf("CheckFast печатает строки (дубль баннера): %q", m.logBuf.String())
	}
}
