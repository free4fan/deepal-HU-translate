# TODO.md — Задачи по проекту Deepal HU Translate

## Закрыто (25.09) — reпак зашитых в layout надписей: ВАРИАНТ ОТКЛОНЁН, инструменты вынесены в бэкап

Решение пользователя (25.09): проект переводит только через RRO; путь
«бинарный патч AXML внутри исходных APK» свёрнут. Всё вынесено в
`/home/user/projects/deepal-HU-translate-bak-repack/` (см. шаг 12
WORKFLOW.md): axml.py, scan/translate/patch/verify_*.py, install_patched.bat,
revert_patched.bat, layout_literals.json + логи.
(`original_apks/` 5.0GB в тот же день возвращён в проект — нужен
RRO-конвейеру для передекомпиляции, `scripts/decompile_apks.py`.)
История собранного пути (56 APK / 1487 литералов / cjk_left=0, live-проверка
на ГУ не проводилась, на машине patched-APK не стояли) — в CHANGELOG
[2026-09-22] и якорях MEMORY.md.

### Открытые задачи (22.09)
- [x] **(25.09, закрыто) «Умное обслуживание» `- - км`**: в `exclude.json`
      8 строк-ключей WT_AutoMaintenance + 2 ключа WT_HDCloudCamera (см. класс
      бага `getString`-как-значение); APK 261/495 res пересобраны, LIVE
      подтверждено — цифры появились. Имена разделов (空调滤芯/制动液/
      减速器油) = данные с сервера, `setText(program)` — по решению
      ОСТАНИЛИСЬ китайскими, репак не делать.
      См. CHANGELOG [2026-09-25], якорь в MEMORY.md.
- [x] **(24.09d, закрыто) RU при локали ≠ ru**: генераторы пишут дубль RU в
      `res/values/` (default-конфиг) помимо `res/values-ru/`; rebuild 100+100,
      validate full 0 ошибок. См. CHANGELOG [2026-09-24d], якорь в MEMORY.md.
- [ ] **Данные assets/raw (RRO не трогает):**
  - `WT_TSpeech/assets/{rule-config.json,command-config*.json,public/cfg/*.cfg}`
    — голосовые команды-хотворды (换挡音/车外音/空调控制/座椅按摩 «открыть/выключить»);
  - `WT_MLWecarControl/res/raw/carconfig_*.json` (SeatComfortOutInSupportDoc и т.п.);
  - `WT_TinnoveCoreService/res/raw/virtual_data.xml`; `WT_AISpace/assets/speech_skill*.yaml`.
  Решение (25.09): reпак-путь отклонён (см. закрытый блок выше) → задача
  ОТЛОЖЕНА; список фраз — `rg` по assets (собирали 22.09, отчёт «не покрыто»).
