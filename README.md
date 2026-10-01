# Deepal HU Translate

Перевод пользовательского интерфейса ГУ Deepal / Changan S05 с китайского на русский
через RRO-оверлеи (Runtime Resource Overlay).

**Нацелено на прошивку ГУ 3.1.2**: оверлеи собраны и проверены на этой сборке
(целые ресурсы по ID, подписи, поведение PMS подтверждены вживую); на других
версиях прошивки не гарантировано.

## Архитектура проекта

```
deepal-HU-translate/
├── decompiled/              # Декомпилированные APK (apktool 3.0.2)
├── translations/            # JSON с переводами (strings + arrays + plurals)
├── overlays/                # Структура RRO-оверлеев из translations/*.json
├── apks_rro_min/            # Готовые подписанные APK-оверлеи
├── keys/                    # platform.jks для подписи
├── targets.txt              # Список целевых приложений (группы 1-4, TOP8, EXCLUDE9)
├── manage.bat               # Менеджер ДИНАМИЧЕСКИХ RRO (читает targets.txt)
├── manage_static.bat        # Менеджер СТАТИЧЕСКИХ RRO (читает targets.txt)
└── *.py                     # Основные скрипты
```

## Скрипты

| Скрипт | Назначение |
|--------|-----------|
| `extract_cjk.py` | Извлечение всех строк (strings, arrays, plurals) из values/ и values-zh/ |
| `scripts/decompile_apks.py` | Декомпиляция APK из original_apks/ в decompiled/ |
| `generate_overlays.py` | Формирование структуры `overlays/<app>/` (динамические — для `adb install`) |
| `create_rro_min.py` | Сборка APK динамических оверлеев из `overlays/` (aapt2 compile + link, подписание) |
| `generate_overlays_static.py` | Формирование структуры `overlays_static/<app>/` (статические — `android:isStatic="true"`, для `/vendor/overlay/`) |
| `create_rro_static.py` | Сборка APK статических оверлеев из `overlays_static/` (аналог `create_rro_min.py`, но статические) |
| `generate_summary.py` | Итоговая таблица: сколько строк, сколько переведено |
| `scripts/quality_check.py` | Аудит качества (CJK в ru, пустые поля) |
| `create_db.py` | SQLite-словарь переводов из `translations/*.json`; `--stats`, `--find`, `--find-ru` |
| `apply_db_translations.py` | Заполняет ПУСТЫЕ `ru` из SQLite-словаря; безопасный матч (app,name)→(app,zh)→уникальный cross-app + guard (17.09), `--dry-run` |
| `validate.py` | **Автопроверка перед сборкой**: форматтеры zh↔ru (порядок + %s/%%), CJK-in-ru, размеры/индексы массивов, plurals few/many, WARN-аудит ширины RU vs ZH коротких UI-строк (секция 1c, `--length-report FILE`), разбор APK через `aapt2 dump` (package-префикс по схеме, ресурсы). Exit 1 при ошибках |
| `strwidth.py` | Метрика «ширины строки» (CJK=2, остальное=1) + пороги `--fit` (`FIT_*` env). Общая для аудита `validate.py` и `improve_translations.py --fit` |
| `improve_translations.py` | **Улучшение/перевод строк в `ru`** (вход `translations/*.json`, выход — поле `ru` той же записи). `--all` / `--defective` / `--empty` / `--long` / `--fit` / `--app` / `--limit` / `--resume` / `--fresh` / `--dry-run`. Guard: паритет форматтеров + wake-word + CJK + дегенеративные ответы + длина для длинных; у `--fit` — **два лимита** (мягкий «только короче по ширине» + жёсткий cap в символах, повтор с `max_chars` в пайлоаде), не укоротилось — `ru` не меняется, список в `logs/fit_stuck.txt`; прогресс `improve_fit_progress.json`. Retry: hint на форматтеры; **точечный CJK-ремонт** (модель переводит только оставшиеся фрагменты с русским контекстом, полный повтор только как фолбэк). Длинные (`len(zh)>=API_LONG_THRESHOLD`, 200) — **по одной** |
| `promt.md` | Системный промпт для improve (HTML/экранирование, плейсхолдеры, глоссарий) |
| `test_improve_selftest.py` | Self-test: guard, specs, wake-word, и aapt2 round-trip (compile+link+dump) |
| `fix_placeholders.py` | Одноразовая правка 12 строк с битыми плейсхолдерами/CJK (17.09.2026), см. CHANGELOG |
| `make_bisect_overs.py [K][A][B][NAME]` | Диагностика: K «кусковых» RRO-APK с частью строк одного приложения (тот же package, ID зачищены) — бисекция проблемного ресурса на ГУ |
| `make_zh_control.py [N]` | Диагностика: RRO-«контроль» — все/первые N ID таргета, но значения КИТАЙСКИЕ (таргетные) — разделение «структура таблицы vs контент перевода» (см. CHANGELOG [2026-09-25]) |
| `create_app_pipeline.sh` | Полный цикл: create_db → generate_overlays(±static) → create_rro_* |
| `export-to-db.sh` | create_db + validate (+ dry-run apply_db если нужно заполнить дыры словарём) |

