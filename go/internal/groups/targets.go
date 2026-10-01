package groups

import (
	"errors"
	"os"
	"strings"
)

// targets.txt — Единственный источник списков целей (группы 1-4, TOP-8,
// EXCLUDE9) для ОБЕИХ схем: manage.bat / manage_static.bat читают его
// напрямую, Go-менеджер — через Load() из внешнего файла (env
// DEEPL_TARGETS / ./targets.txt в CWD).
//
// Бинарник НЕСЁТ списка APK: нет ни go:embed, ни встроенной копии —
// при отсутствии читаемого targets.txt Load() возвращает ошибку.
// Для другой прошивки достаточно заменить файл (батники подхватят сами;
// deepl: DEEPL_TARGETS или файл в CWD).

// Targets — разобранные секции targets.txt.
type Targets struct {
	Groups  [4][]string // группы 1..4 (индексы 0..3)
	Top8    []string
	Exclude []string
}

// Имена секций (как в targets.txt, сравнение без учёта регистра).
const (
	SecGroup1  = "GROUP1"
	SecGroup2  = "GROUP2"
	SecGroup3  = "GROUP3"
	SecGroup4  = "GROUP4"
	SecTop8    = "TOP8"
	SecExclude = "EXCLUDE9"
)

// Parse — парсит содержимое targets.txt. Формат: строки «#...» и пустые —
// комментарии; строка, совпадающая с именем секции (с учётом пробелов на
// краях), — переключатель; остальные строки — имена (несколько через
// пробел допустимо). Имена до первой секции игнорируются.
func Parse(data []byte) (*Targets, error) {
	var t Targets
	sec := -1
	sections := map[string]int{ // -1 = отвалить, >=0 индекс
		SecGroup1: 0, SecGroup2: 1, SecGroup3: 2, SecGroup4: 3,
		SecTop8: 10, SecExclude: 11,
	}
	add := func(sec int, name string) {
		switch sec {
		case 0:
			t.Groups[0] = append(t.Groups[0], name)
		case 1:
			t.Groups[1] = append(t.Groups[1], name)
		case 2:
			t.Groups[2] = append(t.Groups[2], name)
		case 3:
			t.Groups[3] = append(t.Groups[3], name)
		case 10:
			t.Top8 = append(t.Top8, name)
		case 11:
			t.Exclude = append(t.Exclude, name)
		}
	}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var matched string
		for name := range sections {
			if strings.EqualFold(line, name) {
				matched = name
				break
			}
		}
		if matched != "" {
			sec = sections[matched]
			continue
		}
		if sec < 0 {
			continue
		}
		for _, name := range strings.Fields(line) {
			add(sec, name)
		}
	}
	if len(t.Groups[0])+len(t.Groups[1])+len(t.Groups[2])+len(t.Groups[3])+len(t.Top8) == 0 {
		return nil, errors.New("пустой список целей: не найдена ни одна секция GROUP1..GROUP4/TOP8")
	}
	return &t, nil
}

// ParseFile — Parse из файла.
func ParseFile(path string) (*Targets, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// Build — Config схемы из Targets (глубокая копия слайсов).
// Статическая схема: GROUP1 = GROUP1 + EXCLUDE9 (как в manage_static.bat),
// EXCLUDE9 отдельно не оставляется.
func Build(scheme Scheme, t *Targets) *Config {
	c := &Config{
		Scheme:  scheme,
		LogsDir: "logs",
		Groups: [4][]string{
			append([]string(nil), t.Groups[0]...),
			append([]string(nil), t.Groups[1]...),
			append([]string(nil), t.Groups[2]...),
			append([]string(nil), t.Groups[3]...),
		},
		Top8:    append([]string(nil), t.Top8...),
		Exclude: append([]string(nil), t.Exclude...),
	}
	if scheme == SchemeStatic {
		c.Prefix = "com.deepal.translate.rro."
		c.ApkDir = "apks_rro_static"
		c.Filter = "com.deepal.translate"
		c.OverlayBase = "/vendor/overlay"
		g1 := make([]string, 0, len(c.Groups[0])+len(c.Exclude))
		g1 = append(g1, c.Groups[0]...)
		g1 = append(g1, c.Exclude...)
		c.Groups[0] = g1
		c.Exclude = nil
	} else {
		c.Prefix = "com.android.vendor.translate.rro."
		c.ApkDir = "apks_rro_min"
		c.Filter = "com.android.vendor"
	}
	return c
}
