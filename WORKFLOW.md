# Deepal HU Translate — Пошаговое руководство

Полный workflow: декомпиляция APK → извлечение строк → перевод/улучшение через Ollama → **автопроверка (`validate.py`)** → генерация RRO-оверлеев → сборка APK → установка на ГУ.

Также поддерживается второй тип — **статические RRO APK**, размещаемые в `/vendor/overlay/` и активные сразу после загрузки.

**Быстрая шпаргалка (существующие переводы):**
```bash
python3 validate.py                                   # обязателен до/после сборки
python3 generate_overlays.py && python3 create_rro_min.py
python3 generate_overlays_static.py && python3 create_rro_static.py
python3 validate.py                                   # финал (включая APK)
```
или весь цикл сразу: `./create_app_pipeline.sh`
**Перевод/улучшение (LLM, запись в `ru`):** `improve_translations.py` (шаг 3, промпт — `promt.md`).

**Шпаргалка «пайплайн с нуля» (новые приложения, 21.09):**
```bash
python3 scripts/decompile_apks.py --app <App>         # 1. декомпиляция
python3 extract_cjk.py                                # 2. извлечение (ОБНУЛЯЕТ ru!
                                                      #    см. предупреждение в шаге 2)
python3 improve_translations.py --all                 # 3. перевод (LLM)
python3 improve_translations.py --styled              # 3b. styled-строки (по фрагментам)
python3 validate.py                                   # 4. автопроверка (обязателен)
python3 generate_overlays.py && python3 create_rro_min.py        # 5. мин
python3 generate_overlays_static.py && python3 create_rro_static.py  # 5b. статик
python3 validate.py                                   # 6. финал (aapt2 dump 200 APK)
python3 test_improve_selftest.py                      # 7. self-test (90/90)
```
Входы/выходы проверены: повторный прогон из чистого состояния не воспроизводит
потерю строк с разметкой (ET-путь извлечателя) и тихие «STRING_TOO_LARGE»
(create_rro_* теперь FAIL'ит приложение, аapt2 предупреждение — в STDOUT,
rc=0; validate.py делает их ошибкой).

---

## Структура проекта

```
deepal-HU-translate/
├── scripts/                              # Вспомогательные скрипты
│   ├── decompile_apks.py                 # Декомпиляция APK через apktool
│   └── quality_check.py                  # Аудит качества переводов
├── translations/<app>.json               # Переводы (zh + ru) для каждого приложения
├── decompiled/<app>/                     # Результат декомпиляции apktool
├── original_apks/<app>.apk              # Исходные APK (для decompile_apks.py)
│
├── overlays/<app>/                       # Динамические RRO — adb install
│   ├── AndroidManifest.xml               # package=com.android.vendor.translate.rro.<app>
│   ├── res/values-ru/strings.xml         # Переведённые ru-строки (ru-локаль)
│   └── res/values/strings.xml            # ДУБЛЬ ru (default-конфиг: RU и при
│                                         #   локали ≠ ru — fallback вместо zh/en)
│
├── overlays_static/<app>/                # Статические RRO — /vendor/overlay
│   ├── AndroidManifest.xml               # package=com.deepal.translate.rro.<app>, isStatic=true
│   ├── res/values-ru/strings.xml         # Переведённые ru-строки (ru-локаль)
│   └── res/values/strings.xml            # ДУБЛЬ ru (default-конфиг, как выше)
│
├── overlay_build/<app>/                  # Временная папка сборки (min)
├── overlay_build_static/<app>/           # Временная папка сборки (static)
├── apks_rro_min/<app>_RRO.apk            # Динамические RRO APK
├── apks_rro_static/<app>_RRO.apk         # Статические RRO APK
├── database/translations.sqlite3         # Словарь (create_db.py / apply_db_translations.py)
├── docs/                                 # Документация (APPS.md — список 100 приложений,
│                                         #   RRO-howto, LAUNCHER_APP_HIDDEN_DESIGN и др.)
├── keys/platform.jks                     # Подпись RRO-оверлеев
├── validate.py / improve_translations.py / promt.md
│   # автопроверка / LLM-улучшение в ru / системный промпт
├── create_app_pipeline.sh / export-to-db.sh
│   # прогонщики: полный цикл сборки / create_db+validate
├── manage.bat                     # менеджер ДИНАМИЧЕСКИХ RRO
│   # (install/enable/disable/uninstall/status/diag/report/reboot), CLI:
│   #   manage.bat <mode> <preset>
├── manage_static.bat              # менеджер СТАТИЧЕСКИХ RRO (root, /vendor/overlay)
│   # (install/uninstall/status/diag/report/reboot)
├── collect_report.bat             # вызывается report из обоих: сборка zip-лога
├── targets.txt                    # единый список целей (группы 1-4, TOP8, EXCLUDE9)
│   # читают bat-менеджеры и deepl (бинарники/скрипты списков не несут)
├── go/                            # Go-менеджер deepl (go.mod, cmd/, internal/)
│   #   build.sh - gofmt+vet+тесты+сборка deepl/deepl.exe в корень
│   #   CLI: deepl dyn|stat <mode> <preset>, deepl reboot, deepl report
│   #   adb: DEEPL_ADB -> PATH -> рядом с бинарником ([ADB] using: <путь>)
│   #   [LOCALE]: ru-RU,en-US,zh-CN перед install/enable (динам.) и install (стат.)
│   #   [R] в меню — полная проверка ADB + пауза на клавишу (не смахивается cls)
└── logshot/                       # Go-инструмент shot: снимок logcat + охота за
    #   runtime-фразой (-q/-cjk/-offline), не в APK (шина/TBOX/scene.db/TTS)
    #   build.sh - gofmt+vet+тесты+сборка shot/shot.exe в корень
#   СОСТАВ АРХИВА РЕЛИЗА (deepal-HU-translate_v3.1.2-N.zip):
#   README.md (руководство пользователя, с v3.1.2-13), deepl, deepl.exe,
#   targets.txt, manage.bat + collect_report.bat (динамические; без
#   collect_report.bat пункт [7] report падает),
#   apks_rro_min/ (200 .apk+.idsig), apks_rro_static/ (200).
#   НЕ входит: manage_static.bat (статика — из git), остальные документы
#   (WORKFLOW/TODO/CHANGELOG/MEMORY), shot*, keys/.
```