## Два способа сборки RRO APK

### 1. Динамические оверлеи (`adb install`)

Вкл/выкл через ADB; скрыты из списка приложений лаунчера (префикс `com.android.*`):

```bash
python3 generate_overlays.py
python3 create_rro_min.py
# Установка:
adb install apks_rro_min/<app>_RRO.apk
adb shell cmd overlay enable --user 0 com.android.vendor.translate.rro.<app>
```

Префиксы пакетов (разные у схемы, оба намеренные — bat-скрипты их знают):
- динамические: `com.android.vendor.translate.rro.<app>` — лаунчер скрывает такие
  пакеты (фильтр `startsWith("com.android")`, см. `LAUNCHER_APP_HIDDEN_DESIGN.md`);
- статические: `com.deepal.translate.rro.<app>`.
Переводы — в `res/values-ru/` (для ru-локали) + дубль в `res/values/`
(default-конфигурация: RU применяется и когда locale ГУ НЕ ru — fallback
вместо zh у WT-* / en у AOSP-*).

**Внимание: 9 AOSP-target оверлеев в динамической схеме НЕ ставятся**
(`adb install` → `INSTALL_FAILED_INTERNAL_ERROR`: «signed with different
certificates… overlay lacks <overlay android:targetName>»). Это чистые AOSP-пакеты
(подписаны AOSP-ключом, а RRO — заводским), `overlay enable` здесь даже
не выполняется. Список (исключены из `manage.bat`, `EXCLUDE9`):
NetworkStack, MediaProviderLegacy, UserDictionaryProvider,
DownloadProvider, DownloadProviderUi, CompanionDeviceManager, MtpService,
CaptivePortalLogin, ContactsProvider. Итого динамически устанавливается
**91 из 100**. Если нужен перевод их строки — путь только статический
(ниже, `/vendor/overlay`, root+remount).

Перед/после сборки:

```bash
python3 validate.py          # семантическая проверка (exit 1 при ошибках)
python3 validate.py --skip-apk   # только translate/overlays (быстро)
```

### 2. Статические оверлеи (`/vendor/overlay/`)

Автоматически загружаются при загрузке, скрыты от пользователя, без `<application>`.
Используют `android:isStatic="true"` — путь к директориям другой:

```bash
python3 generate_overlays_static.py     # структура в overlays_static/<app>/
python3 create_rro_static.py            # сборка в apks_rro_static/
# Установка в /vendor/overlay/<package>/ (или /product/overlay/) — требуются root-права:
adb root && adb remount
adb shell mkdir -p /vendor/overlay/<app>
adb push apks_rro_static/<app>_RRO.apk /vendor/overlay/<app>/
adb reboot
```

## Быстрый старт

