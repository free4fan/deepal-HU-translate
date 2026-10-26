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