---

## Зависимости (путь)

```
Java/Python:
  /usr/bin/aapt2                — компиляция XML в Android
  /usr/bin/apksigner            — подпись APK
  apktool jar                     (в PATH)

Android SDK (android.jar):
  /opt/android-sdk/platforms/android-34/android.jar
  /opt/android-sdk/build-tools/34.0.0/aapt
  /opt/android-sdk/build-tools/34.0.0/aapt2

Python:  pip3 install requests

Go:  go + gofmt (1.22+)       — build.sh (deepl) и logshot/build.sh (shot);
                                  готовые бинарники в корне/релизах
```

---

## Референс скриптов

| Скрипт | Назначение |
|--------|-----------|
| `extract_cjk.py` | Извлечение из `decompiled/`: все строки (strings, arrays, plurals) из values/ и values-zh/ в `translations/*.json`. **Обнуляет `ru`** (см. шаг 2) |
| `scripts/decompile_apks.py` | Декомпиляция APK из original_apks/ в decompiled/ (apktool) |
| `generate_overlays.py` | Формирование `overlays/<app>/` (динамические — для `adb install`); пишет RU в `res/values-ru/` **и** дубль в `res/values/` (default-конфиг: RU применяется и при локали ГУ ≠ ru); `--only <App>` |
| `create_rro_min.py` | Сборка APK динамических оверлеев (aapt2 compile + link, подпись `keys/platform.jks`); `--only <App>` |
| `generate_overlays_static.py` | Формирование `overlays_static/<app>/` (статические, `isStatic="true"`) |
| `create_rro_static.py` | Сборка APK статических оверлеев (аналог min); `--only <App>` |
| `generate_summary.py` | Итоговая таблица: сколько строк, сколько переведено, сколько APK (source of truth статуса) |
| `scripts/quality_check.py` | Аудит качества (CJK в ru, пустые поля); отчёт в `/tmp/quality_report.json` |
| `create_db.py` | SQLite-словарь переводов из `translations/*.json`; `--stats`, `--find <zh>`, `--find-ru` |
| `apply_db_translations.py` | Заполняет ПУСТЫЕ `ru` из SQLite-словаря (матч (app,name)→(app,zh)→уникальный cross-app + guard), `--dry-run` |
| `validate.py` | **Автопроверка перед сборкой**: форматтеры zh↔ru (порядок + %s/%%), CJK-in-ru, размеры/индексы массивов против источника, plurals few/many, package-префикс, дубли name; WARN-аудит ширины RU vs ZH (`--length-report FILE`, секция 1c); разбор APK через `aapt2 dump` (`--skip-apk` — без). **Exit 1 при ошибках** |
| `strwidth.py` | Метрика «ширины строки» (CJK=2, остальное=1) + пороги `--fit` (FIT_* env). Общая для `validate.py` и `improve_translations.py --fit` |
| `improve_translations.py` | **Перевод/улучшение строк в `ru`** (LLM). Режимы: `--all`, `--defective` (пустые/CJK/битые плейсхолдеры/дегенеративные), `--empty`, `--long` (длинные, по одной; обходит progress-фильтр), `--fit` (укорочение «раздутых»), `--styled` (строки с разметкой, по фрагментам, кэш `improve_styled_cache.json`), `--app <имя>`, `--limit`, `--resume`, `--fresh`, `--dry-run` |
| `promt.md` | Системный промпт для improve (HTML/экранирование, плейсхолдеры, глоссарий) |
| `test_improve_selftest.py` | Self-test: guard, specs, wake-word, aapt2 round-trip (compile+link+dump) |
| `make_bisect_overs.py [K][A][B][NAME]` | Диагностика: K «кусковых» RRO-APK с частью строк одного приложения (бисекция проблемного ресурса на ГУ) |
| `make_zh_control.py [N]` | Диагностика: RRO «контроль» — все/первые N ID таргета, но значения КИТАЙСКИЕ (разделяет «структура таблицы» vs «контент перевода») |
| `create_app_pipeline.sh` | Полный цикл: create_db → generate_overlays(±static) → create_rro_* |
| `export-to-db.sh` | create_db + validate (± dry-run apply_db) |

### Словарь (SQLite)

```bash
python3 create_db.py                 # пересобрать словарь из translations/*.json
python3 create_db.py --stats         # статистика
python3 create_db.py --find 蓝牙      # поиск по zh
python3 apply_db_translations.py --all --dry-run   # безопасно: только пустые ru
```

Не запускайте `apply_db_translations.py --all` **без** `--dry-run`, пока
`improve_translations --all` не дойдёт до конца.

### Переменные окружения improve_translations.py