```bash
# 1. Декомпилировать APK (если новые)
python3 scripts/decompile_apks.py

# 2. Извлечь строки из декомпилированных APK
python3 extract_cjk.py

# 3. Улучшить/перевести строки (LLM, запись в ru) — см. секцию ниже
API_URL=... API_KEY=ollama API_MODEL=qwen3.8:27b \
  python3 improve_translations.py --app <app_name>   # или --defective / --all

# 4. Автопроверка (обязательно перед сборкой)
python3 validate.py

# 5. Сформировать overlays и собрать RRO APK
python3 generate_overlays.py && python3 create_rro_min.py
python3 generate_overlays_static.py && python3 create_rro_static.py

# 6. Посмотреть статус
python3 generate_summary.py
```

Или одним прогонщиком: `./create_app_pipeline.sh` (create_db → generate → сборка обоех наборов).

### Улучшить переводы (LLM-прогон по translations/*.json, запись в `ru`)

```bash
# проверить, что бы улучшалось (без отправки):
API_URL=http://10.0.0.128:11434/v1/chat/completions API_KEY=ollama \
API_MODEL=qwen3.8:27b python3 improve_translations.py --dry-run

# точечно: только дефектные строки (пустые ru / CJK / битые плейсхолдеры /
# дегенеративные None на длинных / слишком короткие ответы на длинных):
API_URL=... API_KEY=ollama API_MODEL=qwen3.8:27b python3 improve_translations.py --defective

# только ДЛИННЫЕ (len(zh)>=200) и дефектные — каждая по одной; обходит
# progress-фильтр (повторно забирает упавшее). То, чем достают одну строчку:
API_URL=... API_KEY=ollama API_MODEL=qwen3.8:27b python3 improve_translations.py --long

# УКРОТИТЬ «раздутые» короткие UI-строки (RU шире ZH → перенос на 2 строки;
# RRO не трогает layout виджетов): мягкий лимит (принимается только КОРОЧЕ
# текущего по ширине и <= cap), если не сработал — жёсткий (повтор с
# max_chars в пайлоаде, cap = min(28, 2·zh_w+8), см. strwidth.py).
# Не укороченные остаются как были и попадают в logs/fit_stuck.txt.
# Прогресс отдельный (improve_fit_progress.json), повторный прогон без
# --fresh не укорачивает уже укороченное.
API_URL=... API_KEY=ollama API_MODEL=qwen3.8:27b python3 improve_translations.py --fit
# сколько осталось «перешироких» — аудит validate:
python3 validate.py --skip-apk --length-report logs/fit_report.txt

# полностью (фон, с resume):
API_DEBUG=1 nohup env API_URL=... API_KEY=ollama API_MODEL=qwen3.8:27b \
python3 improve_translations.py --all --resume &

# после: автопроверка + пересборка:
python3 validate.py && python3 generate_overlays.py && python3 create_rro_min.py
```

> **Длинные строки:** `API_LONG_THRESHOLD` (по умолч. 200) отделяет их — в
> `--all`/`--defective` такие идут **по одной** (одна длинная в пакете из 8
> ломала весь батч — см. CHANGELOG [2026-09-18b]). Переменные окружения:
> `API_LONG_THRESHOLD`, `API_BATCH_SIZE`, `API_MAX_TOKENS`, `API_TIMEOUT`,
> `API_DEBUG`, `API_DRY_RUN`.

## Зависимости

- **Java** 21+, **Python** 3.12+
- `aapt2`, `apksigner` — Android SDK Build Tools
- `platform.jks` — подпись оверлеев
- **Ollama** (host `10.0.0.128:11434`): `qwen3.8:27b` — улучшение/перевод через `improve_translations.py`

## Статус (18.09.2026, 12:43) — ЗАВЕРШЕНО, 100%

> Актуальную статистику всегда можно получить через `python3 generate_summary.py`
> и `python3 validate.py` — это source of truth, таблица ниже снимок.

