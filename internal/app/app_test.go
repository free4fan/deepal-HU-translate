package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"deepal-hu-translate/internal/adb"
	"deepal-hu-translate/internal/groups"
	"deepal-hu-translate/internal/opslog"
)

// mockClient — полный мок adb.Client для тестов app.
type mockClient struct {
	calls      []string
	uid        int
	uidAfter   int // после root
	deviceOK   bool
	installOut string
	rootErr    bool
	remountOK  bool
}

func keyOf(args ...string) string { return strings.Join(args, " ") }

func (m *mockClient) Shell(ctx context.Context, args ...string) adb.Result {
	k := "shell " + keyOf(args...)
	m.calls = append(m.calls, k)
	switch k {
	case "shell id -u":
		return adb.Result{Out: itoa(m.uid) + "\n", Code: 0}
	default:
		return adb.Result{Out: "", Code: 0}
	}
}

func (m *mockClient) ShellOut(ctx context.Context, args ...string) adb.Result {
	m.calls = append(m.calls, "shellout "+keyOf(args...))
	return adb.Result{Out: ""}
}

func (m *mockClient) Push(ctx context.Context, local, remote string) adb.Result {
	m.calls = append(m.calls, "push "+local+" "+remote)
	return adb.Result{Code: 0}
}

func (m *mockClient) InstallR(ctx context.Context, apk string) adb.Result {
	m.calls = append(m.calls, "install -r "+apk)
	if m.installOut == "" {
		return adb.Result{Out: "Performing Streamed Install\nSuccess\n", Code: 0}
	}
	return adb.Result{Out: m.installOut, Code: 0}
}

func (m *mockClient) Uninstall(ctx context.Context, pkg string) adb.Result {
	m.calls = append(m.calls, "uninstall "+pkg)
	return adb.Result{Out: "Success\n"}
}

func (m *mockClient) Root(ctx context.Context) adb.Result {
	m.calls = append(m.calls, "root")
	if m.rootErr {
		return adb.Result{Out: "adbd cannot run as root in production builds", Code: 255}
	}
	m.uid = m.uidAfter
	return adb.Result{Out: "restarting adbd as root"}
}

func (m *mockClient) Reboot(ctx context.Context) adb.Result {
	m.calls = append(m.calls, "reboot")
	return adb.Result{Out: ""}
}

func (m *mockClient) UID(ctx context.Context) (int, string, error) {
	m.calls = append(m.calls, "uid")
	return m.uid, itoa(m.uid), nil
}

func (m *mockClient) WaitDevice(ctx context.Context) error {
	m.calls = append(m.calls, "wait-device")
	return nil
}

func (m *mockClient) KillServer(ctx context.Context) adb.Result {
	m.calls = append(m.calls, "kill-server")
	return adb.Result{}
}
func (m *mockClient) StartServer(ctx context.Context) adb.Result {
	m.calls = append(m.calls, "start-server")
	return adb.Result{}
}
func (m *mockClient) Remount(ctx context.Context) adb.Result {
	if m.remountOK {
		return adb.Result{Code: 0}
	}
	return adb.Result{Code: 1}
}
func (m *mockClient) Available() bool                        { return true }
func (m *mockClient) Version(ctx context.Context) adb.Result { return adb.Result{Out: "adb version x"} }
func (m *mockClient) Check(ctx context.Context) bool {
	m.calls = append(m.calls, "check")
	return m.deviceOK
}
func (m *mockClient) CheckFast(ctx context.Context) bool {
	m.calls = append(m.calls, "check-fast")
	return m.deviceOK
}
func (m *mockClient) DevicesState(ctx context.Context) adb.State {
	if m.deviceOK {
		return adb.StateDevice
	}
	return adb.StateNone
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}

func containsCall(calls []string, what string) bool {
	for _, c := range calls {
		if c == what {
			return true
		}
	}
	return false
}