`API_URL`, `API_KEY`, `API_MODEL` (по умолчанию Ollama
`http://10.0.0.128:11434/v1/chat/completions`, `qwen3.8:27b`), плюс
`API_LONG_THRESHOLD` (умолч. 200: строки `len(zh)>=` порога отправляются
**по одной** — одна длинная в батче на 8 роняла весь пакет), `API_BATCH_SIZE`,
`API_MAX_TOKENS`, `API_TIMEOUT`, `API_DEBUG`, `API_DRY_RUN`.
Прогресс — `translations/improve_progress.json` (resume/fresh; save после
каждого батча). `--fit` — свой прогресс `improve_fit_progress.json`
(попадают только УКРОЧЕННЫЕ; повторный прогон без `--fresh` не укорачивает
повторно); не укороченные «переширокие» — `logs/fit_stuck.txt` (ручное ревью),
аудит «до/после» — `validate.py --skip-apk --length-report …` (секция 1c, WARN).

### Префиксы пакетов и скрытие из лаунчера

- **динамические**: `com.android.vendor.translate.rro.<app>` — лаунчер скрывает
  такие пакеты (фильтр №1: `startsWith("com.android")`; детали —
  `docs/LAUNCHER_APP_HIDDEN_DESIGN.md`);
- **статические**: `com.deepal.translate.rro.<app>`, `isStatic="true"`,
  ставятся в `/vendor/overlay/<app>/` (root+remount).

Переводы в оверлее: `res/values-ru/` (для ru-локали) **+** дубль в `res/values/`
(default-конфиг: RU применяется и когда locale ГУ НЕ ru — fallback вместо zh
у WT-* / en у AOSP-*).

### Ограничение: 9 AOSP-target динамически НЕ ставятся (EXCLUDE9)

Проверено по живым логам 22.09 (CHANGELOG [2026-09-22b]): 9 оверлеев, целящих
чистые AOSP-пакеты — NetworkStack, MediaProviderLegacy, UserDictionaryProvider,
DownloadProvider, DownloadProviderUi, CompanionDeviceManager, MtpService,
CaptivePortalLogin, ContactsProvider — `adb install` отклоняет
(`INSTALL_FAILED_INTERNAL_ERROR`: «signed with different certificates, and the
overlay lacks <overlay android:targetName>»; подписаны AOSP-ключом, а RRO —
заводским; `overlay enable` не выполняется). **Решение (25.09): исключены из
`manage.bat`** (секция `EXCLUDE9` в `targets.txt`; не входят в GROUP1 ни при
`A`, ни при `auto`) — динамически ставится **91 из 100**. Перевод их строк —
только статикой (`manage_static.bat` автоматически добирает EXCLUDE9 в GROUP1,
т.е. в static ставятся 100).

### Нормализация перевода (`_sanitize_ru`, aapt2-safe)

`improve_translations.py` пишет модельный ответ в `ru` после нормализации:

| Было (модель) | Стало в `ru` | Зачем |
|---|---|---|
| `&amp;lt;`, `&amp;quot;`, `&amp;gt;`, `&amp;apos;` | `&lt;`, `&quot;`, `&gt;`, `&apos;` | двойное экранирование → одинарное, иначе `generate_overlays._esc()` даст `&amp;lt;` и aapt2 FAIL |
| `'`, `&apos;`, `&#x27;` | ‘ (U+2019) | aapt2 отклоняет одиночный `'` и даже `&apos;` (проверено compile-матрицей) |
| живые `\n`, `\r`, `\t` | литеральные `\\n`, `\\t` | конвенция ресурсов: `\n` в XML — два символа |
| `%s`, `%1$d`, `%.1f`, `%%`, URL, «» | не трогается | паритет форматтеров гонится guard'ом |

### Лимит aapt2: строка ≤ 32 700 UTF-8-байт

aapt2 молча режет значение >32767 байт в «STRING_TOO_LARGE» (warning, не
error). Генераторы (`AAPT2_MAX_BYTES=32700`) **не пишут** такие строки в
оверлей (на ГУ остаётся текст источника), печатают `[WARN]`; `create_rro_*`
FAIL'ит приложение; `validate.py` — STRING_TOO_LARGE в APK = ошибка.
Текущий состав (6 строк): 4 больших юридических документа WT_FusionNavigation +
`road_book_my_agreement_two_content` + WT_GameCenter/`str_procotol`. Перевод
сохранён в `translations/*.json`; для включения нужен split на несколько
ресурсов (правки приложения).

### Строки, НЕ попадающие в оверлей (`translations/exclude.json`)

Некоторые строки приложениями используются **как данные/ключи**, а не как
надписи — их RU-перевод ломает логику. Классический пример (25.09):
`dialog_part_*_title_text` в WT_AutoMaintenance сравниваются
(`getString(id).equals(push.getMaintenanceProgram())`) со **значением сервера**
(всегда китайским) — RU-ключ = сравнение ложно = карточки «Умного
обслуживания» не заполнялись («Осталось - - км»). Механизм:
`exclude.json` = `{"App": {"name": "причина"}}`; генераторы читают его
(`load_excludes()`/`filter_excluded()`) и не пишут перечисленные строки в
оверлей (на ГУ остаётся zh); RU-перевод **остаётся** в `translations/<app>.json`
(справка/будущий репак, не удалять); `exclude.json` исключён из ignore-списков
всех скриптов, читающих `translations/*.json`. Состав (25.09):
`WT_AutoMaintenance` — 8 строк `dialog_part_*_title_text`;
`WT_HDCloudCamera` — `setting_guardian_mode`, `cruise_protect_model` (ключи
голосовых команд). **Осознанное ограничение:** заголовки «Умного
обслуживания» (空调滤芯/制动液/减速器油) остаются **китайскими** — данные
с сервера, рессурса нет; RU-вариант = только smali-репак (решено не делать).
После правки `exclude.json`/`ru`: `generate_overlays* → create_rro_* --only <App> → validate.py`.

## Шаг 1. Декомпилировать APK