| | Число |
|---|---|
| Декомпилированных APK | **122** |
| JSON-файлов приложений | **100** (+ `improve_progress.json`) |
| Всего записей (zh) | **≈17 827** |
| Переведено CJK→ru | **17 827 / 17 827 (100.0%)** |
| Переведено приложений | **100 / 100 (100.0%)** |
| Собрано RRO APK | **100 dynamic + 100 static** (≈2.9 МБ, 0 FAIL) |
| Валидация (`validate.py`, с `aapt2 dump`) | **0 ошибок** |
| Self-test improve | **42 / 42** |
| Устанавливается динамически (`manage.bat` install) | **91 из 100** (9 AOSP-target не ставятся `adb install` — см. «Внимание» выше; `manage.bat` их исключил из GROUP1/`auto`) |
| Остальные 22 из 122 декомпилированных APK | CJK-строк в `res/` нет (системные сервисы/шеймы/фреймворк) — переводить нечего, RRO не назначены |

LLM-улучшение (`improve_translations.py --all`) и добор длинных строк
(`--long`) завершены: **все 100 приложений переведены**. Три длинных
юридических документа (9 273–10 774 симв.) переведены, последняя строка
добита точечным CJK-ремонтом — см. CHANGELOG [2026-09-18b], [2026-09-18c].

Длинные строки (`len(zh) >= API_LONG_THRESHOLD`, по умолч. 200) в
`improve_translations.py` отправляются **по одной** (batch из 8 с длинной
рывал весь пакет). Если в длинном переводе остаются китайские фрагменты,
`improve_translations.py` чинит их **точечно** (CJK-ремонт: модель
переводит только оставшиеся слова с русским контекстом, без регенерации
всего 30к+ текста).

### Нормализация перевода (aapt2-safe)

**Основной путь (сейчас): `improve_translations.py`** пишет модельный ответ
напрямую в `ru` после `_sanitize_ru()`:

| Было (модель) | Стало в `ru` | Зачем |
|---|---|---|
| `&amp;lt;`, `&amp;quot;`, `&amp;gt;`, `&amp;apos;` | `&lt;`, `&quot;`, `&gt;`, `&apos;` | двойное экранирование → одинарное, иначе `generate_overlays._esc()` даст `&amp;lt;` и aapt2 FAIL |
| `'`, `&apos;`, `&#x27;` | ‘ (U+2019) | aapt2 отклоняет одиночный `'` и даже `&apos;` (проверено compile-матрицей) |
| живые `\n`, `\r`, `\t` | литеральные `\\n`, `\\t` | конвенция ресурсов: `\n` в XML — два символа |
| `%s`, `%1$d`, `%.1f`, `%%`, URL, «» | не трогается | паритет форматтеров гонится guard'ом |

Поэтому `WT_MultiMediaCenter/dialog_privacy_policy_msg_start_up`
(`&lt;a href='...'&gt;` + ‘ вместо «) компилируется: compile + link OK (72 KB).
Контроль: `validate.py` (17.09.2026: **0 ошибок**); намеренные исключения —
wake-words, см. `CJK_WHITELIST` в `validate.py`/`improve_translations.py`.

### Строки, НЕ попадающие в оверлей (`translations/exclude.json`)

Некоторые строки приложений используются кодом **не как надписи, а как
данные/ключи** (сравнение `getString(id)` с серверными значениями и т.п.).
Их RU-перевод ломает логику (симптом: «Умное обслуживание» показывало
`Осталось - - км` вместо цифр — RU-ключ не совпадал с серверным
`maintenanceProgram`). Такие строки выносятся в
`translations/exclude.json` (`{app: {name: причина}}`) — генераторы
(`generate_overlays{,_static}.py` → `load_excludes()`/`filter_excluded()`)
не пишут их в оверлей, на ГУ остаётся исходное (zh) значение. RU-перевод
остаётся в `translations/<app>.json` (`ru`) — как справка и для будущего
репака, не удалять. Текущий состав (25.09):
- `WT_AutoMaintenance`: 8 строк `dialog_part_*_title_text` (ключи,
  `MainActivity.R()`/`MaintainUtil`);
