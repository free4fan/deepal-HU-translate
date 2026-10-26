package app

import "testing"

// wantLocales — фиксированное значение (ru в начале). Жёсткая сверка:
// manage.bat :EnsureRuLocale и deepl должны ставить ОДНОИ ТОИ ЖЕ список.
func TestWantLocalesRuFirst(t *testing.T) {
	if wantLocales == "" {
		t.Fatal("wantLocales пуст")
	}
	if wantLocales[:3] != "ru-" {
		t.Errorf("ru не в начале: %q", wantLocales)
	}
	if wantLocales != "ru-RU,en-US,zh-CN" {
		t.Errorf("expect ru-RU,en-US,zh-CN, got %q", wantLocales)
	}
}