```bash
python3 scripts/decompile_apks.py                     # все APK из original_apks/
python3 scripts/decompile_apks.py --app WT_Launcher   # одно конкретное
```

Результат: `decompiled/<app>/AndroidManifest.xml`, `apktool.yml`, `res/values-*/`

---

## Шаг 2. Извлечь строки в JSON

> **⚠️ ВНИМАНИЕ (21.09): `extract_cjk.py` ОБНУЛЯЕТ `ru`.** `main()`
> перезаписывает каждый `translations/<app>.json` целиком, значение
> `ru` — пустым. **Не гонять extract_cjk.py после завершённого перевода**
> (17 840 строк уже переведены). Запускайте ТОЛЬКО если в `decompiled/`
> добавились новые приложения/фразы И вы готовы перезапустить LLM-прогон.
> Извлечение сейчас (21.09) 1:1 совпадает с текущим JSON: 17 832 plain
> (regex-путь, zero-drift) + 13 styled (подраздел ниже).

```bash
python3 extract_cjk.py
```

Что делает:
- Сканирует `decompiled/<app>/res/values-zh/` → поля `zh`
- Сканирует `decompiled/<app>/res/values-ru/` → поля `ru` (уже существующие переводы)
- Формирует `translations/<app>.json`, структура:
```json
{
  "name": "app_name",
  "type": "string",
  "zh": "系统",
  "ru": "Система"
}
```
- Формирует список объектов, которые будут переводиться.

**Изоляция plain vs styled (zero-drift, 21.09):** plain-строки извлекаются
старым raw-regex'ом на битах файла (17 832 значения zh побайтово как раньше).
Строки с вложенной разметкой (`<b>/<a>/<annotation>/<Data>…`) regex обрывал на
первом `<` — их было 13, они **никогда** не попадали в JSON. Теперь их
забирает ET-путь (`_string_inner_xml` — ручной обход дерева, минимум-escape
`&`/`<`, без дублирования `tail` и без `&gt;`-дрейфа) и помечает
`"styled": true`. Plurals/arrays — всегда через ET/`itertext()` (полный текст
включая вложенное). При `ParseError` файла — фолбэк на regex, как до 21.09.
Перевод `styled`-строк — только по фрагментам: `improve_translations.py
--styled` (разметка сохраняется побайтово, переводятся лишь текстовые
фрагменты; кэш `translations/improve_styled_cache.json`).

---

## Шаг 3. Перевести / улучшить строки (LLM-прогон напрямую в `ru`)

Единственный путь переводов — `improve_translations.py` (промпт — `promt.md`):

```bash
# dry-run (без отправки):
API_URL=http://10.0.0.128:11434/v1/chat/completions API_KEY=ollama \
API_MODEL=qwen3.8:27b python3 improve_translations.py --dry-run

# точечно дефектные (пустые/CJK/битые плейсхолдеры) или всё:
... python3 improve_translations.py --defective
... python3 improve_translations.py --app WT_Launcher
... python3 improve_translations.py --all --resume   # фоновый: nohup ... &

# только длинные (len(zh)>=API_LONG_THRESHOLD, умолч. 200) и дефектные,
# каждая по одной; обходит progress-фильтр (повторно забирает упавшее):
... python3 improve_translations.py --long

# УКРОТИТЬ «раздутые» короткие UI-строки (RU шире ZH → перенос на 2 строк;
# RRO не трогает layout виджетов). Два лимита: мягкий (принимается только
# КОРОЧЕ текущего по ширине и <= cap), затем жёсткий (повтор с max_chars в
# пайлоаде). Не укороченные — как были + logs/fit_stuck.txt. Свой прогресс:
# improve_fit_progress.json (повтор без --fresh не укорачивает повторно).
... python3 improve_translations.py --fit
# остался ли «переширокий» остаток — аудит validate (секция 1c, WARN):
python3 validate.py --skip-apk --length-report logs/fit_report.txt
```

`improve_translations.py`:
- Для каждой строки из `translations/<app>.json` вызывает Ollama с текстом на китайском,
  результат пишет в поле `ru` той же записи.
- Системный промпт — `promt.md` (плейсхолдеры parité+порядок, HTML-теги как в zh,
  wake-word `***你好***` не переводится, глоссарий).
- **Длинные строки** (`len(zh) >= API_LONG_THRESHOLD`, по умолч. 200) отправляются
  **по одной**: одна длинная в батче на 8 переполняла/обрезала ответ модели и роняла
  весь батч (включая соседей). `--long` — режим именно для них.
- **CJK-ремонт** (`_repair_cjk`): если в длинном переводе остались китайские
  фрагменты, модель не гоняют на полный повтор (5+ минут, роняет готовое и
  оставляет другие фрагменты) — отправляется маленькое русское «окно» (±50
  симв.) вокруг каждого фрагмента, модель возвращает окно исправленным, окно
  подставляется обратно (lenient-парсер на кривой JSON модели). Полный повтор —
  только фолбэк.
- Guard перед записью: паритет+порядок форматтеров, обязательный wake-word, CJK
  (кроме whitelist), дегенеративные ответы (`none/null/…`; `нет`/`неизвестно` НЕ
  в списке — это легитимные переводы), для длинных ещё `len(ru) >= 0.25·len(zh)`
  → при сбое `ru` НЕ затирается; retry с hint'ом на форматтеры, точечный
  CJK-ремонт, полный повтор с hint — фолбэк.
- Нормализация (`_sanitize_ru`): double-escape → single → raw; `'`→’ U+2019;
  живые `\n`/`\t` → литеральные. `%s`, `%d`, `%%`, URL, «» “” не трогаются.