- `WT_HDCloudCamera`: `setting_guardian_mode`, `cruise_protect_model`
  (ключи голосовых команд — сравнение с `json.optString("Mode")`).

**Решение (25.09, утверждено): названия разделов «Умного обслуживания»
(空调滤芯/制动液/减速器油 — салонный фильтр/тормозная жидкость/
редукторное масло) остаются китайскими.** Это **данные с сервера**
(`maintenanceProgram`), выводятся `setText(program)` без ресурсов — RRO
принципе не покрывает, RU-вариант возможен только smali-репаком
(отклонено; имена деталей = данные из каталога производителя).
Цифры («Осталось N км / дн.») — по-русски. Живая проверка пройдена.

## Workflow

```
translate/improve → validate → generate_overlays(±static) → create_rro_* → validate
```

```bash
# 1. Перевести / улучшить (один из вариантов):
API_MODEL=qwen3.8:27b python3 improve_translations.py --app <app_name>   # улучшение в ru
python3 apply_db_translations.py --app <app_name> --dry-run  # пополнение пустых из SQLite-словаря

# 2. Автопроверка (обязательно перед сборкой):
python3 validate.py

# 3. Сформировать overlays и собрать APK:
python3 generate_overlays.py        # динамические (values-ru)
python3 create_rro_min.py
python3 generate_overlays_static.py # статические (values-ru)
python3 create_rro_static.py

# 4. Финальная проверка (включая разбор APK):
python3 validate.py
```

### Словарь (SQLite)

```bash
python3 create_db.py                 # пересобрать словарь из translations/*.json
python3 create_db.py --stats         # статистика
python3 create_db.py --find 蓝牙      # поиск по zh
python3 apply_db_translations.py --all --dry-run   # безопасно: только пустые ru,
                                # матч (app,name)→(app,zh)→уникальный cross-app, guard
```
Не запускайте `apply_db_translations.py --all` **без** `--dry-run` до того, как
`improve_translations --all` дойдёт до конца.

### Ключевые приложения (из overlays.txt)

| Приложение | Целевое назначение |
|---|---|
| AdayoAPA | Парковка (автопарковка) |
| AdayoDvr | Видеорегистратор |
| Camera | Камера |
| WT_AirConditioner | Климат-контроль |
| WT_BTPhone | Bluetooth + телефон |
| WT_Launcher | Главная панель |
| WT_MultiMediaCenter | Медиацентр |
| WT_VehicleCenter | Центр управления автомобилем |

Полный список всех 100 приложений (CJK-строки + назначение) — `docs/APPS.md`.

## Список целей (`targets.txt`)

Группы-списки приложений (Группы 1-4, TOP-8, EXCLUDE9) хранятся в **одном
файле `targets.txt`** — в коде скриптов (`manage.bat`, `manage_static.bat`,
Go-менеджер `deepl`) списков НЕТ. Для другой прошивки, где названия/набор
APK отличаются, достаточно отредактировать `targets.txt` — менять код
скриптов не нужно.

```bash
# Формат: имя секции на отдельной строке (GROUP1..GROUP4, TOP8, EXCLUDE9),
# далее по одному имени в строке (или несколько через пробел).
# # в начале строки и пустые строки игнорируются.
```

- **динамическая схема** (`manage.bat`): ставит `GROUP1..GROUP4`, `TOP8`,
  `auto`; `EXCLUDE9` (9 AOSP-target) исключается — их `adb install` отклоняет.
- **статическая схема** (`manage_static.bat`): `GROUP1` автоматически
  добирает `EXCLUDE9` (в `/vendor/overlay` они ставятся), `EXCLUDE9`
  отдельным списком не используется.
- **Go-менеджер** (`deepl`): читает `targets.txt` из текущей директории
  (или `DEEPL_TARGETS=/путь`), при отсутствии использует встроенную копию,
  которую `build.sh` синхронизирует из корневого `targets.txt`.

## Установка на ГУ