// containsAPK — был ли вызов "install -r <apk>".
func containsAPK(calls []string) bool {
	for _, c := range calls {
		if strings.HasPrefix(c, "install -r ") {
			return true
		}
	}
	return false
}

// appWithApp — App с буфером вывода (для проверки строк в консоли).
type appWithApp struct {
	*App
	buf *strings.Builder
}

// newAppBuilder — App, пишущая в буфер (подменяемо).
func newAppBuilder(m *mockClient) *appWithApp {
	var sb strings.Builder
	a := New(context.Background(), groups.Dynamic(), m)
	a.Out = &sb
	a.In = nil
	a.Now = func() time.Time { return time.Date(2026, 9, 29, 9, 9, 9, 0, time.Local) }
	a.PmsDelay = 0
	return &appWithApp{App: a, buf: &sb}
}

func openTestOps(dir string) (*opslog.OpsLog, error) {
	return opslog.Open(dir, "enable", "8", "dynamic", time.Now())
}

// ---- installSuccess (регрессия хвостового пробела/CR) ----

func TestInstallSuccess(t *testing.T) {
	cases := map[string]bool{
		"Success\n":                            true,
		"  Success \n":                         true,
		"Success\r\n":                          true,
		"Performing Streamed Install\nSuccess": true,
		"Failure [INSTALL_FAILED_USER_RESTRICTED]": false,
		"Performing Streamed Install\nFailure":     false,
		"success lowercase":                        true,
		"":                                         false,
	}
	for in, want := range cases {
		if got := installSuccess(in); got != want {
			t.Errorf("installSuccess(%q) = %v, want %v", in, got, want)
		}
	}
}

// ---- ensureRoot ----

func TestEnsureRootAlreadyRoot(t *testing.T) {
	m := &mockClient{uid: 0, uidAfter: 0, deviceOK: true}
	a := newAppBuilder(m).App
	ok, prev, cur := a.ensureRoot(nil)
	if !ok || prev != 0 || cur != 0 {
		t.Errorf("ensureRoot: ok=%v prev=%d cur=%d", ok, prev, cur)
	}
	if containsCall(m.calls, "root") {
		t.Error("не должен вызывать adb root, если уже uid=0")
	}
}

func TestEnsureRootSwitches(t *testing.T) {
	m := &mockClient{uid: 2000, uidAfter: 0, deviceOK: true}
	a := newAppBuilder(m).App
	ok, prev, cur := a.ensureRoot(nil)
	if !ok || prev != 2000 || cur != 0 {
		t.Errorf("ensureRoot: ok=%v prev=%d cur=%d", ok, prev, cur)
	}
	if !containsCall(m.calls, "root") {
		t.Error("не вызван adb root")
	}
}

func TestEnsureRootNotSupported(t *testing.T) {
	m := &mockClient{uid: 2000, uidAfter: 2000, deviceOK: true, rootErr: true}
	w := newAppBuilder(m)
	ok, _, cur := w.ensureRoot(nil)
	if ok || cur != 2000 {
		t.Errorf("ensureRoot: ok=%v cur=%d (root не поддерживается)", ok, cur)
	}
	if !strings.Contains(w.buf.String(), "[WARN]") || !strings.Contains(w.buf.String(), "WT_WtSystemUI") {
		t.Errorf("WARN-сообщение не выдалось:\n%s", w.buf.String())
	}
}