- Прогресс — `translations/improve_progress.json` (resume/fresh), save после каждого
  батча. Не-`--all` режимы (`--defective`/`--empty`/`--long`) НЕ фильтруются
  progress'ом (умеют повторять упавшее) и НЕ стирают его прогресс (merge).
- `--fit` — укорочение «раздутых» коротких UI-строк (`strwidth.py`: короткие zh по
  ширине + ru заметно ширее — soft-порог 2.5x/24w). Два лимита последовательно:
  **мягкий** — guard принимает ответ только если он КОРОЧЕ текущего по
  `display_width` И `len <= hard_cap`; **жёсткий** — повторный запрос с `max_chars`
  в пайлоаде (cap в символах, `min(28, 2·zh_w + 8)`), guard — `len <= max_chars`.
  Не укоротилось — текущий `ru` НЕ затирается, строка в `logs/fit_stuck.txt`
  (ручное ревью). Свой прогресс — `improve_fit_progress.json`: попадают только
  УКРОЧЕННЫЕ (повторный прогон без `--fresh` не укорачивает повторно — нет риска
  переусечения смысла); stuck строки повторятся в следующую итерацию.
  Аудит «до/после» — `validate.py --skip-apk --length-report …` (секция 1c, WARN).
- При запуске в фоне: `nohup env API_URL=... API_KEY=ollama API_MODEL=qwen3.8:27b \
  python3 improve_translations.py --all --resume >> logs/improve_translations.log 2>&1 &`

> Исторический пакетный путь (`translate_one.py` / `translate_batch.py` → `new_ru` →
> `apply_db_new_ru.py`) удалён 18.09 — см. CHANGELOG [2026-09-18].

## Шаг 4. Автопроверка (validate.py) — ОБЯЗАТЕЛЬНО перед сборкой

```bash
python3 validate.py            # всё, включая разбор APK через aapt2 dump
python3 validate.py --skip-apk # только translations/overlays (быстро)
```
Exit 1 при ошибках: форматтеры zh↔ru (порядок), CJK-in-ru, размер/индексы массивов
против источника, plurals few/many, package-префикс, дубли name внутри приложения.

## Шаг 5. Собрать RRO APK из перевода

```bash
python3 generate_overlays.py                          # для всех приложений
python3 generate_overlays.py --only WT_Launcher       # одно приложение
python3 create_rro_min.py                             # собрать все APK
python3 create_rro_min.py --only WT_Launcher          # собрать одно APK
```

`generate_overlays.py`:
- Генерирует `overlays/<app>/AndroidManifest.xml` (с `targetPackage` из `decompiled/<app>/AndroidManifest.xml`, package `com.android.vendor.translate.rro.<app>` — скрытие из лаунчера)
- Генерирует переведённые строки в `overlays/<app>/res/values-ru/` (для ru-локали) И в `res/values/` (дубль, default-конфиг: RU применяется и при локали ≠ ru)

`create_rro_min.py`:
- Копирует `overlays/<app>/` в `overlay_build/<app>/`
- Компилирует XML-файлы через `aapt2 compile`
- Создаёт APK через `aapt2 link` (с `--manifest`, `--auto-add-overlay`, `--no-resource-removal`)
- Подписывает APK через `apksigner` (ключ `keys/platform.jks`).

Результат: `apks_rro_min/<app>_RRO.apk`

### Исключения строк из оверлея (`translations/exclude.json`)

Некоторые строки используются приложением **как данные/ключи**, а не как
надписи — их RU-перевод ломает логику. Классический пример (решено 25.09,
«Умное обслуживание» показывало `Осталось - - км` вместо цифр): id
`dialog_part_*_title_text` в `MainActivity.R()`/`MaintainUtil`
сравниваются (`getString(id).equals(push.getMaintenanceProgram())`) со
**значением сервера** (всегда китайским) — RU-перевод ключа = сравнение
ложно = карточки не заполняются.

Механизм:
```
translations/exclude.json  →  { "App": { "name": "причина" }, ... }
```
- `generate_overlays{,_static}.py` читают его (`load_excludes()`),
  `filter_excluded()` убирает перечисленные строки до записи в
  `overlays*/<app>/res/values-ru/` — на ГУ остаётся **исходное (zh) значение**;
-RU-перевод таких строк **остаётся** в `translations/<app>.json` (`ru`) —
  как справка/для будущего smali-репака, НЕ удалять;
- `exclude.json` уже исключён из ignore-списков всех скриптов, читающих
  `translations/*.json` (generate*, validate, improve_translations,
  apply_db_translations) — он не воспринимается как приложение.

Текущий состав (25.09): `WT_AutoMaintenance` — 8 строк
`dialog_part_*_title_text`; `WT_HDCloudCamera` — `setting_guardian_mode`,
`cruise_protect_model` (ключи голосовых команд: сравнение с
`json.optString("Mode")`). После изменения `exclude.json`/`ru`:
`generate_overlays* → create_rro_* --only <App> → validate.py --app`.

**Осознанное ограничение:** заголовки карточек «Умного обслуживания»
(空调滤芯/制动液/减速器油) останутся **китайскими** — это данные сервера
(`setText(program)`, ресурса нет), RU-вариант = только smali-репак
(решено **не делать**, имена деталей — данные каталога производителя).
Цифры («Осталось N км / дн.») — по-русски. Живая проверка пройдена 25.09.

---


## Шаг 6. Установить RRO APK на ГУ

Для динамических RRO APK (из `apks_rro_min/`) — bat запускается на Windows-машине
с adb (пакеты `com.android.vendor.translate.rro.*` — bat их «знает»):

