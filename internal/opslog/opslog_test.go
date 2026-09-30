package opslog

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

var tsRe = regexp.MustCompile(`^\d{14}$`)

func TestOpenWritesHeader(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 29, 9, 5, 3, 0, time.Local)
	ol, err := Open(dir, "install", "8", "dynamic", now)
	if err != nil {
		t.Fatal(err)
	}
	defer ol.Close(now, 0, 0, 0, 0)

	// имя файла: ops_install_<14 цифр>.log
	base := filepath.Base(ol.Path())
	if !strings.HasPrefix(base, "ops_install_") || !strings.HasSuffix(base, ".log") {
		t.Fatalf("имя файла: %q", base)
	}
	ts := strings.TrimSuffix(strings.TrimPrefix(base, "ops_install_"), ".log")
	if !tsRe.MatchString(ts) {
		t.Fatalf("таймстемп %q не 14 цифр", ts)
	}
	if ts != "20260929090503" {
		t.Errorf("ts = %q, want 20260929090503", ts)
	}

	data, _ := os.ReadFile(ol.Path())
	got := string(data)
	if !strings.Contains(got, "mode=install") || !strings.Contains(got, "preset=8") ||
		!strings.Contains(got, "schema=dynamic") || !strings.Contains(got, "начало") {
		t.Errorf("шапка не верна: %q", got)
	}
	// В имени И в шапке не должно быть ':' (баг батников OTST:=_.log)
	if strings.Contains(got, ":=") {
		t.Errorf("найдено ':=' в логе — баг батников вернулся: %q", got)
	}
}

func TestWriteLinesAndClose(t *testing.T) {
	dir := t.TempDir()
	now := time.Unix(1750000000, 0)
	ol, _ := Open(dir, "disable", "A", "static", now)
	ol.Begin("[DISABLE] Camera [com.deepal.translate.rro.camera]")
	ol.Db("cmd overlay disable ok")
	ol.Root("uid=2000 - adb root...")
	ol.Result("OK")
	ol.Close(now, 5, 4, 1, 0)

	data, _ := os.ReadFile(ol.Path())
	got := string(data)
	for _, want := range []string{
		"---- [DISABLE] Camera [com.deepal.translate.rro.camera]",
		"  db: cmd overlay disable ok",
		"  [ROOT] uid=2000 - adb root...",
		"  RESULT: OK",
		"конец: всего=5 ok=4 err=1 нет_apk=0",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("нет строки %q в: %q", want, got)
		}
	}
}

func TestNilReceiverNoPanic(t *testing.T) {
	var ol *OpsLog
	ol.WriteLine("x")
	ol.Root("x")
	ol.Result("x")
	ol.Begin("x")
	ol.Close(time.Now(), 0, 0, 0, 0)
	if ol.Path() != "" {
		t.Error("nil.Path() должно быть ''")
	}
}

func TestLastOpsFiles(t *testing.T) {
	dir := t.TempDir()
	// чужие файлы не трогаем
	os.WriteFile(filepath.Join(dir, "other.log"), []byte("x"), 0o644)
	os.Mkdir(filepath.Join(dir, "ops_install_20260101000000.log"), 0o755) // папка — не берём
	os.WriteFile(filepath.Join(dir, "ops_fakelog"), []byte("x"), 0o644)   // без .log — не берём

	f1 := filepath.Join(dir, "ops_install_old.log")
	f2 := filepath.Join(dir, "ops_enable_newer.log")
	f3 := filepath.Join(dir, "ops_uninstall_newest.log")
	for _, p := range []string{f1, f2, f3} {
		os.WriteFile(p, []byte("x"), 0o644)
	}
	// устанавливаем разные mtime: f1 самый старый
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.Local)
	t2 := time.Date(2026, 2, 1, 0, 0, 0, 0, time.Local)
	t3 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.Local)
	os.Chtimes(f1, t1, t1)
	os.Chtimes(f2, t2, t2)
	os.Chtimes(f3, t3, t3)

	got := LastOpsFiles(dir, 2)
	if len(got) != 2 {
		t.Fatalf("LastOpsFiles(2) = %d файлов: %v", len(got), got)
	}
	if filepath.Base(got[0]) != "ops_uninstall_newest.log" {
		t.Errorf("самый свежий должен быть первым: %v", got)
	}
	if filepath.Base(got[1]) != "ops_enable_newer.log" {
		t.Errorf("второй: %v", got)
	}
	if !strings.HasSuffix(got[0], filepath.Join(dir, "ops_uninstall_newest.log")) {
		t.Errorf("полные пути: %v", got)
	}
}