func TestEnsureRootWritesOpsLog(t *testing.T) {
	dir := t.TempDir()
	ol, err := openTestOps(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := &mockClient{uid: 2000, uidAfter: 0, deviceOK: true}
	w := newAppBuilder(m)
	w.ensureRoot(ol)
	ol.Close(time.Now(), 0, 0, 0, 0)
	data, _ := os.ReadFile(ol.Path())
	if !strings.Contains(string(data), "[ROOT]") {
		t.Errorf("в ops-логе нет [ROOT]: %q", string(data))
	}
}

// ---- dyn install ----

func setupDyn(t *testing.T) (*groups.Config, *mockClient, *appWithApp) {
	t.Helper()
	dir := t.TempDir()
	cfg := groups.Dynamic()
	apkDir := filepath.Join(dir, "apks")
	os.MkdirAll(apkDir, 0o755)
	cfg.ApkDir = apkDir
	m := &mockClient{uid: 0, deviceOK: true}
	w := newAppBuilder(m)
	w.Cfg = cfg
	w.Logs = filepath.Join(dir, "logs")
	return cfg, m, w
}

func TestRunDynamicInstallOk(t *testing.T) {
	cfg, m, w := setupDyn(t)
	os.WriteFile(filepath.Join(cfg.ApkDir, "Camera_RRO.apk"), []byte("fake-apk"), 0o644)

	o, err := w.Run(ModeInstall, "8")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if o.Total != 8 {
		t.Errorf("Total = %d, want 8", o.Total)
	}
	if o.Ok != 1 {
		t.Errorf("Ok = %d, want 1 (только Camera найдена)", o.Ok)
	}
	if o.Miss != 7 {
		t.Errorf("Miss = %d, want 7", o.Miss)
	}
	// install и enable вызваны для camera
	if !containsCall(m.calls, "install -r "+cfg.ApkPath("Camera")) {
		t.Errorf("install не вызван: %v", m.calls)
	}
	if !containsCall(m.calls, "shell cmd overlay enable --user 0 com.android.vendor.translate.rro.camera") {
		t.Errorf("enable не вызван: %v", m.calls)
	}
	// ops-лог
	ents, _ := os.ReadDir(w.Logs)
	if len(ents) == 0 {
		t.Error("ops-лог не создан")
	}
	if !strings.Contains(w.buf.String(), "Итог: install") {
		t.Errorf("нет summary:\n%s", w.buf.String())
	}
}

func TestRunDynamicInstallFail(t *testing.T) {
	cfg, m, w := setupDyn(t)
	m.installOut = "Failure [INSTALL_FAILED_...]"
	os.WriteFile(filepath.Join(cfg.ApkDir, "Camera_RRO.apk"), []byte("fake-apk"), 0o644)

	o, err := w.Run(ModeInstall, "8")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if o.Ok != 0 || o.Fail != 1 || o.Miss != 7 {
		t.Errorf("счётчики: %+v", o)
	}
}

// установка пакета вне групп: имя пакета как пресет
func TestRunDynamicArbitraryPackage(t *testing.T) {
	cfg, m, w := setupDyn(t)
	// пакет из G2 (не в TOP-8) — как одиночное имя
	os.WriteFile(filepath.Join(cfg.ApkDir, "AdayoDvrLocalService_RRO.apk"), []byte("x"), 0o644)

	o, err := w.Run(ModeInstall, "AdayoDvrLocalService")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if o.Total != 1 || o.Ok != 1 {
		t.Errorf("счётчики: %+v", o)
	}
	if !containsCall(m.calls, "install -r "+cfg.ApkPath("AdayoDvrLocalService")) {
		t.Errorf("install не вызван для произвольного пакета: %v", m.calls)
	}

	// несколько имён через пробел + дубли
	os.WriteFile(filepath.Join(cfg.ApkDir, "AdayoDvr_RRO.apk"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(cfg.ApkDir, "Camera_RRO.apk"), []byte("x"), 0o644)
	m.calls = nil
	o, err = w.Run(ModeEnable, "AdayoDvr Camera AdayoDvr")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if o.Total != 2 {
		t.Errorf("Total = %d, want 2 (дубли убраны)", o.Total)
	}
	if !containsCall(m.calls, "shell cmd overlay enable --user 0 com.android.vendor.translate.rro.adayodvr") {
		t.Errorf("enable adayodvr не вызван: %v", m.calls)
	}
	if !containsCall(m.calls, "shell cmd overlay enable --user 0 com.android.vendor.translate.rro.camera") {
		t.Errorf("enable camera не вызван")
	}
}

func TestRunDynamicEnable(t *testing.T) {
	cfg, m, w := setupDyn(t)
	os.WriteFile(filepath.Join(cfg.ApkDir, "Camera_RRO.apk"), []byte("x"), 0o644)

	o, err := w.Run(ModeEnable, "8")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if o.Total != 8 {
		t.Errorf("Total = %d", o.Total)
	}
	if !containsCall(m.calls, "shell cmd overlay enable --user 0 com.android.vendor.translate.rro.camera") {
		t.Errorf("enable не вызван: %v", m.calls)
	}
}

func TestRunDynamicUninstall(t *testing.T) {
	cfg, m, w := setupDyn(t)
	_ = cfg
	os.WriteFile(filepath.Join(groups.Dynamic().ApkDir, "x"), []byte("x"), 0o644)

	o, err := w.Run(ModeUninstall, "8")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if o.Total != 8 {
		t.Errorf("Total = %d", o.Total)
	}
	// сначала disable, потом uninstall
	if !containsCall(m.calls, "shell cmd overlay disable --user 0 com.android.vendor.translate.rro.camera") {
		t.Errorf("disable не вызван: %v", m.calls)
	}
	if !containsCall(m.calls, "uninstall com.android.vendor.translate.rro.camera") {
		t.Errorf("uninstall не вызван: %v", m.calls)
	}
	// после uninstall — заметка про reboot
	if !strings.Contains(w.buf.String(), "Перезагрузите для применения") {
		t.Errorf("после uninstall нет заметки про reboot:\n%s", w.buf.String())
	}
}

// ---- reboot ----

// Run(ModeReboot, "") — adb reboot без цели/root, без ops-лога.
func TestRunReboot(t *testing.T) {
	cfg, m, w := setupDyn(t)
	_ = cfg

	o, err := w.Run(ModeReboot, "")
	if err != nil {
		t.Fatalf("Run(reboot): %v", err)
	}
	if o.Total != 0 {
		t.Errorf("reboot не должен считать цели, Total=%d", o.Total)
	}
	if !containsCall(m.calls, "reboot") {
		t.Errorf("adb reboot не вызван: %v", m.calls)
	}
	if containsCall(m.calls, "root") {
		t.Error("reboot не должен требовать root")
	}
	out := w.buf.String()
	if !strings.Contains(out, "[REBOOT]") || !strings.Contains(out, "adb reboot") {
		t.Errorf("нет сообщения о перезагрузке:\n%s", out)
	}
}

// после динамического install тоже показывается заметка про reboot.
func TestRunInstallRebootNote(t *testing.T) {
	cfg, m, w := setupDyn(t)
	_ = m
	os.WriteFile(filepath.Join(cfg.ApkDir, "Camera_RRO.apk"), []byte("fake-apk"), 0o644)

	if _, err := w.Run(ModeInstall, "8"); err != nil {
		t.Fatalf("Run(install): %v", err)
	}
	if !strings.Contains(w.buf.String(), "Перезагрузите для применения") {
		t.Errorf("после install нет заметки про reboot:\n%s", w.buf.String())
	}
}

// произвольное имя пакета без APK — это не ошибка, а MISS (нет файла на хосте)
func TestRunArbitraryPkgMissingApk(t *testing.T) {
	cfg, m, w := setupDyn(t)
	_ = cfg

	o, err := w.Run(ModeInstall, "SomePkg")
	if err != nil {
		t.Fatalf("Run: %v (отсутствие APK — это MISS, не ошибка)", err)
	}
	if o.Total != 1 || o.Miss != 1 || o.Ok != 0 {
		t.Errorf("счётчики: %+v", o)
	}
	if containsAPK(m.calls) {
		t.Error("install не должен вызываться без APK на хосте")
	}
	out := w.buf.String()
	if !strings.Contains(out, "[WARN] SomePkg: нет") {
		t.Errorf("нет WARN про отсутствие APK:\n%s", out)
	}
}

// ---- stat ----

func setupStatic(t *testing.T) (*groups.Config, *mockClient, *appWithApp) {
	t.Helper()
	dir := t.TempDir()
	cfg := groups.Static()
	apkDir := filepath.Join(dir, "apks_static")
	os.MkdirAll(apkDir, 0o755)
	cfg.ApkDir = apkDir
	m := &mockClient{uid: 0, deviceOK: true, remountOK: true}
	w := newAppBuilder(m)
	w.Cfg = cfg
	w.Logs = filepath.Join(dir, "logs")
	return cfg, m, w
}

func TestRunStaticInstall(t *testing.T) {
	cfg, m, w := setupStatic(t)
	os.WriteFile(filepath.Join(cfg.ApkDir, "Camera_RRO.apk"), []byte("x"), 0o644)

	o, err := w.Run(ModeInstall, "8")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if o.Total != 8 || o.Ok != 1 {
		t.Errorf("счётчики: %+v", o)
	}
	if !containsCall(m.calls, "shell mkdir -p /vendor/overlay/com.deepal.translate.rro.camera") {
		t.Errorf("mkdir не вызван: %v", m.calls)
	}
	// push — путь может отличаться разделителем; проверяем хвост
	foundPush := false
	for _, c := range m.calls {
		if strings.Contains(c, "push ") && strings.HasSuffix(c, "/vendor/overlay/com.deepal.translate.rro.camera/") {
			foundPush = true
		}
	}
	if !foundPush {
		t.Errorf("push не вызван: %v", m.calls)
	}
}

func TestRunStaticUninstall(t *testing.T) {
	cfg, m, w := setupStatic(t)
	_ = cfg

	o, err := w.Run(ModeUninstall, "8")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if o.Total != 8 {
		t.Errorf("Total = %d", o.Total)
	}
	if !containsCall(m.calls, "shell rm -rf /vendor/overlay/com.deepal.translate.rro.camera") {
		t.Errorf("rm -rf не вызван: %v", m.calls)
	}
}

// ---- status / report / no-adb ----

func TestRunStatus(t *testing.T) {
	m := &mockClient{uid: 0, deviceOK: true}
	w := newAppBuilder(m)
	_, err := w.Run(ModeStatus, "")
	if err != nil {
		t.Fatalf("Run(status): %v", err)
	}
	if !containsCall(m.calls, "shell cmd overlay list --user 0") {
		t.Errorf("overlay list не вызван: %v", m.calls)
	}
}

func TestRunNoAdb(t *testing.T) {
	m := &mockClient{uid: 0, deviceOK: false}
	w := newAppBuilder(m)
	_, err := w.Run(ModeStatus, "")
	if err == nil {
		t.Error("ожидалась ошибка при недоступном adb")
	}
}

func TestRunReport(t *testing.T) {
	dir := t.TempDir()
	cfg := groups.Dynamic()
	cfg.ApkDir = filepath.Join(dir, "apks")
	os.MkdirAll(cfg.ApkDir, 0o755)
	m := &mockClient{uid: 0, deviceOK: true}
	w := newAppBuilder(m)
	w.Cfg = cfg
	w.Logs = dir

	_, err := w.Run(ModeReport, "")
	if err != nil {
		t.Fatalf("Run(report): %v", err)
	}
}

// report без ADB — не запускается (manage.bat: ADB_CHECK до любого mode).
func TestRunReportNoAdb(t *testing.T) {
	m := &mockClient{uid: 0, deviceOK: false}
	w := newAppBuilder(m)
	_, err := w.Run(ModeReport, "")
	if err == nil {
		t.Error("report должен требовать ADB (ожидалась ошибка)")
	}
}
