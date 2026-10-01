package adb

import (
	"context"
	"strings"
	"testing"
	"time"
)

// mock — единая точка подмены: execFn отвечает на все команды,
// availableFn управляет наличием adb, devices — вывод `adb devices`.
type mock struct {
	*Adb
	calls     []string // "devices", "kill-server", "shell id -u", ...
	devices   string   // текущий вывод adb devices
	available bool
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
	var b strings.Builder // тихий логгер
	a.Logger = &b
	_ = b
	m := &mock{Adb: a, devices: "List of devices attached\n", available: true}
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
	if !strings.Contains(m.Logger.String(), "adb not found") {
		t.Errorf("лог не содержит 'adb not found': %q", m.Logger.String())
	}
}

func TestCheckImmediateDevice(t *testing.T) {
	m := newMock()
	m.devices = devDevice()
	if got := m.Check(context.Background()); !got {
		t.Error("Check(device сразу) = false, want true")
	}
	if !strings.Contains(m.Logger.String(), "[OK] ADB connected.") {
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
	log := m.Logger.String()
	if !strings.Contains(log, "НЕ АВТОРИЗОВАНА") {
		t.Errorf("нет сообщения об авторизации: %q", log)
	}
	if !strings.Contains(log, "остановка.") {
		t.Errorf("нет 'остановка.': %q", log)
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