- [ ] **git init** (проект не под git — *.py, bat'ы, overlays, apks_rro_* —
  защитить бэкапом; translations/*.json — главный актив).

## Текущий статус (21.09.2026) — ЗАВЕРШЕНО, 100%

- ✅ Перевод: **17 840 строк**, 100/100 приложений, CJK-in-ru: 0
  (17 827 базовых + 13 строк с размéткой, найденных/переведённых 20.09 —
  см. CHANGELOG [2026-09-21]).
- ✅ RRO APK: **100 dynamic + 100 static**, 0 FAIL, **0 «STRING_TOO_LARGE»**
  (aapt2 dump по всем 200).
- ✅ `validate.py` (с aapt2 dump): **0 ошибок**; self-test improve **59/59**.
- ✅ Все прогоны завершены: `--all` + `--long` (вкл. CJK-ремонт последней
  длинной строки). Прогресс — `translations/improve_progress.json`
  (**17 747 done** = все).
- ⚠️ **6 строк не влезает в oверлей** (лимит aapt2 32 767 UTF-8-байт) → на ГУ
  остаётся исходный текст (а не «STRING_TOO_LARGE»). Перевод сохранён в
  `translations/*.json`; см. «Открытые задачи».

### Сделано 17.09 (см. CHANGELOG [2026-09-17b..d])
- [x] Починены 5 багов данных (values-ru, массивы 0..N, plurals few/many, 12 строк с %s/CJK)
- [x] `validate.py` (семантика: форматтеры по порядку, CJK, массивы, APK, дубли name)
- [x] `improve_translations.py` + `promt.md` + self-test + hint-retry
- [x] Починка `specs()` (старый regex не видел `%s`/`%%`)
- [x] Префикс `com.android.vendor.*` у динамических (скрытие из лаунчера)
- [x] `create_db.py`/`apply_db_translations.py` аудит+пофикс (песочница, не на данных)

### Сделано 18.09 (см. CHANGELOG [2026-09-18])
- [x] Удалён исторический путь translate: `translate_one.py`, `translate_batch.py`,
      `api_translate_ambiguity.py`, `scan_ambiguities.py`, `apply_db_new_ru.py`,
      `revert_cjk_translations.py`, `run_pipeline.sh` (единственный путь переводов —
      `improve_translations.py`)
- [x] Добавлены прогонщики `create_app_pipeline.sh`, `export-to-db.sh`
- [x] Синхронизация документации (README/MEMORY/TODO/WORKFLOW/CHANGELOG) с фактическим составом скриптов

### Сделано 18.09 (см. CHANGELOG [2026-09-18b])
- [x] `improve_translations --all` доведён до конца
- [x] Длинные строки (`len(zh)>=200`) — по одной: `API_LONG_THRESHOLD`,
      `_make_chunks`, новый режим `--long` (обходит progress-фильтр)
- [x] Починка корня `"None"` в данных: `str(None)` от JSON `null` → `res.get("ru") or ""`;
      guard: `DEGENERATE_ANSWERS` + `LONG_RATIO_MIN`; hint-retry на CJK-остатки
- [x] Починка обнуления progress (не-`--resume` сбрасывал `done=set()`): merge через
      `load_progress()`
- [x] Self-test 33→**37**; пересборка обоих наборов APK (в т.ч. с гигантскими `<string>`)
      — 0 FAIL; `validate.py` 0 ошибок

### Сделано 18.09 (см. CHANGELOG [2026-09-18c])
- [x] Точечный CJK-ремонт (`_repair_cjk`): окно ±50 симв. вокруг каждого
      CJK-фрагмента → модель возвращает окно исправленным → подстановка
      (без регенерации всего 30к+ перевода); lenient-парсер на кривой JSON
      модели; 3 попытки; полный повтор — только фолбэк
- [x] Добита последняя строка: `WT_FusionNavigation/road_book_my_agreement_two_content`
      (9 273 zh → 33 152 ru, 6 фрагментов): **17 827/17 827 (100.0%)**, 100/100 apps
- [x] Self-test 37→**42/42** (+ блок `parse_repair`); финальная пересборка,
      `validate.py` 0 ошибок

### Сделано 19–20.09 (см. CHANGELOG [2026-09-19..19d, 2026-09-20])
- [x] `collect_report.bat` + режим `report` в обоих менеджерах (сбор zip для анализа)
- [x] Меню bat появляется ВСЕГДА даже без adb (баннер статуса + пункт [R]);
      убрана ADB-блокировка до меню
- [x] Логи установки `logs\ops_<mode>_<TS>.log` (оба менеджера) + попадание в report-zip
- [x] Починка парсера отчёта: `pm list` (граница `=`), строки `overlay list`
      (двойной поиск), ANR с auto `adb root` + trace; новые raw 43/44/45
- [x] Wine-проверка (headless 9.0): пойманы и починены 13 live-багов (скобки в
      `if (...)`-блоках, «ECHO is OFF»); bat синтаксически проверены end-to-end

### Сделано 21.09 (см. CHANGELOG [2026-09-21])
- [x] Найдены и переведены **13 непереведённых строк** с вложенной разметкой
  (`<b>/<a>/<annotation>/<Data>…</Data>`), которые `extract_cjk.py` regex
  `([^<]*)` никогда не извлекал: 9 у WT_FusionNavigation (Amap-уступки/политика),
  Fota/upgrade_service_agreement_content, CarService/imsi_protection_warning,
  PackageInstaller/uninstall_application_text_all_users,
  ManagedProvisioning/read_more_delete_profile. Перевод по фрагментам
  (разметка побайтово 1:1, CJK-in-ru=0), записи помечены `"styled": true`.
- [x] Найден и учтён **лимит aapt2 32 767 UTF-8-байт на строку** (аapt2 молча
  режет в «STRING_TOO_LARGE»). До этого в готовых APK уже были 2 такие строки
  (WT_FusionNavigation/road_book_my_agreement_two_content,
  WT_GameCenter/str_procotol) — на ГУ читалось «STRING_TOO_LARGE». Теперь
  генераторы (`generate_overlays*.py`, `AAPT2_MAX_BYTES=32700`) НЕ пишут такие
  строки в оверлей (на ГУ остаётся исходник), печатают `[WARN]`.
- [x] Генераторы: ветка `styled` (русская строка с разметкой — verbatim без
  `_esc()`), `improve_translations.is_target()` не прогоняет `styled`.
- [x] Пересборка: 100+100 APK, 0 FAIL, 0 «STRING_TOO_LARGE» (aapt2 dump по
  всем 200). validate.py: 0 ошибок; self-test 59/59.
 - [x] **V. ЗАВЕРШЕНО (21.09) — «пайплайн с нуля»: закрыты 5 дыр**
  (TODO 21.09 12:28 → исполнение 21.09 ~13:30, сессия 2). Повторный прогон
  `extract_cjk → improve → validate → generate → build` из чистого состояния
  не воспроизводит баги. Контекст: (а) `extract_cjk.py:38` regex `([^<]*)` рвал
  строки с вложенной разметкой (потеря 13); (б) aapt2 при значении >32767
  UTF-8-байт ТИХО писал «STRING_TOO_LARGE» (warning, не error; 2 строки уже
  сидели в старых APK); (в) лимит одинаков plain/styled; (г) рус. перевод ×2.5
  → 6 строк не влезли. План (см. CHANGELOG [2026-09-21b]):
  1. [x] якорь продолжения в TODO/MEMORY;
  2. [x] бэкап 6 файлов (сост. ДО работ 21.09) → `/tmp/opencode/bak_pipeline21/`;
  3. [x] extract_cjk.py: ГИБРИД — plain прежним raw-regex (zero-drift к
     17 832 zh), styled через ET, plurals/arrays через ET/itertext,
     `"styled": true`; ParseError → regex. **Найден+починен баг первичной
     ET-версии** (дублирование tail + `&gt;`-дрейф у 9 WTN) → ручной
     сериализатор `_string_inner_xml` (0 потерь/дрейф);
  4. [x] ПЕСОЧНАЯ check (read-only): 0 потерь + ровно 13 новых styled +
     13/13 побайто с текущими JSON + self-close <br/>;
  5. [x] improve_translations.py `--styled` (механика fix_markup3, кэш
     improve_styled_cache.json); сеть НЕ гоняна (13 уже переведены);
  6. [x] validate.py: STRING_TOO_LARGE в APK = **ОШИБКА** (a+array+plurals);
     байт-аудит >32700 = WARN; CJK-ресурс decompiled ∉ JSON = WARN;
  7. [x] create_rro_{min,static}.py: «string too large…» в stdout (rc=0) →
     **FAIL приложения** (rmtree, None);
  8. [x] test_improve_selftest.py: +test_styled/+test_aapt2_limit → **90/90**;
  9. [x] верификация: rebuild 100+100 = 0 FAIL, **md5 не изменились**,
     validate полный (aapt2 dump 200) = 0 ошибок (7 WARN штатно);
  10. [x] DOCS: WORKFLOW 5.13 + «с нуля» + предупреждение extract_cjk ОБНУЛЯЕТ
      ru; MEMORY/CHANGELOG [2026-09-21b] обновлены.
  10. [ ] DOCS: WORKFLOW.md 5.13 + шпаргалка «с нуля» + предупреждение
      «extract_cjk.py ОБНУЛЯЕТ ru — не гонять после перевода»; MEMORY.md.

- [ ] **6 строк не влезают в лимит aapt2 и намеренно не в оверлеях** (на ГУ —
  текст источника): 4 больших документа WT_FusionNavigation (clause_content,
  clause_content1, policy_content, policy_content1 — русский перевод ~2x
  китайского по байтам) + WT_FusionNavigation/road_book_my_agreement_two_
  content + WT_GameCenter/str_procotol. Перевод сохраняется в
  `translations/*.json` (поле `ru`); для включения — нужен split на несколько
  ресурсов (правки приложения) либо укорочение перевода.
- [x] **Live-HU-проверка bat-менеджеров** (закрыто 22.09, см. CHANGELOG
      2026-09-22b): реальные форматы `pm list`/`cmd overlay list` сверены с
      живым репортом, нашлись и починены: false-«Ошибок: 100» (неэкранированные
      скобки в RESULT-строках ops-лога), ложный «Установлено: 0» (формат
      `package:PKG versionCode:N` без `=`), GROUP3 `WT_PhoneLink`→`PhoneLink`,
       ANR-dump без `/data/anr`-префикса. **Осталось решить (по логам):**
       ~~9 dynamic-оверлеев с AOSP-target (NetworkStack/Providers/…) не
       ставятся `adb install` → перевод в static или исключение из групп~~
       (закрыто 25.09: исключены из manage.bat — `EXCLUDE9`, установка
       идёт по 91 пакету; перевод их строк при необходимости — статика);
      ~~wt_wtsystemui STATE_DISABLED → enable~~ (закрыто 25.09: enable
      требует ROOT — target com.android.systemui объявляет `<overlayable>`
      → `manage.bat` теперь весь прогон делает от root;
      live 26.09: `OK (установлен и включён)`);
      ~~wt_link `STATE_NO_IDMAP`~~ (закрыто 27.09: после чистого
      uninstall+install от root — `[x]`; при повторе — `uninstall`+`install`
      пакета от root, диагностика в `raw/46_idmap.txt`);
      отследить vendor-краш com.tinnove.wecarspeech (NumberFormatException, не наш).
- [ ] По желанию: `apply_db_translations.py --all --dry-run` (заполнить из словаря;
      LLM-прогон завершён, теперь безопасно) и только потом без dry-run
- [ ] `git init` + `.gitignore` (decompiled/, original_apks/, __pycache__,
      logs/, overlay_build*/), commit
