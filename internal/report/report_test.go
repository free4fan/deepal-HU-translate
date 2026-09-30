package report

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"deepal-hu-translate/internal/groups"
)

func dynamicTestCfg() *groups.Config { return groups.Dynamic() }

// ---- PerPackage ----

func TestPkgVersionClassicSpace(t *testing.T) {
	// "package:X versionCode:N" — граница хвостового пробела (fota vs fotaservice)
	out := "package:com.x.rro.fota versionCode:1\npackage:com.x.rro.fotaservice versionCode:7\n"
	v, ok := pkgVersion(splitLines(out), "com.x.rro.fota")
	if !ok || v != "1" {
		t.Errorf("fota: %q %v, want 1/true", v, ok)
	}
	v, ok = pkgVersion(splitLines(out), "com.x.rro.fotaservice")
	if !ok || v != "7" {
		t.Errorf("fotaservice: %q %v, want 7/true", v, ok)
	}
}

func TestPkgVersionEquals(t *testing.T) {
	out := "package:com.x.rro.camera=versionCode:42\n"
	v, ok := pkgVersion(splitLines(out), "com.x.rro.camera")
	if !ok || v != "42" {
		t.Errorf("equals-формат: %q %v, want 42/true", v, ok)
	}
}

func TestPkgVersionNoVersion(t *testing.T) {
	out := "package:com.x.rro.ldm\n"
	v, ok := pkgVersion(splitLines(out), "com.x.rro.ldm")
	if !ok || v != "?" {
		t.Errorf("без версии: %q %v, want ?/true", v, ok)
	}
}

func TestPkgVersionMissing(t *testing.T) {
	v, ok := pkgVersion(splitLines("package:com.x.other versionCode:3\n"), "com.x.rro.x")
	if ok || v != "?" {
		t.Errorf("отсутствует: %q %v, want ?/false", v, ok)
	}
}

func TestOverlayStateMarkers(t *testing.T) {
	out := "  [x] com.x.rro.camera (com.android.camera)\n" +
		"  [ ] com.x.rro.laucher (com.x)\n" +
		"  --- com.x.rro.broken (com.y)\n" +
		"  [x] com.android.systemui"
	lines := splitLines(out)
	if s := overlayState(lines, "com.x.rro.camera"); s != StateOn {
		t.Errorf("camera: %q", s)
	}
	if s := overlayState(lines, "com.x.rro.laucher"); s != StateOff {
		t.Errorf("launcher: %q", s)
	}
	if s := overlayState(lines, "com.x.rro.broken"); s != StateErr {
		t.Errorf("broken: %q", s)
	}
	// пакет В КОНЦЕ строки (прежний формат Android)
	if s := overlayState(lines, "com.android.systemui"); s != StateOn {
		t.Errorf("systemui (конец строки): %q", s)
	}
	if s := overlayState(lines, "com.z.absent"); s != StateNA {
		t.Errorf("absent: %q", s)
	}
}

func TestOverlayStateErrPriority(t *testing.T) {
	// "---" выигрывает даже если "[x]" где-то рядом (формат: маркер в начале)
	out := "  --- com.x.rro.both\n"
	if s := overlayState(splitLines(out), "com.x.rro.both"); s != StateErr {
		t.Errorf("err priority: %q", s)
	}
}