```bat
REM Единый менеджер динамических RRO:
manage.bat                                    REM интерактивное меню
manage.bat install 8                          REM install + enable, top-8
manage.bat install A                          REM все группы (1+2+3+4)
manage.bat disable 8
manage.bat diag
manage.bat report                             REM zip-логи для анализа
manage.bat enable 2 / disable 3 / uninstall A REM остальные режимы
```

> В интерактивном меню всегда виден баннер статуса ADB (быстрая проверка) и
> пункт **[R]** «проверить заново» (полная проверка + рестарт демона; в
> `deepl` результат [R] удерживается паузой «Нажмите любую клавишу...» —
> без неё cls смахивал вывод). Меню появляется и без подключённого
> устройства — после подключения HU жмите [R].
> Установка/enable/disable/uninstall пишутся в `logs\ops_<mode>_<TS>.log`.

**То же самое через Go-менеджер `deepl`** (бинарник из релиза/репозитория,
читает тот же `targets.txt` и APK из тех же каталогов):

```bat
deepl.exe                 REM меню (динамические)
deepl.exe dyn install 8   REM install + enable, top-8
deepl.exe stat install A  REM статические, все группы
deepl.exe report dynamic  REM zip-логи
deepl.exe reboot          REM перезагрузить ГУ
```

Поиск adb у `deepl` (bat требует `where adb` — platform-tools в PATH):
`DEEPL_ADB=C:\путь\adb.exe` → PATH → **adb.exe рядом с deepl.exe**; выбранный
путь печатается `[ADB] using: <путь>`. Перед install/enable **оба**
менеджера сами выставляют `system_locales=ru-RU,en-US,zh-CN` (строки
`[LOCALE] было/стало` в консоль и ops-лог): `manage.bat` — подпрограмма
`:EnsureRuLocale` (добавлена 27.10; live-фикс WT_VehicleCenter по-китайски
при локали ГУ без ru), `deepl` — аналог. Без этой строки при en/zh-локали
ГУ русские строки молча не проявляются (см. FAQ ниже,
«Частично не переведено» → «Локаль ГУ»).

Или вручную (adb с любой ОС):
```bash
adb install apks_rro_min/AdayoAPA_RRO.apk
adb shell cmd overlay enable --user 0 com.android.vendor.translate.rro.adayoapa
```

---

## Шаг 7. Собрать статические RRO APK

Статические RRO работают иначе: они размещаются в `/vendor/overlay/` и активны сразу после загрузки. Они не могут быть отключены пользователем.

```bash
python3 generate_overlays_static.py                   # для всех
python3 generate_overlays_static.py --only WT_Launcher
python3 create_rro_static.py                          # собрать все APK
python3 create_rro_static.py --only WT_Launcher
```

`generate_overlays_static.py`:
- Генерирует `overlays_static/<app>/AndroidManifest.xml` с `package="com.deepal.translate.rro.<app>"`, `isStatic="true"`.
- Генерирует переведённые строки в `overlays_static/<app>/res/values-ru/` (для ru-локали) И в `res/values/` (дубль, default-конфиг — как в динамических).

`create_rro_static.py`:
- Копирует `overlays_static/<app>/` в `overlay_build_static/<app>/`
- Компилирует XML-файлы через `aapt2 compile`
- Создаёт APK через `aapt2 link`
- Подписывает APK через `apksigner`.

Результат: `apks_rro_static/<app>_RRO.apk`

---

## Шаг 8. Установить статические RRO APK на ГУ

```bat
REM Менеджер статических RRO (root, /vendor/overlay):
manage_static.bat                REM интерактивное меню
manage_static.bat install 8      REM 8 критичных (top-8)
manage_static.bat install A      REM все группы
manage_static.bat uninstall A
manage_static.bat report         REM zip-логи для анализа

REM или через Go-менеджер:
deepl.exe stat                   REM меню (статические)
deepl.exe stat install A
deepl.exe stat report
```

Или вручную:
```bash
adb root && adb remount
adb shell mkdir -p /vendor/overlay/AdayoAPA
adb push apks_rro_static/AdayoAPA_RRO.apk /vendor/overlay/AdayoAPA/
adb reboot
```

---

## Шаг 9. Собрать логи для анализа (report)

Если перевод на ГУ работает не так (строки не меняются, часть приложений
не переведены, приложения падают) — собрать диагностический пакет:

```bat
manage.bat report            REM динамические:  logs\deepal_dynamic_report_<TS>.zip
manage_static.bat report     REM статические:   logs\deepal_static_report_<TS>.zip
deepl.exe report             REM динамические (Go-менеджер)
deepl.exe report stat        REM статические (Go-менеджер)
```

`collect_report.bat` (вызывается обоими) собирает **только чтение** — ничего
на устройстве не меняет и не удаляет:
1. `raw/20_packages_ours.txt` — `pm list --show-versioncode` наших пакетов
   (установлены ли, какой versionCode);
2. `raw/25_overlay_all.txt` — полный `cmd overlay list --user 0`;
3. `raw/30_per_package.txt` — построчная сводка по 100 целевым пакетам
   (ver + state оверлея [x]/[ ]/---/нет);
4. `raw/10_locale.txt` — locale системы (перевод применяется к ru?);
5. `raw/26_vendor_overlay.txt` — (только static) `ls -l` наших папок в
   `/vendor/overlay/` (сам запрошу `adb root`, если не видно);
6. `raw/40_crash.txt`, `raw/41_fatal.txt`, `raw/42_anr.txt` — сбои:
   crash buffer / FATAL EXCEPTION / ANR (при пустом `/data/anr` сам
   запрошу `adb root` и дамплю trace);
7. `raw/00_device.txt` (getprop+uptime+df), `raw/02_apk_local.txt`
   (APK на хосте, что планировалось ставить);
