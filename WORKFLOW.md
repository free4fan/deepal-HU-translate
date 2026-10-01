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
└── go/                            # Go-менеджер deepl (go.mod, cmd/, internal/)
    #   build.sh - gofmt+vet+тесты+сборка deepl/deepl.exe в корень
    #   CLI: deepl dyn|stat <mode> <preset>, deepl reboot
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
```

---

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
> пункт **[R]** «проверить заново» (полная проверка + рестарт демона). Меню
> появляется и без подключённого устройства — после подключения HU жмите [R].
> Установка/enable/disable/uninstall пишутся в `logs\ops_<mode>_<TS>.log`.

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

## Шаг 12. Перевод зашитых в layout надписей — ВАРИАНТ ОТКЛОНЁН (25.09), архив в /home/user/projects/deepal-HU-translate-bak-repack/

**Решение (25.09, пользователь):** репатированный вариант (бинарный патч
AXML внутри исходных APK) отменён — проект переводит только через RRO.
Все артефакты шага вынесены в бэкап
`/home/user/projects/deepal-HU-translate-bak-repack/`:
`{axml.py, scan_layout_literals.py, translate_layout_literals.py,
patch_apk_layouts.py, verify_patched_apks.py, install_patched.bat,
revert_patched.bat, layout_literals.json,
logs/{layout_literals.json, apk_patch_report.json, apk_patch_verify.json,
patched_map.txt, translate_layout_literals.log}}`.
`apks_patched/` (4.8GB) на данной машине не собирался — нечего было
переносить. На ГУ **никакие** patched-APK не установлены (live-проверка
окончательно не проводилась) — откат с машины не требуется.
(`original_apks/` 25.09 возвращён в проект — нужен RRO-конвейеру для
передекомпиляции, `scripts/decompile_apks.py`.)

**Контекст (исторически):** 56 APK / 1487 CJK-литералов в binary-XML
layout (RRO layout не покрывает). Инструмент был готов (byte-exact
round-trip 137/137) и собран, но не принят. Если когда-либо понадобится
вернуться: вернуть файлы из бэкапа, история — в CHANGELOG [2026-09-22]
и якорях MEMORY.md; установка/откат (data: `adb install -r -d` /
`pm uninstall`; system: push + remount) — в README бэкапа-бат'ов.

**Не покрывается RRO и не покрывалось этим шагом** (осталось открытым,
см. TODO 22.09): `assets/` и `res/raw/` (голосовые hotwords WT_TSpeech,
конфигурация WT_MLWecarControl, virtual_data.xml, speech yaml) и
CJK-константы в smali (аудит: 0 по примерам).

---

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

---

## История версий

| Версия | Дата | Изменения |
|--------|------------|---------------------------------------------------------------------|
| 5.18 | 2026-09-25 | **Отказ от reпак-варианта** (решение пользователя): файлы и данные шага 12 (axml.py, scan/translate/patch/verify, install_patched.bat, revert_patched.bat, layout_literals.json + логи) вынесены в `/home/user/projects/deepal-HU-translate-bak-repack/`; step 12 переведён в «ОТКЛОНЁН/архив»; TODO переписан; patched-APK на ГУ не были установлены (live-проверка не проводилась). `original_apks/` в бэкап НЕ отправлен (и туда не попал) — остался/вернулся в проект, т.к. нужен RRO-конвейеру (декомпиляция) |
| 5.17 | 2026-09-24 | **Живой log `logs/bat/` (report 093642): `collect_report.bat` рвался на [7/9], отчёт обрывался пополам.** Корень: `collect_report.bat:270` — вторая строка многострочного `REM` в блоке дампа ANR **без приставки `REM`** (выпала при переносе) → cmd исполняет её как команду, неэкранированная `)` в `безвредно)` закрывает `if !N_ANR! GTR 0 (` досрочно, хвостовой `.` → `. was unexpected at this time.` → блок рвался до дампа трассы, [8/9]/[9/9]/`summary.txt`/zip не выполнялись. Wine-репро идентично живому обрыву. Починено: `REM` на :270. Второй (неблокирующий) дефект того же прогона — `The system cannot find the file specified.` в banner `:CollectReport`: литеральный `<TS>`/`<pkg>` в `echo` = input-redirection → экранировано `^<…^>` в `manage.bat:567`, `manage_static.bat:540`, `install_all_static.bat:50`. Установка в прогоне 24.09 в порядке: 91/100 (9 AOSP-target ожидаемо не ставятся), WT_WtSystemUI `[ ]` (vendor-баг enable), wecarspeech FATAL (не наш) |
| 5.16 | 2026-09-23 | **Итерация 2 по live-репорту 22.09 21:26.** Подтверждено: 22b-чинки работают (91/9, phonelink `[x]`, ver виден, 0 `---`). Новая волна починки: ops-логи были ПОТЕРЯНЫ (имя `ops_…_OTST:=_.log` — замена ПАРЫ `!X:=_!` в `:OpSTs` дала битое имя с `:`, файл не создавался) → таймстемп wmic/powershell `YYYYMMDDHHMMSS`; FATAL-счётчик (`find /C` = `---------- N C(S) FOUND`, токен1=дефисы → «---------- 0 - всё чисто» при 4 FATAL) → построчный счётчик как N_ANR; ANR-trace Permission denied (`anr_*`=system:system 600) → `adb root` ВСЕГДА перед дампом; `overlay enable` в install был скрыт (WT_WtSystemUI стоял `[ ]` без следов) → `:DoOpAdb` с выводом + retry + `[WARN]` в лог; хрупкий `"%%I"=="Success"` (хвостовой пробел/CR) → префикс-матч 7 символов. Summary: ровно 9 отсутствующих = «ОЖИДАЕМО» (9 AOSP-target) |
| 5.15 | 2026-09-22 | **Живые логи HU (logs/bat/ + report-zip): manage.bat/отчёт починены.** Корень false-`[ERROR]`/«OK: N / Ошибок: N» — неэкранированные скобки в `RESULT:`-строках ops-лога (та же класс-бага, что 5.12, но на строках в ЛОГ; экранированы в manage/manage_static + 3 Legacy-обёртки; wine-репро до/после). Формат `pm list` этого ГУ = `package:PKG versionCode:N` (пробел, без `=`) → парсер отчёта новый (было ложное 0/100, стало 90/10; `tokens=3 delims=:`). GROUP3 `WT_PhoneLink`→`PhoneLink` (файл `PhoneLink_RRO.apk`, пакет `…rro.phonelink`). ANR-dump: `head -150 /data/anr/$f` (полный путь). **Найденные на устройстве:** 9 dynamic-оверлеев с AOSP-target не ставятся `adb install` (DELETE_FAILED_INTERNAL_ERROR, нет в overlay list) — решение: static или исключение из групп; WT_WtSystemUI STATE_DISABLED → enable; vendor-краш wecarspeech (NumberFormatException, не наш) |
| 5.14 | 2026-09-22 | **Зашитые в layout надписи** (RRO не покрывает) — путь собран, **25.09 ОТКЛОНЁН пользователем, архив вынесен** в `/home/user/projects/deepal-HU-translate-bak-repack/` (см. шаг 12). История: `scan_layout_literals.py` (122 APK → 56 apps/1487 unique/3761 uses); `translations/layout_literals.json` 1487/1487; `axml.py` (AXML read/write, byte-exact 137/137) + `patch_apk_layouts.py` → apks_patched/ 56 APK, подпись platform.jks 56/56, cjk_left=0. RRO layout override доказан невозможным (aapt2). Не покрыто: assets/raw + smali (0) — см. TODO |
| 5.13 | 2026-09-21 | Извлечение «чисто»: plain — regex (zero-drift), **markup-строки (`<b>/<a>/<annotation>/<Data>`) — ET** (`_string_inner_xml`, ручная сериализация) + `"styled": true`; plurals/arrays через ET/`itertext`; fallback на regex при `ParseError`. aapt2 **32767-байтовый** лимит: >32700 → строка **не** в оверлей (на ГУ остаётся текст источника, а не «STRING_TOO_LARGE»); create_rro_* теперь **FAIL'ит** приложение при «too large» (aapt2-предупреждение в STDOUT, rc=0); validate.py: STRING_TOO_LARGE=ОШИБКА, байт-аудит >32700=WARN, CJK-ресурс ∉ JSON=WARN. styled-перевод по фрагментам — `improve_translations.py --styled` (кэш improve_styled_cache.json). Self-test 90/90. Предупреждение: `extract_cjk.py` ОБНУЛЯЕТ `ru` — не гонять после перевода |
| 5.12 | 2026-09-20 | Wine-проверка bat (headless 9.0 + mock-adb): синтаксис/структура/меню подтверждены; пойманы и починены 13 live-багов — 12 неэкранированных скобок в `echo` внутри `if (...)`-блоков (обоих менеджеров + report) и «ECHO is OFF» в summary (дефолты MODEL/REL/INC_LINE); `:DoOpAdb` на портативный `type "!F!"`. Ограничение wine задокументировано: adb-форматы `pm list`/`overlay list` и реальный report — только живая Windows+adb+HU |
| 5.11 | 2026-09-19 | Логи установки/работы: `logs\ops_<mode>_<TS>.log` у обоих менеджеров (вывод adb + RESULT по каждому пакету, попадает в report-zip); report: raw/43_main + 44_overlay_main (работа apk) + 45_overlay_dump (ошибочные оверлеи) + ANR с auto root и trace; починка парсера (pm list `=`/delims==, overlay list двойной поиск, fota≠fotaservice) |
| 5.10 | 2026-09-19 | `manage.bat`/`manage_static.bat`: меню появляется ВСЕГДА даже при недоступном adb (баннер статуса + пункт [R] «проверить заново»); убрана ADB-блокировка до меню (была `exit /b 1` в `:ADB_CHECK` и вечный 45-сек цикл ожидания авторизации) |
| 5.9 | 2026-09-19 | Сбор логов для анализа: `collect_report.bat` + режим `report` в `manage.bat` (пункт [7]) и `manage_static.bat` (пункт [5]); zip в `logs\deepal_<scheme>_report_<TS>.zip` (pm list, overlay states по 100 пакетам, locale, /vendor/overlay, crash/FATAL/ANR, summary с авто-выводом «есть ли проблемы») |
| 5.8 | 2026-09-18 | Ревью динамических bat: весь функционал сведён в `manage.bat` (install/enable/disable/uninstall/status/diag, группы 1-4 + TOP-8 + A/C/auto, CLI `manage.bat <mode> <preset>`, счётчики+сводка); `install_8/install_all/disable_8/diag` удалены (менеджер один — `manage.bat`) |
| 5.7 | 2026-09-18 | Tочечный CJK-ремонт длинных строк (`_repair_cjk`: окно ±50 симв., lenient-парсер, 3 retry, полный повтор = фолбэк); добита последняя строка road_book_my_agreement_two_content (9 273→33 152 ru): **17 827/17 827 (100.0%)**, 100/100 apps; self-test 42/42 |
| 5.6 | 2026-09-18 | Длинные строки (len(zh)>=200) — по одной (API_LONG_THRESHOLD, _make_chunks) + режим --long (обходит progress-фильтр); починка str(None)→"None" (res.get("ru") or ""); guard: DEGENERATE_ANSWERS + LONG_RATIO_MIN + CJK-hint-retry; починка обнуления progress (merge, не --resume → done=set()); self-test 37/37; доведено до 17 826/17 827 (1 строка) |
| 5.5 | 2026-09-18 | Удалён исторический путь translate (translate_one/batch, api_translate_ambiguity, scan_ambiguities, apply_db_new_ru, revert_cjk, run_pipeline); единственный путь переводов — improve_translations.py; добавлены create_app_pipeline.sh / export-to-db.sh; синхронизация документации |
| 5.4 | 2026-09-17 | add improve_translations.py + promt.md + validate.py + self-test; починка specs() (%s/%%, порядок); values-ru у обеих схем; массивы 0..N; plurals few/many; create_db/apply_db пофикс (матч (app,name)+guard) |
| 5.3 | 2026-09-17 | reverted префикс динамических — com.android.vendor.translate.rro.* (скрытие из лаунчера, LAUNCHER_APP_HIDDEN_DESIGN.md фильтр №1); bat'ы уже его использовали |
| 5.2 | 2026-09-17 | Починены 5 багов данных (см. REVIEW.md): 12 строк с %s/CJK, пустые array-элементы, plurals few/many |
| 5.1 | 2026-09-17 | added apply_db_new_ru.py (new_ru → ru), добавлена _sanitize_new_ru нормализация (%s, HTML-entities, apostrophes). Сборка 100/100 OK, 0 FAIL |
| 4.0 | 2026-09-13 | Полная актуализация по всем скриптам, добавлены статические RRO, скрытие из лаунчера |
| 3.0 | 2026-09-13 | Добавлены статические RRO, LAUNCHER_APP_HIDDEN_DESIGN.md |
| 2.0 | 2026-09-05 | Финальная версия workflow, 8 целевых приложений переведены |
| 1.0 | 2026-09-04 | Первый рабочий цикл: decompile → extract → translate → build |
