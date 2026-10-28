package app

import (
	"strings"
	"testing"
)

// originalLocales — «оригинальная» (OEM/из-коробки) локаль ГУ, на которую
// восстанавливает пункт меню [9] / CLI locale-restore. Жёсткая сверка:
// bat-ники (:RestoreOrigLocale) и deepl должны ставить ОДИН И ТОТ ЖЕ список.
// Решение 27.10 (live): из коробки ГУ сидит в zh-CN.
func TestOriginalLocalesConst(t *testing.T) {
	if originalLocales != "zh-CN" {
		t.Errorf("originalLocales = %q, want zh-CN", originalLocales)
	}
}

// Run(ModeLocaleRestore) из RU-состояния: settings put zh-CN + контрольное
// get; вывод с [LOCALE] и итоговой zh-CN.
func TestRestoreOriginalLocaleFromRu(t *testing.T) {
	m := &mockClient{uid: 0, deviceOK: true, locale: wantLocales}
	w := newAppBuilder(m)
	_, err := w.Run(ModeLocaleRestore, "")
	if err != nil {
		t.Fatalf("Run(ModeLocaleRestore): %v", err)
	}
	if !containsCall(m.calls, "shell settings get system system_locales") {
		t.Errorf("локаль не прочитана (settings get) в: %v", m.calls)
	}
	if !containsCall(m.calls, "shell settings put system system_locales "+originalLocales) {
		t.Errorf("нет put оригинальной локали: %v", m.calls)
	}
	out := w.buf.String()
	if !strings.Contains(out, "[LOCALE]") || !strings.Contains(out, originalLocales) {
		t.Errorf("вывод без [LOCALE]/zh-CN:\n%s", out)
	}
}

// Уже в оригинальной локали — put НЕ вызывается (no-op, без лишних команд).
func TestRestoreOriginalLocaleNoopWhenCurrent(t *testing.T) {
	m := &mockClient{uid: 0, deviceOK: true, locale: originalLocales}
	w := newAppBuilder(m)
	_, err := w.Run(ModeLocaleRestore, "")
	if err != nil {
		t.Fatalf("Run(ModeLocaleRestore): %v", err)
	}
	if len(m.locales) != 0 {
		t.Errorf("settings put вызван, хотя locale уже %s: %v", originalLocales, m.locales)
	}
	if !strings.Contains(w.buf.String(), originalLocales) {
		t.Errorf("вывод без текущей локали:\n%s", w.buf.String())
	}
}