func TestPerPackage(t *testing.T) {
	cfg := dynamicTestCfg()
	pkgsOut := "package:" + cfg.Prefix + "camera versionCode:3\n" +
		"package:" + cfg.Prefix + "fota versionCode:1\n" +
		"package:" + cfg.Prefix + "fotaservice versionCode:2\n"
	ovlOut := "  [x] " + cfg.Prefix + "camera (com.android.camera)\n" +
		"  --- " + cfg.Prefix + "fota\n" +
		"  [ ] " + cfg.Prefix + "fotaservice\n"
	targets := []string{"Camera", "Fota", "fotaservice", "NonExistent"}
	got := PerPackage(targets, cfg.Prefix, pkgsOut, ovlOut)
	if len(got) != 4 {
		t.Fatalf("len = %d", len(got))
	}
	want := map[string]Pkg{
		"Camera":      {Name: "Camera", Pkg: cfg.Prefix + "camera", Version: "3", Installed: true, Overlay: StateOn},
		"Fota":        {Name: "Fota", Pkg: cfg.Prefix + "fota", Version: "1", Installed: true, Overlay: StateErr},
		"fotaservice": {Name: "fotaservice", Pkg: cfg.Prefix + "fotaservice", Version: "2", Installed: true, Overlay: StateOff},
		"NonExistent": {Name: "NonExistent", Pkg: cfg.Prefix + "nonexistent", Version: "?", Installed: false, Overlay: StateNA},
	}
	for _, p := range got {
		w, ok := want[p.Name]
		if !ok {
			t.Errorf("лишний %q", p.Name)
			continue
		}
		delete(want, p.Name)
		if p != w {
			t.Errorf("%s: got %+v want %+v", p.Name, p, w)
		}
	}
	for n, w := range want {
		t.Errorf("не найдено %q (want %+v)", n, w)
	}
}

// ---- CountFatal / FilterLines / FindProps ----

func TestCountFatal(t *testing.T) {
	s := "line1\nFATAL EXCEPTION: main\nE AndroidRuntime: x\nFATAL EXCEPTION: other\nok\n"
	if n := CountFatal(s); n != 2 {
		t.Errorf("CountFatal = %d, want 2", n)
	}
	if n := CountFatal(""); n != 0 {
		t.Errorf("CountFatal(пусто) = %d", n)
	}
}

func TestFilterLines(t *testing.T) {
	s := "a overlay b\nc idmap d\ne com.x pkg\nplain line\nOVERLAY upper\n"
	got := FilterLines(s, []string{"overlay", "idmap"})
	if !strings.Contains(got, "a overlay b") || !strings.Contains(got, "c idmap d") ||
		!strings.Contains(got, "OVERLAY upper") {
		t.Errorf("FilterLines пропустил строки: %q", got)
	}
	if strings.Contains(got, "plain line") || strings.Contains(got, "com.x pkg") {
		t.Errorf("FilterLines взял лишнее: %q", got)
	}
}

func TestFindProps(t *testing.T) {
	dev := "ro.product.model=Deepal S07\nro.build.version.release=13\nro.build.fingerprint=x/y\n"
	props := FindProps(dev, "ro.product.model", "ro.build.version.release", "ro.build.fingerprint")
	if props[0] != "ro.product.model=Deepal S07" || props[1] != "ro.build.version.release=13" || props[2] != "ro.build.fingerprint=x/y" {
		t.Errorf("FindProps = %v", props)
	}
	m2 := FindProps("", "ro.product.model")[0]
	if m2 != "?" {
		t.Errorf("пустой -> %q, want ?", m2)
	}
}

// ---- Вердикты ----

func TestVerdict(t *testing.T) {
	tests := []struct {
		name string
		s    Stats
		want string
	}{
		{"all ok", Stats{}, "ВСЕ В ПОРЯДКЕ: все пакеты установлены, оверлеи включены, FATAL-сбоев нет."},
		{"miss 9", Stats{Miss: 9}, "ОЖИДАЕМО: 9 AOSP-target оверлеев не ставятся через adb install (NetworkStack/Providers/…); остальные установлены"},
		{"miss 1", Stats{Miss: 1}, "ПРОБЛЕМА: 1 пакет(ов) НЕ УСТАНОВЛЕНО, см. raw/30_per_package.txt"},
		{"off", Stats{Off: 2}, "ПРОБЛЕМА: 2 оверлей(ей) выключен, см. raw/30_per_package.txt"},
		{"err", Stats{Err: 1}, "ПРОБЛЕМА: 1 оверлей(ей) в ошибке ---, см. raw/45_overlay_dump.txt"},
		{"fatal", Stats{Fatal: 3}, "ПРОБЛЕМА: найдено FATAL EXCEPTION: 3, см. raw/40_crash.txt"},
		{"anr", Stats{Anr: 2}, "ПРОБЛЕМА: найдено ANR: 2, см. raw/42_anr.txt"},
		// приоритет: miss > off > err > fatal > anr
		{"miss wins over fatal", Stats{Miss: 1, Fatal: 5}, "ПРОБЛЕМА: 1 пакет(ов) НЕ УСТАНОВЛЕНО, см. raw/30_per_package.txt"},
	}
	for _, tt := range tests {
		if got := Verdict(tt.s); got != tt.want {
			t.Errorf("%s: %q want %q", tt.name, got, tt.want)
		}
	}
}

