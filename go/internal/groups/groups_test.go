package groups

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDynamicConfig(t *testing.T) {
	c := Dynamic()
	if c.Prefix != "com.android.vendor.translate.rro." {
		t.Errorf("Prefix = %q", c.Prefix)
	}
	if c.ApkDir != "apks_rro_min" {
		t.Errorf("ApkDir = %q", c.ApkDir)
	}
	if c.Filter != "com.android.vendor" {
		t.Errorf("Filter = %q", c.Filter)
	}
	// EXCLUDE9 — ровно 9
	if len(c.Exclude) != 9 {
		t.Errorf("len(Exclude) = %d, want 9: %v", len(c.Exclude), c.Exclude)
	}
	// TOP-8
	if len(c.Top8) != 8 {
		t.Errorf("len(Top8) = %d, want 8", len(c.Top8))
	}
	// GROUP1 dynamic НЕ содержит NetworkStack (исключён)
	for _, g := range c.Groups {
		for _, m := range g {
			if m == "NetworkStack" {
				t.Errorf("GROUP dynamic содержит NetworkStack — он должен быть в EXCLUDE9")
			}
		}
	}
	if c.OverlayBase != "" {
		t.Errorf("dynamic.OverlayBase = %q, want ''", c.OverlayBase)
	}
}

func TestStaticConfig(t *testing.T) {
	c := Static()
	if c.Prefix != "com.deepal.translate.rro." {
		t.Errorf("Prefix = %q", c.Prefix)
	}
	if c.ApkDir != "apks_rro_static" {
		t.Errorf("ApkDir = %q", c.ApkDir)
	}
	if c.OverlayBase != "/vendor/overlay" {
		t.Errorf("OverlayBase = %q", c.OverlayBase)
	}
	if c.Filter != "com.deepal.translate" {
		t.Errorf("Filter = %q", c.Filter)
	}
	if len(c.Exclude) != 0 {
		t.Errorf("static Exclude = %v, want пусто", c.Exclude)
	}
	// GROUP1 static = dynamic + 9 AOSP-target
	g1 := c.Groups[0]
	found := map[string]bool{}
	for _, m := range g1 {
		found[m] = true
	}
	for _, e := range []string{"NetworkStack", "MediaProviderLegacy", "UserDictionaryProvider", "DownloadProvider", "DownloadProviderUi", "CompanionDeviceManager", "MtpService", "CaptivePortalLogin", "ContactsProvider"} {
		if !found[e] {
			t.Errorf("static GROUP1 не содержит %s", e)
		}
	}
}

func TestResolvePreset(t *testing.T) {
	c := Dynamic()

	got, p, ok := c.ResolvePreset("1")
	if !ok || p != "1" || !reflect.DeepEqual(got, c.Groups[0]) {
		t.Errorf("preset 1: %v %v %v", got, p, ok)
	}

	got, _, ok = c.ResolvePreset("8")
	if !ok || !reflect.DeepEqual(got, c.Top8) {
		t.Errorf("preset 8: %v", got)
	}

	got, _, ok = c.ResolvePreset("A")
	if !ok {
		t.Fatal("preset A: !ok")
	}
	wantAll := c.All()
	if !reflect.DeepEqual(got, wantAll) {
		t.Errorf("preset A != All(): %v / %v", got, wantAll)
	}

	got, _, ok = c.ResolvePreset("C")
	if !ok {
		t.Fatal("preset C: !ok")
	}
	wantC := append(append([]string{}, c.Groups[1]...), c.Groups[2]...)
	if !reflect.DeepEqual(got, wantC) {
		t.Errorf("preset C: %v", got)
	}

	got, _, ok = c.ResolvePreset("0")
	if ok || got != nil {
		t.Errorf("preset 0: %v %v, want !ok", got, ok)
	}

	got, _, ok = c.ResolvePreset("")
	if ok || got != nil {
		t.Errorf("preset '': %v %v, want !ok", got, ok)
	}

	// "bogus" — не пресет, но допустимое имя пакета -> цель ["bogus"]
	got, _, ok = c.ResolvePreset("bogus")
	if !ok || len(got) != 1 || got[0] != "bogus" {
		t.Errorf("preset bogus: %v %v, want [bogus]", got, ok)
	}
}

func TestResolvePresetArbitraryNames(t *testing.T) {
	c := Dynamic()

	// произвольный регистр известного имени -> канонический (для пути к APK)
	got, _, ok := c.ResolvePreset("wt_wtsystemui")
	if !ok || len(got) != 1 || got[0] != "WT_WtSystemUI" {
		t.Errorf("wt_wtsystemui -> %v %v, want [WT_WtSystemUI]", got, ok)
	}

	// несколько имён через пробел + дубли
	got, _, ok = c.ResolvePreset("Camera camera AdayoDvr")
	if !ok || len(got) != 2 {
		t.Errorf("Camera camera AdayoDvr -> %v %v, want [Camera AdayoDvr]", got, ok)
	}
}

func TestResolvePresetCaseInsensitive(t *testing.T) {
	c := Dynamic()
	got, _, ok := c.ResolvePreset("a")
	if !ok || len(got) != len(c.All()) {
		t.Errorf("preset 'a' (нижний регистр) не работает: %v %v", got, ok)
	}
}

func TestPackage(t *testing.T) {
	c := Dynamic()
	if c.Package("WT_Launcher") != "com.android.vendor.translate.rro.wt_launcher" {
		t.Errorf("Package(WT_Launcher) = %q", c.Package("WT_Launcher"))
	}
	c2 := Static()
	if c2.Package("Camera") != "com.deepal.translate.rro.camera" {
		t.Errorf("static Package(Camera) = %q", c2.Package("Camera"))
	}
}

func TestApkPath(t *testing.T) {
	c := Dynamic()
	if got := c.ApkPath("WT_Launcher"); got != "apks_rro_min/WT_Launcher_RRO.apk" && filepath.Separator != '/' {
		t.Errorf("ApkPath = %q", got)
	}
}

func TestAllNoDupes(t *testing.T) {
	c := Dynamic()
	all := c.All()
	seen := map[string]bool{}
	dupes := 0
	for _, m := range all {
		if seen[m] {
			dupes++
		}
		seen[m] = true
	}
	if dupes != 0 {
		t.Errorf("All(): дублей = %d", dupes)
	}
}

func TestAutoTargets(t *testing.T) {
	c := Dynamic()
	dir := t.TempDir()
	c.ApkDir = dir
	// создаём apk, включая EXCLUDE9
	for _, n := range []string{"WT_Launcher", "NetworkStack", "Camera"} {
		if err := os.WriteFile(filepath.Join(dir, n+"_RRO.apk"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(dir, "not_rro.txt"), []byte("x"), 0o644)
	got := c.AutoTargets()
	want := []string{"Camera", "WT_Launcher"} // NetworkStack исключён
	if !reflect.DeepEqual(got, want) {
		t.Errorf("AutoTargets = %v, want %v", got, want)
	}
}

func TestAutoTargetsStaticIncludesAll(t *testing.T) {
	c := Static()
	dir := t.TempDir()
	c.ApkDir = dir
	for _, n := range []string{"NetworkStack", "Camera"} {
		os.WriteFile(filepath.Join(dir, n+"_RRO.apk"), []byte("x"), 0o644)
	}
	got := c.AutoTargets()
	want := []string{"Camera", "NetworkStack"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("static AutoTargets = %v, want %v", got, want)
	}
}
