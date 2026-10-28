package app

import (
	"strings"

	"deepal-hu-translate/internal/opslog"
)

// wantLocales — целевой список system_locales: ru в начале (см. manage.bat
// :EnsureRuLocale; live-фикс 2026-10-04 по WT_VehicleCenter). Точное совпадение
// (values-en / values-zh-rCN таргета с кит. значениями) в разрешении
// конфигураций ВЫШЕ default values/ оверлея; values-ru/ участвует только при
// `ru` в preferred. Батник и deepl используют ОДНОИ ТОО ЖЕ значение.
const wantLocales = "ru-RU,en-US,zh-CN"

// originalLocales — «оригинальная» (OEM, из коробки) локаль ГУ, на которую
// восстанавливает пункт меню [9] / CLI locale-restore (обратная операция
// ensureRuLocale). Решение 27.10 (live): из коробки ГУ сидит в zh-CN.
// Bat-ники (:RestoreOrigLocale) и deepl используют ОДИН И ТОТ ЖЕ список.
const originalLocales = "zh-CN"

// ensureRuLocale — check+add: `settings get system system_locales`; если
// значение != wantLocales — `settings put system system_locales wantLocales`.
// Вызывать ТОЛЬКО от root (обёртки runDynamic/runStatic делают это после
// ensureRoot/rootRemount). Возвращает итоговое значение для строки [LOCALE].
func (a *App) ensureRuLocale(ol *opslog.OpsLog) (cur string) {
	res := a.Adb.Shell(a.Ctx, "settings", "get", "system", "system_locales")
	cur = strings.TrimSpace(res.Out)
	if wantLocales == cur {
		a.print("[LOCALE] system_locales уже: %s\n", wantLocales)
		if ol != nil {
			ol.Db("[LOCALE] уже: " + wantLocales)
		}
		return cur
	}
	a.print("[LOCALE] system_locales сейчас: %q - выставляю %q\n", cur, wantLocales)
	if ol != nil {
		ol.Db("[LOCALE] было: " + cur)
	}
	a.Adb.Shell(a.Ctx, "settings", "put", "system", "system_locales", wantLocales)
	res = a.Adb.Shell(a.Ctx, "settings", "get", "system", "system_locales")
	cur = strings.TrimSpace(res.Out)
	if cur == "null" {
		cur = ""
	}
	a.print("  [LOCALE] теперь: %s (уже запущенные процессы увидят RU после своего перезапуска или reboot)\n", cur)
	if ol != nil {
		ol.Db("[LOCALE] стало: " + cur)
	}
	return cur
}

// RestoreOriginalLocale — вернуть систему в исходную (до перевода) локаль:
// `settings put system system_locales zh-CN`. Обратная операция
// ensureRuLocale (install/enable). Не нужен root (settings shell работает),
// поэтому в Run() идёт в ветке status/diag/reboot — без цели/root/ops-лога.
// Уже запущенным процессам изменение видно после их перезапуска или reboot.
func (a *App) RestoreOriginalLocale() {
	res := a.Adb.Shell(a.Ctx, "settings", "get", "system", "system_locales")
	cur := strings.TrimSpace(res.Out)
	if cur == "null" {
		cur = ""
	}
	if cur == originalLocales {
		a.print("[LOCALE] system_locales уже оригинальная: %s - ничего не меняю\n", originalLocales)
		return
	}
	a.print("[LOCALE] system_locales сейчас: %q - возвращаю оригинальную %q (до перевода)\n", cur, originalLocales)
	a.Adb.Shell(a.Ctx, "settings", "put", "system", "system_locales", originalLocales)
	res = a.Adb.Shell(a.Ctx, "settings", "get", "system", "system_locales")
	cur = strings.TrimSpace(res.Out)
	if cur == "null" {
		cur = ""
	}
	a.print("  [LOCALE] теперь: %s (уже запущенным процессам видно после их перезапуска или reboot)\n", cur)
}