// ---- FormatPerPackage ----

func TestFormatPerPackage(t *testing.T) {
	cfg := dynamicTestCfg()
	pkgs := []Pkg{
		{Name: "Camera", Pkg: cfg.Prefix + "camera", Version: "3", Installed: true, Overlay: StateOn},
		{Name: "Gone", Pkg: cfg.Prefix + "gone", Version: "?", Installed: false, Overlay: StateNA},
	}
	out := FormatPerPackage(cfg, pkgs)
	for _, s := range []string{
		"com.android.vendor.translate.rro.camera | ver=3 | overlay=[x]",
		"| НЕТ | overlay=? <=== НЕ УСТАНОВЛЕН",
		"Сводка: всего=2 установлено=1 отсутствует=1",
		"Overlay: [x]=1 [ ]=0 ---=0 в списке нет=1",
	} {
		if !strings.Contains(out, s) {
			t.Errorf("нет %q\nв:\n%s", s, out)
		}
	}
}

// ---- FormatSummary ----

func TestFormatSummary(t *testing.T) {
	s := Stats{Total: 10, Abs: 1, Miss: 9, On: 0, Off: 0, Err: 0, NA: 0, Fatal: 0, Anr: 0, VendorDirs: 42}
	out := FormatSummary("static", "2026-09-29_090503", "m", "13", "f", Verdict(s), "ru-RU", s)
	for _, s := range []string{
		"Дата: 2026-09-29_090503",
		"ОТСУТСТВУЮТ:            9  <=== ПРОБЛЕМА",
		"(это 9 AOSP-target оверлеев",
		"Статических оверлеев в /vendor/overlay: 42",
		"ОЖИДАЕМО:",
	} {
		if !strings.Contains(out, s) {
			t.Errorf("summary: нет %q", s)
		}
	}
	// dynamic: без строки /vendor
	if strings.Contains(FormatSummary("dynamic", "t", "?", "?", "?", "v", "l", Stats{}), "vendor/overlay: 0") {
		t.Error("dynamic не должен содержать vendor-строку")
	}
}

// ---- ZipDir ----

func TestZipDir(t *testing.T) {
	dir := t.TempDir()
	rdir := filepath.Join(dir, "report_2026-01-01_000000")
	raw := filepath.Join(rdir, "raw")
	if err := os.MkdirAll(raw, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(raw, "01.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rdir, "summary.txt"), []byte("s"), 0o644); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(dir, "out.zip")
	if err := ZipDir(rdir, zipPath); err != nil {
		t.Fatalf("ZipDir: %v", err)
	}
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer zr.Close()
	names := map[string]bool{}
	for _, f := range zr.File {
		names[filepath.Base(f.Name)] = true
	}
	if !names["01.txt"] || !names["summary.txt"] {
		t.Errorf("в zip нет файлов: %v", names)
	}
}

// ---- countAnr / validDirs ----

func TestValidDirs(t *testing.T) {
	out := "/vendor/overlay/com.x.a\nls: cannot access '/vendor/overlay/com.y': No such file or directory\n\n"
	got := validDirs(out)
	if len(got) != 1 || got[0] != "/vendor/overlay/com.x.a" {
		t.Errorf("validDirs = %v", got)
	}
}

func TestToLower(t *testing.T) {
	if toLower("WT_Launcher") != "wt_launcher" {
		t.Errorf("toLower = %q", toLower("WT_Launcher"))
	}
}
