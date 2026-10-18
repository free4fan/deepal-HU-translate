package hunt

import (
	"strings"
	"testing"
)

func TestCollectCJKBasicTokens(t *testing.T) {
	lines := []string{
		"10-02 10:00:00.000  100  100 I T: ok",
		"10-02 10:00:01.000  200  200 D FDBus: 请检查四周情况,注意安全！",
		"10-02 10:00:02.000  200  200 D FDBus: 请检查四周情况 again",
		"10-02 10:00:03.000  300  300 I Car: 即将执行",
	}
	rep := CollectCJK(lines)
	if rep.TotalLines != 4 {
		t.Fatalf("TotalLines=%d", rep.TotalLines)
	}
	if rep.CJKLines != 3 {
		t.Fatalf("CJKLines=%d, want 3", rep.CJKLines)
	}
	// «请检查四周情况» и «注意安全» — разные токены (ла
	byText := map[string]CJKToken{}
	for _, tk := range rep.Tokens {
		byText[tk.Text] = tk
	}
	if c := byText["请检查四周情况"].Count; c != 2 {
		t.Errorf("请检查四周情况 count=%d, want 2", c)
	}
	if c := byText["即将执行"].Count; c != 1 {
		t.Errorf("即将执行 count=%d, want 1", c)
	}
	if pid := byText["请检查四周情况"].Pid; pid != 200 {
		t.Errorf("pid=%d, want 200", pid)
	}
}

func TestCollectCJKDominatedPid(t *testing.T) {
	lines := []string{
		"10-02 10:00:00.000  111  111 I A: 测试",
		"10-02 10:00:01.000  222  222 I B: 测试",
		"10-02 10:00:02.000  222  222 I B: 测试",
	}
	rep := CollectCJK(lines)
	if len(rep.Tokens) != 1 {
		t.Fatalf("tokens=%d", len(rep.Tokens))
	}
	tok := rep.Tokens[0]
	if tok.Pid != 222 {
		t.Errorf("dominant pid=%d, want 222 (2 hits)", tok.Pid)
	}
}

func TestCollectCJKSortByCount(t *testing.T) {
	lines := []string{
		"10-02 10:00:00.000  1 1 I A: 甲",
		"10-02 10:00:01.000  1 1 I A: 甲",
		"10-02 10:00:02.000  1 1 I A: 甲",
		"10-02 10:00:03.000  1 1 I A: 乙",
	}
	rep := CollectCJK(lines)
	if len(rep.Tokens) != 2 {
		t.Fatalf("tokens=%d", len(rep.Tokens))
	}
	if rep.Tokens[0].Text != "甲" || rep.Tokens[0].Count != 3 {
		t.Errorf("first=%+v", rep.Tokens[0])
	}
	if rep.Tokens[1].Text != "乙" {
		t.Errorf("second=%+v", rep.Tokens[1])
	}
}

func TestCollectCJKNoCJK(t *testing.T) {
	rep := CollectCJK([]string{"10-02 10:00:00.000  1 1 I A: plain", "junk"})
	if rep.CJKLines != 0 || len(rep.Tokens) != 0 || len(rep.EmitPids) != 0 {
		t.Fatalf("want empty, got %+v", rep)
	}
}

func TestCollectCJKUnicodeAstral(t *testing.T) {
	// идеограмма Ext B (U+20000+, astral) тоже ловится
	lines := []string{"10-02 10:00:00.000  1 1 I A: 𠀀 end"}
	rep := CollectCJK(lines)
	if len(rep.Tokens) != 1 || rep.Tokens[0].Text != "𠀀" {
		t.Fatalf("astral not captured: %+v", rep.Tokens)
	}
}

func TestReCJKOnly(t *testing.T) {
	if reCJK.MatchString("") {
		t.Error("empty")
	}
	if !reCJK.MatchString("请检查") {
		t.Error("cjk")
	}
	if reCJK.MatchString(",。！") { // пунктуация — нет
		t.Error("punct should not match")
	}
	if reCJK.MatchString("abc123") {
		t.Error("latin")
	}
}

func TestCJKFormat(t *testing.T) {
	rep := CollectCJK([]string{
		"10-02 10:00:00.000  222  222 I FDBus: 请检查四周情况",
	})
	rep.AttrPids(map[int]string{222: "com.wt.camera"})
	out := rep.Format("cjk-test")
	for _, want := range []string{
		"unique tokens: 1",
		"请检查四周情况",
		"com.wt.camera",
		"pid=222",
		"## pids emitting CJK",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Format lacks %q:\n%s", want, out)
		}
	}
}
