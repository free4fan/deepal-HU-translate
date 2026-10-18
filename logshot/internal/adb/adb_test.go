package adb

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestDevicesStateParsing(t *testing.T) {
	a := New()
	a.CmdTimeout = time.Second
	a.Logger = &strings.Builder{}
	a.SetExecFn(func(ctx context.Context, name string, args ...string) (string, string, int) {
		return "List of devices attached\nABC123\tdevice\nDEF\tunauthorized\n\n", "", 0
	})
	devs := a.Devices(context.Background())
	if len(devs) != 2 {
		t.Fatalf("want 2 devices, got %v", devs)
	}
	if devs[0].Serial != "ABC123" || devs[0].State != "device" {
		t.Errorf("dev0 = %+v", devs[0])
	}
}

func TestStreamFnReceivesLine(t *testing.T) {
	a := New()
	a.Logger = &strings.Builder{}
	var got []string
	a.SetStreamFn(func(ctx context.Context, name string, args []string, lineFn func(string) error) error {
		got = []string{"line1", "line2"}
		for _, l := range got {
			if err := lineFn(l); err != nil {
				return err
			}
		}
		return nil
	})
	ok := a.Stream(context.Background(), func(line string) error {
		return nil
	}, "logcat", "-v", "threadtime")
	if !ok {
		t.Fatal("stream ok=false")
	}
	if len(got) != 2 {
		t.Fatalf("got %v, want 2 lines pushed", got)
	}
}

func TestRunUsesExecFn(t *testing.T) {
	a := New()
	a.CmdTimeout = time.Second
	a.Logger = &strings.Builder{}
	a.SetExecFn(func(ctx context.Context, name string, args ...string) (string, string, int) {
		return "uid-0-out", "", 0
	})
	if got := a.ShellS(context.Background(), "id", "-u"); got != "uid-0-out" {
		t.Errorf("ShellS = %q", got)
	}
}

// Регрессия 04.10: Shell ДОЛЖЕН передать составную команду ОДНИМ аргументом
// `adb shell <cmd>` (device-shell разберёт ;|< 2>/ сам). Было `shell sh -lc
// <cmd>` (4 аргумента) → adb склизал без перекавычивания → `getprop` без
// аргумента = полный дамп (meta locale/camera_pid/camera_apk/scenemode_pid
// битые).
func TestShellPassesCommandAsSingleArg(t *testing.T) {
	a := New()
	a.Logger = &strings.Builder{}
	var gotArgs []string
	a.SetExecFn(func(ctx context.Context, name string, args ...string) (string, string, int) {
		gotArgs = args
		return "v1 | v2", "", 0
	})
	cmd := "getprop persist.sys.locale 2>/dev/null; getprop ro.product.locale 2>/dev/null"
	if got := a.Shell(context.Background(), cmd); got != "v1 | v2" {
		t.Errorf("Shell = %q", got)
	}
	// ровно 2 аргумента: "shell" + ВЕСЬ cmd одним аргументом (никаких sh/-lc)
	if len(gotArgs) != 2 || gotArgs[0] != "shell" || gotArgs[1] != cmd {
		t.Errorf("Shell args = %v, want [shell %q]", gotArgs, cmd)
	}
}

func TestUIDNum(t *testing.T) {
	a := New()
	a.Logger = &strings.Builder{}
	a.SetExecFn(func(ctx context.Context, name string, args ...string) (string, string, int) {
		return "0\n", "", 0
	})
	if u := a.UID(context.Background()); u != 0 {
		t.Errorf("UID=%d, want 0", u)
	}
}
