package report

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"deepal-hu-translate/internal/adb"
	"deepal-hu-translate/internal/groups"
)

// mockAdb — реализация adbIf для сквозного прогона Run.
type mockAdb struct {
	responses       map[string]string
	calls           []string
	rootCalls       int
	vendorAfterRoot string // если != "" — после первого root ls -d видит эти папки
}

func (m *mockAdb) ShellOut(_ context.Context, args ...string) adb.Result {
	full := strings.Join(args, " ")
	m.calls = append(m.calls, full)
	// эмуляция: /vendor/overlay видно только после root
	if m.vendorAfterRoot != "" && m.rootCalls > 0 &&
		strings.HasPrefix(full, "ls -d /vendor/overlay/") {
		return adb.Result{Out: m.vendorAfterRoot}
	}
	if v, ok := m.responses[full]; ok {
		return adb.Result{Out: v}
	}
	return adb.Result{Out: ""}
}

func (m *mockAdb) Root(_ context.Context) adb.Result {
	m.rootCalls++
	return adb.Result{}
}

func (m *mockAdb) Version(_ context.Context) adb.Result {
	return adb.Result{Out: "Android Debug Bridge version 1.0.41"}
}

func TestRunDynamic(t *testing.T) {
	dir := t.TempDir()
	apkDir := filepath.Join(dir, "apks_rro_min")
	os.MkdirAll(apkDir, 0o755)
	os.WriteFile(filepath.Join(apkDir, "Camera_RRO.apk"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(apkDir, "WT_Launcher_RRO.apk"), []byte("y"), 0o644)

	cfg := groups.Dynamic()
	cfg.ApkDir = apkDir

	pfx := cfg.Prefix
	m := &mockAdb{responses: map[string]string{
		"getprop":                            "ro.product.model=Deepal S07\nro.build.version.release=13\nro.build.fingerprint=A/B/1:13\n",
		"uptime":                             " 10:00:00 up 1 day\n",
		"df -h":                              "Filesystem Size Used Avail\n/dev 8G 4G 4G\n",
		"settings get system system_locales": "ru-RU\n",
		"getprop ro.product.locale":          "ru-RU\n",
		"getprop persist.sys.locale":         "null\n",
		"pm list packages --show-versioncode " + cfg.Filter: "package:" + pfx + "camera versionCode:10\n" +
			"package:" + pfx + "wt_launcher versionCode:3\n",
		"cmd overlay list --user 0":                              "  [x] " + pfx + "camera (base)\n" + "  [ ] " + pfx + "wt_launcher (base)\n",
		"logcat -d -b crash -t 500 -v time":                      "01-01 00:00:00.000 E AndroidRuntime: FATAL EXCEPTION: main\n",
		"logcat -d -s AndroidRuntime:E System.err:W *:S -v time": "FATAL EXCEPTION: main\n",
		"logcat -d -b main -t 2000 -v time":                      "overlay: enabled camera\nidmap: ok\nplain line\n",
		"ls -l /data/anr":                                        "",
		"ls /data/anr":                                           "",
		"logcat -d -s IdmapManager:V Idmap:V -v time":            "idmap ok\n",
	}}
	ctx := context.Background()
	var out strings.Builder
	zipPath, summaryPath, stats, err := Run(ctx, Input{
		Cfg: cfg, Adb: m, Now: time.Date(2026, 9, 29, 10, 0, 0, 0, time.Local), LogsDir: dir, Out: &out,
	})
	if err != nil {
		t.Fatalf("Run: %v\n%s", err, out.String())
	}
	if !strings.HasSuffix(zipPath, "deepl_dynamic_report_2026-09-29_100000.zip") {
		t.Errorf("zip имя: %s", zipPath)
	}
	if _, err := os.Stat(zipPath); err != nil {
		t.Fatalf("zip нет: %v", err)
	}
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	byName := map[string]bool{}
	for _, f := range zr.File {
		byName[f.Name] = true
	}
	// проверяем, что все raw файлы внутри карточки
	for _, n := range []string{
		"summary.txt", "00_device.txt", "02_apk_local.txt", "20_packages_ours.txt",
		"25_overlay_all.txt", "30_per_package.txt", "40_crash.txt", "41_fatal.txt", "42_anr.txt",
		"43_main.txt", "44_overlay_main.txt", "45_overlay_dump.txt", "46_idmap.txt",
	} {
		found := false
		for full := range byName {
			if strings.Contains(full, n) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("в zip нет %s", n)
		}
	}
	// dynamic: нет vendor-файла
	for full := range byName {
		if strings.Contains(full, "26_vendor") {
			t.Errorf("dynamic не должен содержать 26_vendor: %s", full)
		}
	}
	// stats: установлены ровно camera+launcher, остальные отсутствуют
	if stats.Abs != 2 || stats.Miss != stats.Total-2 {
		t.Errorf("stats Abs/Miss: %+v", stats)
	}
	if stats.Total == 0 {
		t.Error("Total == 0")
	}
	if stats.On != 1 || stats.Off != 1 {
		t.Errorf("stats On/Off: %+v", stats)
	}
	if stats.Fatal != 1 {
		t.Errorf("Fatal = %d, want 1", stats.Fatal)
	}
	// summary: вердикт PROBLEM (много не установлено)
	data, _ := os.ReadFile(summaryPath)
	s := string(data)
	if !strings.Contains(s, "<=== ПРОБЛЕМА") {
		t.Errorf("summary: нет ПРОБЛЕМА\n%s", s)
	}
	if !strings.Contains(s, "Deepal S07") || !strings.Contains(s, "ru-RU") {
		t.Errorf("summary: нет model/локаль\n%s", s)
	}
	// 30_per_package: строка camera
	pp, _ := os.ReadFile(filepath.Join(dir, "report_2026-09-29_100000", "raw", "30_per_package.txt"))
	if !strings.Contains(string(pp), pfx+"camera | ver=10 | overlay=[x]") {
		t.Errorf("30_per_package: нет camera строки\n%s", string(pp))
	}
	if !strings.Contains(string(pp), pfx+"wt_launcher | ver=3 | overlay=[ ]") {
		t.Errorf("30_per_package: нет wt_launcher\n%s", string(pp))
	}
	// 44_overlay_main: overlay/idmap строки, без plain
	o44, _ := os.ReadFile(filepath.Join(dir, "report_2026-09-29_100000", "raw", "44_overlay_main.txt"))
	s44 := string(o44)
	if !strings.Contains(s44, "overlay: enabled") || !strings.Contains(s44, "idmap: ok") || strings.Contains(s44, "plain line") {
		t.Errorf("44_overlay_main: %q", s44)
	}
	// 45: нет ошибочных -> заглушка
	d45, _ := os.ReadFile(filepath.Join(dir, "report_2026-09-29_100000", "raw", "45_overlay_dump.txt"))
	if !strings.Contains(string(d45), "Оверлеев в ошибке") {
		t.Errorf("45: %q", string(d45))
	}
}

func TestRunStaticVendorRoot(t *testing.T) {
	dir := t.TempDir()
	cfg := groups.Static()
	cfg.ApkDir = filepath.Join(dir, "apks_rro_static")
	os.MkdirAll(cfg.ApkDir, 0o755)

	m := &mockAdb{
		responses: map[string]string{
			"getprop":                            "ro.product.model=Deepal\n",
			"uptime":                             "",
			"df -h":                              "",
			"settings get system system_locales": "null\n",
			"getprop ro.product.locale":          "ru-RU\n",
			"getprop persist.sys.locale":         "",
			"pm list packages --show-versioncode " + cfg.Filter: "",
			"cmd overlay list --user 0":                         "",
			// без root: пусто (ответ вернёт заглушка в ShellOut)
			"ls -d /vendor/overlay/com.deepal.translate.rro.*":       "",
			"logcat -d -b crash -t 500 -v time":                      "",
			"logcat -d -s AndroidRuntime:E System.err:W *:S -v time": "",
			"ls -l /data/anr":                             "",
			"ls /data/anr":                                "",
			"logcat -d -b main -t 2000 -v time":           "",
			"logcat -d -s IdmapManager:V Idmap:V -v time": "",
		},
		vendorAfterRoot: "/vendor/overlay/com.deepal.translate.rro.camera\n" +
			"/vendor/overlay/com.deepal.translate.rro.launcher\n",
	}

	ctx := context.Background()
	var out strings.Builder
	_, _, stats, err := Run(ctx, Input{Cfg: cfg, Adb: m, Now: time.Now(), LogsDir: dir, Out: &out})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if stats.VendorDirs != 2 {
		t.Errorf("VendorDirs = %d, want 2 (после root)", stats.VendorDirs)
	}
	if m.rootCalls == 0 {
		t.Error("root не запрашивался при пустом ls")
	}
	// static: есть 26_vendor_overlay.txt
	found := false
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(dir, e.Name(), "raw", "26_vendor_overlay.txt")
		if _, e2 := os.Stat(p); e2 == nil {
			data, _ := os.ReadFile(p)
			if !strings.Contains(string(data), "папки: 2") {
				t.Errorf("26_vendor: %q", string(data))
			}
			found = true
		}
	}
	if !found {
		t.Error("26_vendor_overlay.txt не найден")
	}
}