8. **Логи установки/работы:** `ops_*.log` (последние 10 из `logs\`, пишутся
   самими менеджерами при install/enable/disable/uninstall) + `raw/43_main.txt`
   (`logcat -b main`, работа apk) + `raw/44_overlay_main.txt` (строки
   overlay/idmap/наши пакеты) + `raw/45_overlay_dump.txt` (`cmd overlay dump`
   оверлеев в ошибке `---`);
9. `summary.txt` — сводка с авто-выводом «есть ли проблемы»
   (не установлено / оверлеи выключены / FATAL / ANR). **Этот файл — отправлять.**

Архив `logs\deepal_<scheme>_report_<TS>.zip` и передавать на анализ.

---

## Шаг 10. Скрыть RRO APK из списка приложений в Launcher

Launcher имеет семь уровней скрытия приложений. Подробное описание: `LAUNCHER_APP_HIDDEN_DESIGN.md`.

Для скрытия RRO-приложения из списка:

**1 способ (рекомендуется):** Добавить package в `exclude_apps`:
```bash
python3 scripts/decompile_apks.py --app com.tinnove.comlib
python3 extract_cjk.py
API_URL=... API_KEY=ollama API_MODEL=qwen3.8:27b \
  python3 improve_translations.py --app com.tinnove.comlib
python3 generate_overlays.py
python3 create_rro_min.py --only com.tinnove.comlib
# Затем в overlays/com.tinnove.comlib/res/values-ru/strings.xml добавить:
# <string-array name="exclude_apps">
#   <item>com.deepal.translate.rro.adayoapa</item>
# </string-array>
```

**2 способ:** Указать package с "исключаемым" префиксом:
- `com.qualcomm.overlay.<app>`
- `com.huawei.overlay.<app>`
- `android.ext.overlay.<app>`

Launcher автоматически исключает эти пакеты в `loadAllApps()` по `startsWith()`.

---

## Шаг 11. Аудит качества переводов

```bash
python3 scripts/quality_check.py
```

Что делает:
- Сканирует все `translations/*.json`.
- Подсчитывает: `zh_only`, `en_only`, `ru_only`, `zh_ru`, `en_ru`, `all_three`.
- Находит CJK в `ru` (ошибка, если модель вернула оригинал).
- Находит непереведённые CJK-строки.
- Определяет строки, которые не нужно переводить (hotkeys, format strings).
- Сохраняет полный отчёт в `/tmp/quality_report.json`.

---

## Шаг 12. Перевод зашитых в layout надписей — ВАРИАНТ ОТКЛОНЁН (25.09)

Решение пользователя: проект переводит только через RRO; «бинарный патч AXML
внутри исходных APK» свёрнут, все артефакты (axml.py, scan/translate/patch/
verify, install/revert_patched.bat, layout_literals.json + логи) вынесены в
`/home/user/projects/deepal-HU-translate-bak-repack/`. На ГУ никакие
patched-APK не устанавливались — откат не требуется. История: CHANGELOG
[2026-09-22], [2026-09-25], 5.14. Не покрыто RRO никогда: assets/ и res/raw/
(голосовые хотворды WT_TSpeech, carconfig WT_MLWecarControl, virtual_data.xml)
— см. TODO.md.

## Полезные команды

```bash
# Аудит качества переводов
python3 scripts/quality_check.py

# Итоговая таблица по всем приложениям
python3 generate_summary.py

# Список всех декомпилированных APK
ls decompiled/ | head -20

# Размер файлов в translations/
du -sh translations/

# Список всех RRO APK
ls -lh apks_rro_min/

# Отладка лаунчера:
adb shell pm list packages --user 0
adb shell settings get global DriveMode
```

---

## Частые проблемы

### "Empty response from Ollama"
- `curl -s http://10.0.0.128:11434/api/tags` — проверить Ollama
- Перезапустить скрипт с `--resume`

### "APK already decompiled"
- `rm -rf decompiled/MyApp`
- Запустить `python3 scripts/decompile_apks.py --app MyApp`

### "No translations to translate"
- `grep -r 你 translations/MyApp.json | head -5`

### aapt2 compile FAIL: `&amp;lt;`, `unescaped apostrophe`, `not a valid string`
- Проблема в двойном HTML-экранировании или апострофах. Все модельные ответы нормализуются автоматически перед записью в `ru` (`_sanitize_ru` в `improve_translations.py`).
- Проверить конкретную строку: `grep -n "dialog_privacy_policy_msg_start_up" translations/WT_MultiMediaCenter.json`.
- После ручной правки JSON прогоните `validate.py` и `test_improve_selftest.py`.

### `deepl`: «adb не найден: нет в PATH и нет рядом с бинарником» / нет подключенных устройств
- adb ищется в порядке: env `DEEPL_ADB=путь` → PATH → `adb.exe` рядом с `deepl`/`deepl.exe`; выбранный путь печатается `[ADB] using: <путь>`.
- Положите `adb.exe` в ту же папку, что бинарник, или `set DEEPL_ADB=C:\путь\adb.exe && deepl.exe`, либо добавьте папку platform-tools в PATH.
- Если adb поднят, но подключение не установилось — два разных состояния:
  - **«Устройство не найдено»** (ГУ не виден) — подключите USB-кабель к ГУ и включите ADB: **инженерное меню ГУ или adbauth** (настроить «Для разработчиков»/нажимать «АВТОРИЗОВАТЬ» на ГУ невозможно — нет доступа);
  - **«Устройство НЕ АВТОРИЗОВАНО»** (unauthorized) — ADB на ГУ уже активен, но **публичный ключ adb этого ПК не добавлен на ГУ**: запустите **adbauth** (на этом ПК) и добавьте публичный ключ adb этого ПК (`adbkey.pub`);
  - в меню [R] — полная проверка (3 попытки + рестарт демона), вывод удерживается паузой «Нажмите любую клавишу...».

### `deepl`: «не найден targets.txt»
- Файл ищется в CWD и всех родительских папках (вверх) либо задаётся env `DEEPL_TARGETS=/путь`. Положите `targets.txt` рядом с бинарником — единый источник групп (91/8/9), эталон — корневой `targets.txt` из репозитория.

### «Частично не переведено» / «какой-то раздел по-китайски»
- **Локаль ГУ:** проверьте, что `system_locales` начинается с `ru-RU`
  (`adb shell settings get system system_locales`; должно быть
  `ru-RU,en-US,zh-CN`). Менеджеры выставляют это сами перед install/enable
  (строка `[LOCALE]` в ops-логе) — но уже запущенные приложения увидят RU
  только после своего перезапуска (force-stop из recent) или reboot ГУ.
  Причина: при локали без ru точное совпадение конфигурации цели (values-en/
  values-zh-rCN) разрешается выше default-`values/` нашего оверлея.
- **9 AOSP-пакетов (NetworkStack/Providers/MTP/…):** их строки в динамике
  не работают (EXCLUDE9 — не ставятся `adb install`); перевод есть только в
  статике (`manage_static.bat` → `/vendor/overlay`).
- **Строки-данные:** заголовки «Умного обслуживания» и ряд строк-ключей
  намеренно НЕ переведены (`translations/exclude.json`, серверные данные) —
  см. референс «Строки, НЕ попадающие в оверлей».
- **6 строк-«гигантов»** (юридические документы) не влезли в лимит aapt2
  (32700 байт) — на ГУ исходный текст; список — TODO.md.
- Всё остальное: диагностика ([6] в меню) + report ([7]) — zip и
  `summary.txt` отвечают, что именно не установлено/не включено.

### `manage.bat`: после строки `[TGT] … - строк с целями: N` окно замирает (меню не появляется)
- Симптом живого наблюдения (28.10): после `[TGT]` окно не обновляется и висит.
  Меню рисуется только ПОСЛЕ быстрой проверки `adb devices` (`:ADB_CHECK_FAST`)
  и `cls` — т.е. застрял `adb devices` (сам скрипт не блокируется ничего
  другого).
- Причины: зависший adb-демон (кратковременно обрывавшееся USB, старая
  сессия с другим клиентом), кабель «зарядка-только», USB-хаб.
- Лечение в отдельном окне: `adb kill-server` (иногда —
  `adb kill-server && adb start-server`), переткнуть кабель, затем
  `adb devices`. После — [R] в меню `manage.bat`. Если `adb devices` в
  отдельном терминале тоже висит — проблема в USB-связи, а не в скрипте.
- Усложнить bat-менеджер таймаутом на `adb devices` можно, но это ломает
  проверенную плоскую форму; сначала — `kill-server` (решение 28.10).

---

## История версий

Только последние (вся история — `CHANGELOG.md`):

| Версия | Дата | Изменения |
|--------|------------|---------------------------------------------------------------------|
| 5.25 | 2026-10-28 | **`locale-restore`** — снять перевод системы: вернуть OEM-локаль zh-CN (обратная операция install/enable). deepl: CLI + меню [9] обеих схем (без root/цели/ops-лога); manage*.bat: пункт [9] + CLI (`:RestoreOrigLocale`). Фикс CLI-ветки: режимы без цели (status/diag/report/reboot/locale-restore) сразу в RUN — раньше reboot/locale-restore падали в group-меню. Wine-smoke 4 CLI-сценария. Релиз v3.1.2-15 |
| 5.24 | 2026-10-28 | **Нумерация релизов: `v<прошивка>-N`.** Все релизы v3.2.1…v3.2.13 → **v3.1.2-1…v3.1.2-13** (git-теги + GitHub-релизы + zip + доки). Причина: не путать версию релиза с версией прошивки ГУ (3.1.2). Дальше v3.1.2-14, 15, …; при смене прошивки — v<новая>-1. Ограничение GitHub: ассет релиза не переименовывается (только delete+reupload) |
| 5.23 | 2026-10-28 | **README.md входит в архив релиза** (с v3.1.2-13) — пользователь видит руководство вместе с архивом. Состав = v3.1.2-12 + README.md (408 записей / 406 файлов) |
| 5.22 | 2026-10-28 | **Документация: README прост (пользователь), нюансы → WORKFLOW/CHANGELOG.** README переписан: установка из архива, меню, режимы, логи, troubleshooting, ссылки; dev-нюансы (таблица скриптов, LLM-режимы, env, нормализация aapt2, exclude.json, EXCLUDE9) перенесены сюда. TODO.md — только статус + открытые (история → CHANGELOG) |
| 5.21 | 2026-10-27 | **`manage.bat`: `:EnsureRuLocale`** — ru в начале `system_locales` перед install/enable (live-фикс 04.10: WT_VehicleCenter по-китайски при локали ГУ без ru). get→compare→`settings put ru-RU,en-US,zh-CN`+read-back; строки `[LOCALE]` в консоль+ops-лог; wine-smoke обеих веток. Релиз v3.1.2-12 |
| 5.20 | 2026-10-27 | **`manage.bat` + `collect_report.bat` в архив релиза** (динамические RRO на Windows-машине). Валидация перед релизом (smoke, wine 9 + фейковый adb.exe): install 8/enable A/disable 1/uninstall C/status/report; manage_static install 8. `manage_static.bat` в архив НЕ входит (статика — из git). v3.1.2-11 |
