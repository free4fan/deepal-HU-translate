# TODO.md — задачи по проекту Deepal HU Translate

## Текущий статус (снимок 28.10)

- Перевод: **100/100 приложений**, ~17 840 строк (17 827 базовых + 13 styled),
  CJK-in-ru: 0. Актуальное — `python3 generate_summary.py`.
- RRO APK: **100 dynamic + 100 static** (`apks_rro_{min,static}/`), 0 FAIL,
  0 «STRING_TOO_LARGE».
- `validate.py` (с aapt2 dump по 200 APK): 0 ошибок; self-test — 90/90.
- Установка: **91 из 100 динамически** (9 AOSP-target — EXCLUDE9, статикой).
- Релиз: **v3.1.2-15** (deepl/deepl.exe + manage.bat + collect_report.bat +
  targets.txt + оба архива APK; README.md в шапке). Локаль: `manage.bat
  :EnsureRuLocale` + аналог в `deepl` (ru-RU в начале `system_locales` перед
  install/enable); **`locale-restore`** ([9]/CLI) — снять перевод и вернуть
  OEM-локаль zh-CN (обратная операция).
- История изменений — `CHANGELOG.md`; рабочий контекст и якоря — `MEMORY.md`.

## Открытые задачи

- **6 строк не влезают в лимит aapt2 (32700 UTF-8-байт) — намеренно вне
  оверлеев** (на ГУ — исходный текст): 4 больших юридических документа
  WT_FusionNavigation (`clause_content`, `clause_content1`, `policy_content`,
  `policy_content1`) + `road_book_my_agreement_two_content` +
  WT_GameCenter/`str_procotol`. RU-перевод сохранён в `translations/*.json`;
  для включения — split на несколько ресурсов (правки приложения) или
  укорочение перевода.
- **Данные assets/raw — RRO не трогает** (путь reпака отклонён 25.09,
  отложено): `WT_TSpeech/assets/*.json+cfg` (голосовые хотворды),
  `WT_MLWecarControl/res/raw/carconfig_*.json`,
  `WT_TinnoveCoreService/res/raw/virtual_data.xml`,
  `WT_AISpace/assets/speech_skill*.yaml`.
- **vendor-краш** `com.tinnove.wecarspeech` (NumberFormatException, не наш) —
  отследить повторения в отчётах; на нас не влияет.
- По желанию: `apply_db_translations.py --all` **без** `--dry-run`
  (заполнить оставшиеся пустые `ru` из SQLite-словаря; LLM-прогон завершён,
  теперь безопасно) — и только потом без dry-run.
- По желанию: дедупликация `create_rro_min.py`/`create_rro_static.py` и
  централизация `targetPackage` (общий модуль сборки + `target_packages.json`).

## Закрыто (свежее — однострочно)

- **(27.10) `deepl`-триада на чужой машине**: adb без platform-tools
  (env `DEEPL_ADB` → PATH → рядом с бинарником), [R] в меню теперь печатает
  результат и держит паузу, unauthorized — своё сообщение (ключ adb ПК через
  adbauth). CHANGELOG [2026-10-27..d].
- **(27.10) `:EnsureRuLocale` в `manage.bat`** (live-фикс WT_VehicleCenter):
  ru в начале `system_locales` перед install/enable. CHANGELOG [2026-10-27f].
- **(27.10) Rелизы v3.1.2-11/v3.1.2-12**: batы динамических (manage.bat +
  collect_report.bat) в архиве релиза; доки о составе.
- **(26.10) «将执行+название сцены» в Camera** — рантайм-конкатенация
  код+данные (SQLite-БД ГУ), **вне зоны RRO, не переводить**. Якорь в
  MEMORY.md, CHANGELOG [2026-10-02].
- **(30.09) WT_AirConditioner «перевод слетал на китайский»** — решено
  mirror-копией `values-zh-rCN/` (значения идентичны RU). MEMORY.
- **(25.09) «Умное обслуживание» `- - км`** — 8 строк-ключей
  WT_AutoMaintenance + 2 ключа WT_HDCloudCamera в `exclude.json`; LIVE-
  подтверждено. Имена разделов (данные сервера) — осознанно остаются
  китайскими. CHANGELOG [2026-09-25].
- **(25.09) RU при локали ≠ ru** — дубль RU в `res/values/` (default-конфиг);
  rebuild 100+100. CHANGELOG [2026-09-24d].
- **(25.09) reпак зашитых layout-надписей — ВАРИАНТ ОТКЛОНЁН**; инструменты
  в `/home/user/projects/deepal-HU-translate-bak-repack/`. CHANGELOG
  [2026-09-22]/[2026-09-25].
- **(22.09) Live-проверка bat-менеджеров** — форматы pm list/overlay list
  сверены; EXCLUDE9 вынесен из динамической установки (91/100); 91/9 в
  сводках; WT_WtSystemUI → root+enable; PhoneLink rename. CHANGELOG
  [2026-09-22b..25].
- **(21.09) Пайплайн с нуля** — 5 дыр закрыто (стрки с разметкой styled,
  лимит aapt2, zero-drift extract); 100+100 APK, 0 FAIL. CHANGELOG
  [2026-09-21..21b].
