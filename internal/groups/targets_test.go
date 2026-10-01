package groups

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// parseTarget — helper: парсинг литерала со стандартной секционной структурой.
func parseTarget(t *testing.T, data string) *Targets {
	t.Helper()
	d := []byte(data)
	tt, err := Parse(d)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return tt
}

// TestParse — парсер: комментарии, пустые, CRLF, имена через пробел и по
// одному, заголовки регистронезависимые, имена до первой секции игнор.
func TestParse(t *testing.T) {
	data := strings.Join([]string{
		"# comment line",
		"",
		"pre-section-name", // до секции -> игнор
		"GROUP1",
		"A B", // несколько имён в одной строке
		"C",   // по одному
		"",
		"group2", // регистр заголовка не важен
		"D ",     // пробел на хвосте
		"# again",
		"TOP8",
		"E",
		"EXCLUDE9",
		"F",
	}, "\r\n")
	tt := parseTarget(t, data)
	want := &Targets{
		Groups:  [4][]string{{"A", "B", "C"}, {"D"}, nil, nil},
		Top8:    []string{"E"},
		Exclude: []string{"F"},
	}
	if !reflect.DeepEqual(tt.Groups, want.Groups) {
		t.Errorf("Groups = %v, want %v", tt.Groups, want.Groups)
	}
	if !reflect.DeepEqual(tt.Top8, want.Top8) {
		t.Errorf("Top8 = %v, want %v", tt.Top8, want.Top8)
	}
	if !reflect.DeepEqual(tt.Exclude, want.Exclude) {
		t.Errorf("Exclude = %v, want %v", tt.Exclude, want.Exclude)
	}
}

// TestParseEmpty — одна секций нет -> ошибка (батник: LOAD_OK=0).
func TestParseEmpty(t *testing.T) {
	for _, in := range []string{"", "# only comment\n\n", "GROUP1\n# но ни одного имени\n"} {
		if _, err := Parse([]byte(in)); err == nil {
			t.Errorf("Parse(%q) = nil error, want err", in)
		}
	}
}

// TestEmbeddedMatchesRoot — встроенная копия (go:embed) не расходитсся с
// корневым targets.txt (build.sh синхронизирует; тест ловит ручную правку
// без пересборки/пересинка).
func TestEmbeddedMatchesRoot(t *testing.T) {
	root, err := os.ReadFile(filepath.Join("..", "..", "targets.txt"))
	if err != nil {
		t.Skipf("нестандартный CWD: %v", err)
	}
	if string(root) != embeddedTargets {
		t.Fatalf("internal/groups/targets.txt отличается от корневого targets.txt — сделайте 'cp targets.txt internal/groups/targets.txt' (или ./build.sh)")
	}
}

// TestEmbeddedData — жёсткая сверка: данные 3.1.2 из targets.txt.
func TestEmbeddedData(t *testing.T) {
	tt := defaultTargets
	n := func(s []string) int { return len(s) }
	if got := n(tt.Groups[0]) + n(tt.Groups[1]) + n(tt.Groups[2]) + n(tt.Groups[3]); got != 91 {
		t.Errorf("динамических целей = %d, want 91", got)
	}
	if got := n(tt.Top8); got != 8 {
		t.Errorf("TOP8 = %d, want 8", got)
	}
	if got := n(tt.Exclude); got != 9 {
		t.Errorf("EXCLUDE9 = %d, want 9", got)
	}
	// дублей внутри данных нет
	seen := map[string]bool{}
	for _, g := range tt.Groups {
		for _, v := range g {
			if seen[v] {
				t.Errorf("дубль цели %q внутри групп", v)
			}
			seen[v] = true
		}
	}
	for _, v := range tt.Top8 {
		if !seen[v] {
			t.Errorf("TOP8 %q не входит в группы 1-4", v)
		}
	}
}

// TestLoadExternal — Load: внешний файл (env-путь явным аргументом),
// fallback при отсутствии, ошибка на битом файле.
func TestLoadExternal(t *testing.T) {
	dir := t.TempDir()
	ext := filepath.Join(dir, "targets.txt")
	content := "GROUP1\nFoo Bar\nGROUP2\nBaz\nTOP8\nFoo\nEXCLUDE9\nQux\n"
	if err := os.WriteFile(ext, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	c, src, err := Load(SchemeDynamic, ext)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if src != ext {
		t.Errorf("src = %q, want %q", src, ext)
	}
	if !reflect.DeepEqual(c.Groups[0], []string{"Foo", "Bar"}) || !reflect.DeepEqual(c.Groups[1], []string{"Baz"}) {
		t.Errorf("группы из внешнего файла: %+v", c.Groups)
	}
	if !reflect.DeepEqual(c.Exclude, []string{"Qux"}) {
		t.Errorf("Exclude = %v, want [Qux]", c.Exclude)
	}

	// нет файла -> fallback на встроенный (не ошибка)
	c2, src2, err := Load(SchemeDynamic, filepath.Join(dir, "nope.txt"))
	if err != nil {
		t.Fatalf("Load(нет файла): %v", err)
	}
	if src2 != "(встроенный targets.txt)" {
		t.Errorf("src = %q, want встроенный", src2)
	}
	if n2 := len(c2.Groups[0]) + len(c2.Groups[1]) + len(c2.Groups[2]) + len(c2.Groups[3]); n2 != 91 {
		t.Errorf("fallback: целей %d, want 91", n2)
	}

	// файл есть, но пуст -> ошибка
	empty := filepath.Join(dir, "empty.txt")
	os.WriteFile(empty, []byte("# пусто\n"), 0o644)
	if _, _, err := Load(SchemeDynamic, empty); err == nil {
		t.Error("Load(пустой файл) = nil, want err")
	}
}