- [x] (18.09) Выесть мусор: `__pycache__/`, `scripts/__pycache__/`, `rro_builds/`,
      `scripts/logs/`; удалить `NEW_SESSION.md`/`PROJECT_DATA.md`/`REVIEW.md`/
      `TRANSLATE_TARGETS.md`; перенести `RRO-overlay-howto.md` в `docs/`
- [ ] Дедупликация `create_rro_min.py`/`create_rro_static.py` и централизация
      `targetPackage` (см. п.8 в уделённом 18.09 `REVIEW.md`; суть: общий модуль
      сборки + `target_packages.json`)

## История

### Статус (02.09.2026, 16:30 UTC — завершено)

- ✅ Перевод: 5621/5621 CJK (**100% — завершён**)
- ✅ RRO APK: 40 файлов, **790 KB** суммарно
- ✅ OLLAMA: все 2 процесса остановлены (перевод завершён)

### Выполнено (снимок 02.09)

- [x] Декомпилировано 63 APK
- [x] Извлечено CJK в 41 JSON файл
- [x] Переведено 2892 CJK (Google Translate gtx)
- [x] Переведено 2752 CJK через OLLAMA ornith:35b-f16 (в фоне)
- [x] Ручной перевод 12 CJK (дни недели, эмодзи, английский текст)
- [x] Ручной перевод ~17 CJK (WT_MultiMediaCenter, OLLAMA не справился — заменено)
- [x] 40 RRO APK собраны (~8 KB каждый) через aapt2 + apksigner
- [x] Все APK подписаны (platform.jks)
- [x] Оптимизация: 120 000x по объёму
- [x] Все старые APK удалены
- [x] Починен aapt2 bug с `&#39;` → исправлено на `&apos;`
- [x] Удалено 17 неиспользуемых скриптов, осталась только рабочая ветка

