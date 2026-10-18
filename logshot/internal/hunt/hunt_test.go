package hunt

import (
	"strings"
	"testing"
)

const (
	l0 = "10-02 21:35:12.001 [main] 10-02 21:35:11.998  1234  1240 I System.out: 请检查四周情况,注意安全!"
	l1 = "10-02 21:35:12.002 [crash] 10-02 21:35:11.999  5678  5680 W ActivityManager: something else"
	l2 = "10-02 21:35:12.003 10-02 21:35:12.000  2345  2345 D FDBus: recv msg id=0x1234 len=64"
	l3 = "junk line without format"
)

func TestParseLine(t *testing.T) {
	cases := []struct {
		raw      string
		ok       bool
		pid, tid int
		level    string
		tag      string
		contains string
	}{
		{l0, true, 1234, 1240, "I", "System.out", ""},
		{l1, true, 5678, 5680, "W", "ActivityManager", ""},
		{l2, true, 2345, 2345, "D", "FDBus", ""},
		{l3, false, 0, 0, "", "", ""},
	}
	for _, c := range cases {
		l := ParseLine(c.raw)
		if l.OK != c.ok {
			t.Fatalf("ParseLine(%q).OK = %v, want %v", c.raw, l.OK, c.ok)
		}
		if !l.OK {
			continue
		}
		if l.PID != c.pid || l.TID != c.tid {
			t.Errorf("ParseLine pids = %d/%d, want %d/%d", l.PID, l.TID, c.pid, c.tid)
		}
		if l.Level != c.level || l.Tag != c.tag {
			t.Errorf("ParseLine level/tag = %s/%s, want %s/%s", l.Level, l.Tag, c.level, c.tag)
		}
		if c.contains != "" && !strings.Contains(l.Msg, c.contains) {
			t.Errorf("Msg %q not contain %q", l.Msg, c.contains)
		}
	}
}

func TestSearchBasic(t *testing.T) {
	lines := []string{l0, l1, l2, l3}
	rep := Search(lines, []string{"请检查四周情况", "не_было"})
	if len(rep.Hits) != 1 {
		t.Fatalf("hits = %+v, want 1", rep.Hits)
	}
	h := rep.Hits[0]
	if h.Phrase != "请检查四周情况" || h.Line.PID != 1234 {
		t.Errorf("hit = %+v", h)
	}
	if len(h.Context) != 4 {
		t.Errorf("context = %d lines, want 4 (full clip)", len(h.Context))
	}
	by := rep.ByPhrase("не_было")
	if len(by) != 0 {
		t.Errorf("ByPhrase(не_было) = %+v", by)
	}
}

func TestSearchMultipleHits(t *testing.T) {
	lines := []string{
		"a 10-02 21:00:00.000  1 1 I X: foo bar",
		"a 10-02 21:00:01.000  2 2 I X: foo!",
	}
	rep := Search(lines, []string{"foo"})
	if len(rep.Hits) != 2 {
		t.Fatalf("want 2 hits, got %d", len(rep.Hits))
	}
	if len(rep.Pids) != 2 {
		t.Fatalf("want 2 pids, got %v", rep.Pids)
	}
}

func TestAttrPID(t *testing.T) {
	sh := func(cmd string) string {
		if strings.Contains(cmd, "/proc/1234/cmdline") {
			return "com.wt.airconditioner "
		}
		if strings.Contains(cmd, "/proc/9/cmdline") {
			return ""
		}
		if strings.Contains(cmd, "pm map 9") {
			return "User 0: pid9\ncom.android.system\n"
		}
		if strings.Contains(cmd, "/proc/77/cmdline") {
			return ""
		}
		if strings.Contains(cmd, "pm map 77") {
			return ""
		}
		return ""
	}
	got := AttrPID(sh, []int{1234, 9, 77})
	if got[1234] != "com.wt.airconditioner" {
		t.Errorf("pid 1234 = %q", got[1234])
	}
	if got[9] != "com.android.system" {
		t.Errorf("pid 9 = %q, want com.android.system", got[9])
	}
	if !strings.Contains(got[77], "не определён: ") {
		t.Errorf("pid 77 = %q, want «не определён:»", got[77])
	}
}

func TestPackagesFromMap(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"User 0: pid9\ncom.a.b,\n\n", []string{"com.a.b"}},
		{"User 0: pid9\ncom.a.b,com.c.d", []string{"com.a.b", "com.c.d"}},
		{"", nil},
	}
	for i, c := range tests {
		got := packagesFromMap(c.in)
		if len(got) != len(c.want) {
			t.Errorf("case %d: got %v, want %v", i, got, c.want)
			continue
		}
		for j := range got {
			if got[j] != c.want[j] {
				t.Errorf("case %d idx %d: got %q want %q", i, j, got[j], c.want[j])
			}
		}
	}
}

func TestIsPackage(t *testing.T) {
	if !IsPackage("com.wt.airconditioner") {
		t.Error("com.wt.airconditioner is package")
	}
	if IsPackage("system_server") {
		t.Error("system_server is not android package")
	}
	if IsPackage("com") {
		t.Error("too short")
	}
}

func TestFormatContainsAll(t *testing.T) {
	lines := []string{l0, l1}
	rep := Search(lines, []string{"请检查四周情况"})
	rep.AttrPids(map[int]string{1234: "com.wt.camera"})
	out := rep.Format("test")
	for _, want := range []string{"请检查四周情况", "1234", "com.wt.camera", "совпадений: 1", "PID-карта"} {
		if !strings.Contains(out, want) {
			t.Errorf("Format() lacks %q\n--\n%s", want, out)
		}
	}
}