```bash
# Скопировать APK на Windows машину
scp apks_rro_min/AdayoAPA_RRO.apk user@deepal-hu:/path/to/transfer/

# Динамические RRO - один менеджер (Windows, adb):
manage.bat                  # интерактивное меню
manage.bat install 8        # (install + enable, top-8)
manage.bat install A        # (все группы 1+2+3+4)
manage.bat disable 8
manage.bat diag
manage.bat report           # собрать zip-логи для анализа (см. ниже)
manage.bat reboot           # перезагрузить ГУ (adb reboot)
# modes: install | enable | disable | uninstall | status | diag | report | reboot
# presets: 1 | 2 | 3 | 4 | 8 (top-8) | A (все) | C (2+3) | auto (все *_RRO.apk) | 0 (меню)
#   A и auto дают 91 пакет: 9 AOSP-target (EXCLUDE9) исключены — не
#   ставятся adb install (см. ограничение устройства ниже).
#   ВАЖНО: после install или uninstall перезагрузите ГУ (manage.bat reboot) —
#   статические оверлеи применяются только при старте, динамические reboot
#   гарантированно чистит кэш/состояние PMS. В меню это пункт [8] (dynamic)
#   и [6] (static).

# Статические (root, /vendor/overlay) - второй менеджер:
manage_static.bat           # меню (или manage_static.bat install A / report / reboot)
```

> **Ограничение устройства (проверено по живым логам 22.09, см.
> CHANGELOG [2026-09-22b]): 9 динамических оверлеев, целящих AOSP-пакеты
> (NetworkStack, MediaProviderLegacy, UserDictionaryProvider,
> DownloadProvider, DownloadProviderUi, CompanionDeviceManager,
> MtpService, CaptivePortalLogin, ContactsProvider), на этой сборке НЕ
> ставятся через `adb install`** (`INSTALL_FAILED_INTERNAL_ERROR`:
> «signed with different certificates… overlay lacks targetName»;
> uninstall — `DELETE_FAILED_INTERNAL_ERROR`, в `overlay list` их нет —
> не ставились ни разу). **Решение (25.09): исключены из `manage.bat`**
> (список `EXCLUDE9`; не входят в GROUP1 ни в `A`, ни в `auto`) —
> установка идёт по 91 пакету. Перевод их строки — только статикой:
> `manage_static.bat` → `/vendor/overlay` (root+remount). Чинка 22.09-22b
> гарантирует, что успешные установки НЕ окрашиваются ложным `[ERROR]`, а сводка
> «OK/Ошибок» совпадает с реальностью (строки `RESULT:` в ops-логе —
> достоверный источник).
>
> **Чтение логов:** `logs\ops_<mode>_YYYYMMDDHHMMSS.log` (имя — чистый
> таймстемп; строки подстановок вида `!X:=_!` в имени файла НЕ
> использовать — на Windows дают битое имя с `:` и файл не создаётся,
> вывод молча теряется). Внутри: `db:` (строки adb) +
> `RESULT: OK (…)/ERROR (…)/WARN (…)` на каждый пакет и финал
> `конец: всего=N ok=… err=… нет_apk=…`. `Failure […]` в строке `db:` =
> реальный отказ adb; `RESULT: WARN (установлен, оверлей не включён)` =
> пакет на месте, оверлей довести: `manage.bat enable`
> (установка сама делает `overlay enable` с 1 ретраем и пишет вывод).
> Если в отчёте «ops_*.log: 0 файла» — имя лога было битым (см. 21:26);
> после правок 23.09 имя формируется wmic/powershell-таймстемпом.

### Сбор логов для анализа (report)

`manage.bat report` / `manage_static.bat report` (или пункт [7]/[5] в меню)
собирают `collect_report.bat` **только-чтение** пакет в
`logs\deepal_<scheme>_report_<TS>.zip` — ЭТО файл и отправлять:

| Файл в zip | Что отвечает |
|---|---|
| `summary.txt` | читать первым: установлено/отсутствует, оверлеи [x]/[ ]/---, FATAL, ANR, авто-вывод |
| `ops_*.log` | **логи установки**: последние 10 `logs\ops_<mode>_<TS>.log` (install/enable/disable/uninstall) |
| `raw/20_packages_ours.txt` | `pm list` наших пакетов + versionCode |
| `raw/25_overlay_all.txt` | полный `cmd overlay list --user 0` |
| `raw/30_per_package.txt` | построчно 100 пакетов: ver + state оверлея [x]/[ ]/---/нет |
| `raw/10_locale.txt` | locale (перевод применяется к ru?) |
| `raw/26_vendor_overlay.txt` | (static) `ls -l` наших папок в `/vendor/overlay` |
| `raw/02_apk_local.txt` | APK на хосте, что планировалось ставить |
| `raw/00_device.txt` | getprop + uptime + df |
| `raw/40_crash.txt`, `41_fatal.txt` | сбои: crash buffer / FATAL EXCEPTION |
| `raw/42_anr.txt` | /data/anr + trace (root) — авто `adb root` при пустом |
| `raw/43_main.txt` | **работа apk**: `logcat -b main -t 2000` |
| `raw/44_overlay_main.txt` | из main: строки overlay/idmap/наши пакеты |
| `raw/45_overlay_dump.txt` | `cmd overlay dump` оверлеев в ошибке `---` |

Логи установки сами пишутся в `logs\ops_<mode>_<TS>.log` при каждом
install/enable/disable/uninstall (см. выше) и автоматически попадают в zip
(последние 10). Скрипт сам попробует `adb root` (для static-каталога и
/data/anr), если нечто не видно без root.

> **Проверка bat-менеджеров.** Syntax/структура/меню/скелет отчёта
> проверены под wine (2026-09-20, см. CHANGELOG [2026-09-20] — там же пойманы
> и починены 13 live-багов, т.ч. неэкранированные скобки в `echo` внутри
> `if (...)`-блоков). Живые проверки на Windows+adb+HU: 22.09
> (CHANGELOG [2026-09-22b]) — форматы `pm list`/`cmd overlay list` сверены
> (парсер: 0/100→90/10), false-`[ERROR]`/двойной счёт,
> `WT_PhoneLink`→`PhoneLink`; 21:26-прогон (CHANGELOG [2026-09-23]) —
> всё встало (91/9, phonelink `[x]`, ver виден), найдены и починены:
> потеря ops-логов (битое имя файла из `:OpSTs`), счётчик FATAL
> (`find /C` → дефисы), ANR-trace (Permission denied → `adb root` до
> дампа), невидимый `overlay enable` (теперь retry+[WARN]), хрупкий
> матч «Success».

| Скрипт | Назначение |
|---|---|
| `manage.bat` | **менеджер динамических RRO**: install/enable/disable/uninstall/status/diag/report, группы 1-4/top-8/все/auto, CLI (`manage.bat <mode> <preset>`) или меню |
| `manage_static.bat` | **менеджер статических RRO**: install/uninstall/status/diag/report (push в `/vendor/overlay/`, root), те же группы |
| `collect_report.bat` | вызывается из обоих `report`: сборка zip-лога для анализа (`logs\deepal_<scheme>_report_<TS>.zip`) |

Префикс пакета динамических: `com.android.vendor.translate.rro.<app>` — лаунчер
скрывает такие пакеты (фильтр №1, `LAUNCHER_APP_HIDDEN_DESIGN.md`).

## Полезные команды

```bash
# Полный статус (все приложения, % перевода, RRO)
python3 generate_summary.py

# Семантическая автопроверка (обязательна перед сборкой и после)
python3 validate.py

# Самопроверка improve-цепочки (guard + aapt2 round-trip)
python3 test_improve_selftest.py

# Аудит качества (исторический)
python3 scripts/quality_check.py

# Прогресс LLM-улучшения
python3 -c "import json;print(len(json.load(open('translations/improve_progress.json'))['done']))"

# Размер переводов / список RRO APK
du -sh translations/
ls -lh apks_rro_min/*.apk apks_rro_static/*.apk
```