### Итоги оптимизации (снимок 02.09)

| Метрика | Было | Стало |
|---|---|---|
| APK размер | ~700 MB | ~8 KB |
| Общий объём | ~44 GB | 790 KB |
| Экономия | — | **~56 000x** |
| Скрипт сборки | apktool 3.0.2 (FileSPI error) | aapt2 + apksigner |

### Установка на ГУ Deepal S05 (исторический вывод, актуальное — README)

```bash
# Проверка подключения
adb devices

# Список всех RRO APK для установки
ls apks_rro_min/*.apk

# Установка одного APK
adb push apks_rro_min/<App>_RRO.apk /sdcard/
adb shell install -r /sdcard/<App>_RRO.apk

# Перезапуск приложения
adb shell am force-stop <package>
adb shell am start -n <package>/.MainActivity

# Проверить статус всех RRO
adb shell cmd overlay list --user 0

# Включить/отключить конкретный RRO
adb shell cmd overlay enable --user 0 com.android.vendor.translate.rro.<app_lowercase>
adb shell cmd overlay disable --user 0 com.android.vendor.translate.rro.<app_lowercase>
```

### Зависимости (исторический снимок; актуальные — README/MEMORY)

- apktool 3.0.2 (декомпиляция), aapt2 (сборка RRO), apksigner (подпись)
- Java 21+, Python 3.12+, platform.jks
- Ollama (10.0.0.128:11434): ornith:35b-bf16, qwen3.8:27b

### Структура проекта (исторический вывод; актуальная — README)

```
deepal-HU-translate/
├── decompiled/              ← 63 декомпилированных APK
├── translations/            ← 41 JSON-файл с переводами
├── apks_rro_min/            ← 40 подписанных RRO APK (~8 KB каждый)
├── generate_summary.py      ← генератор итоговой таблицы
├── create_rro_min.py        ← сборка RRO через aapt2 + apksigner
├── translate_ollama_ornith.py ← перевод через Ollama (в фоне)
├── install.bat              ← установка 8 APK (overlays.txt)
├── install_all.bat          ← установка 40 APK
├── disable_8.bat            ← деактивация 8 APK
├── disable_all.bat          ← деактивация 40 APK
├── uninstall_all.bat        ← удаление 40 APK
├── NEW_SESSION.md           ← документация для новой сессии
├── README.md                ← краткая справка
├── CHANGELOG.md             ← история изменений
└── TODO.md                  ← эта страница
```

### Следующие шаги (исторический снимок 02.09, опционально)

- [ ] Установить на ГУ Deepal S05 (начать с крупных приложений)
- [ ] Протестировать переводы в приложении
- [ ] Отключить неиспользуемые скрипты
