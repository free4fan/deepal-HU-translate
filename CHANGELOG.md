# Changelog

## [2026-09-30] — WT_AirConditioner: после закрытия машины перевод «слетал на китайский» (одном меню настроек климата) — РЕШЕНО: mirror RU в `values-zh-rCN/`

**Симптом (пользователь).** RRO стоит, после ребута перевод есть; закрываешь машину, уходишь, приходишь — в меню настроек климата строки снова китайские (пример: `demisting` 自动除雾 вместо «Автодефрост»).

**Корень (smali-доказано, AUDIT по всем 124 decompiled).** WT_AirConditioner — ЕДИНСТВЕННЫЙ таргет, форсирующий `Locale.CHINA` в СВОИХ Resources (проверено grep `Locale;->CHINA` + `setLocale`/`createConfigurationContext` по всем `decompiled/*/smali`; 23 других приложения с `CHINA` используют её в SimpleDateFormat/утилитах, на выбор ресурсов не влияют). Три места:
1. `app/AirApplication.onConfigurationChanged()` — любое config-change → `Resources.updateConfiguration(conf,CHINA)`: app-Resources приложения форс zh (persistent-app получает config-change после закрытия/возврата машины);
2. `ui/activity/AirSettingsActivity.attachBaseContext()` — `createConfigurationContext(conf с CHINA)`: ВЕСЬ контекст экрана настроек = zh; `AirSettingsSwitch` (`item_settings_*_switch_layout.xml`: `app:air_settings_name="@string/demisting"`) резолвит строки из этого zh-контекста — поэтому слетает именно это одно меню;
3. `view/SetWindowView.initViews()` — тот же форс для окна-настроек.

RRO-дубль RU в `values/` (default) и `values-ru/` такому форсу не противостоял: запрос локали `zh` идёт в точный слой `values-zh-rCN/`, где у оверлея НЕ было ничего → fallback в source `values/` (кит.) / свой default-запрос.

**Фикс (РРО-сторонний, без правки кода).** `generate_overlays.py` + `generate_overlays_static.py`: новая константа `ZH_MIRROR_APPS = ("WT_AirConditioner",)` — для приложений из списка RU-файлы (`strings.xml`/`plurals.xml`/`arrays.xml`) дописываются ТРЕТЬЕЙ копией в `res/values-zh-rCN/` (значения идентичные, resId — те же canonical-ID, зафиксированные target public.xml). Форс zh → точное zh-rCN-совпадение → берётся RU из оверлея. Билдеры не менялись (компилируют все `values*/`; flat-имена `values-zh-rCN_*.flat` не пересекаются). Пересобрано: `apks_rro_{min,static}/WT_AirConditioner_RRO.apk` (~44 KB, +7 KB vs 37) — `aapt2 dump`: `string/demisting (zh-rCN) "Автодефрост"`, 212/212 resId = target, `validate.py --app WT_AirConditioner` = 0 ОШИБОК.

**На машине:** переустановить/перезаписать `apks_rro_min/WT_AirConditioner_RRO.apk` (динамическая схема: `adb install -r`, при необходимости `uninstall` старого + `install` от root — см. якорь 27.09 про idmap) ИЛИ `apks_rro_static/` в `/vendor/overlay/WT_AirConditioner/` + `adb reboot`. Проверять: настройки климата после закрытия/возврата машины, строка «Автодефрост».

**Новый таргет с проблемой** → сначала skan (команда в MEMORY-якоре), при подтверждении форса ресурсов — добавить в `ZH_MIRROR_APPS` ОБОИХ генераторов → перегенерировать → пересобрать `--only <App>`.

## [2026-09-29] — `uninstall` рвался на `. was unexpected at this time.`: `call :DoOpAdb root` ВНУТРИ if-блока (+ WARN-текст с `^). ` в блоке)

**Симптом (2 прогона 29.09 09:03, репорт `report_2026-09-29_090418`).** Меню → [4] Удалить → [A]:
- `09:03:24`: ops-лог оборван ПОСЛЕ `db: restarting adbd as root` (это последняя строка `call :DoOpAdb root` внутри `if not "!UID_NOW!"=="0" ( … )`), ни одного `---- [UNINSTALL] …`;
- `09:03:43` (повтор): в ops-логе только заголовок, дальше ничего;
- консоль: после баннера `uninstall` — `. was unexpected at this time.`
Коллекция логов при этом **собралась полностью** (45/46/summary, zip) — фикс `[8/9]` от 28.09 подтверждён живьём.

**Корень (то же семейство, что 24c «безвредно).»).** В скобочном блоке `if (…) ( call :DoOpAdb root )` подпрограмма `:DoOpAdb` содержит `for /f (…) do ( … )` + редиректы + `exit /b !ADBRC!` — возврат из такого `call` изнутри чужого блока ломает state парсера; wine с той же формой прощает, живой cmd — нет (live: 09:04 09-24/09-27 — та же форма шла от root, 29.09 09:03 — обе попытки рухнули; текст/ревизия в этом же if-регионе 28.09 менялась). Дополнительно вчерашний WARN-текст в том же if/else-блоке верификации несёл `…device busy^). enable…` — `^)`+`.` внутри блока: класс ошибки 24c.

**Починено (`manage.bat`, ОБЕ копии: repo + `deepal-HU-translate-3.2.0` = deployed `dynamic_hide`):** root-переключение разобрано на ПЛОСКИЕ строки — нет ни `call`, ни compound-блока:
```
if "!UID_NOW!"=="0" goto RUN_ROOT_OK
echo [ROOT] adb shell uid=!UID_NOW! - переключаю на root ^(adb root, рестарт adbd^)...
if defined OPS_LOG echo   [ROOT] uid=!UID_NOW! - adb root... >> "!OPS_LOG!"
adb root >"!IDF!" 2>&1
set "AOUT="
for /f "usebackq delims=" %%I in (`type "!IDF!"`) do set "AOUT=%%I"
del "!IDF!" >nul 2>&1
echo   db: !AOUT!
if defined OPS_LOG echo   db: !AOUT! >> "!OPS_LOG!"
:RUN_ROOT_OK
```
WARN-строка в безопасный текст без `^). `. Поведение 1:1: root переключается
перед каждым прогоном (без флага — фикс 28.09b сохранён), строка с root и
`restarting adbd as root` уходит в `db:`-полезную часть. Блок верификации
`[OK]`/`[WARN]` сохранён (дал живой маркер 09:04).

**Wine-проверка плоского блока (mock adb, state-файл):** uid=2000 → `[ROOT]…` +
`restarting adbd as root` + блок доходит до конца (раньше рвался ровно здесь);
uid=0 → no-op; `unexpected` нет. (Полный end-to-end в wine — ограничение
wine-`call`/`timeout`, финал — live-прогон.)

**Полный аудит 29.09 (5 bat, обе ревизии):** нечётных кавычек вне REM — 0;
`call` `for`-подпрограммы ВНУТРИ скобочного блока — 0; паттерн `^). ` в блоках — 0;
баланс скобок 0; CRLF, без BOM.

**На машине:** скопировать ОБА `manage.bat` (repo-ревизия == 3.2.0 == то, что в `dynamic_hide`) + свежий `collect_report.bat`, повторить uninstall. Маркер: `[ROOT] uid=0 (было 2000)` в ops-логе до первой `---- [UNINSTALL]`.

## [2026-09-28b] — «перестало от root устанавливаться»: флаг `ADB_ROOT_DONE` пережил РЕБУТ ГУ → root-блок пропущен, прогон от shell (uid 2000)

**Симптом.** Прогон `manage.bat install A` 28.09 18:14 (репорт `report_2026-09-28_181841`): 91/91 `Success`, НО `WT_WtSystemUI` — `RESULT: WARN (установлен, оверлей не включён; SecurityException UID2000)`; в `raw/25` — `[ ] wt_wtsystemui`. В ops-логе 18:14 **нет строки `[ROOT]`** — корневой блок не выполнялся, весь прогон от шелла (uid 2000). Прогон 09:04 (до) прошёл от root: `[ROOT] uid=2000 - adb root...` → `restarting adbd as root` → `[ROOT] uid=0 (было 2000)`, WtSystemUI `[x]`.

**Корень.** `manage.bat` имел `if defined ADB_ROOT_DONE goto ENSROOT_DONE` — флаг «один раз на bat-процесс (окно)». Между 09:04 и 18:14 ГУ **РЕБУТНУЛО** (репорт 18:18: `up 5 min`): `adb root` НЕ переживает ребут (adbd стартует как shell), а флаг в живом окне — пережил → корневой блок пропущен, enable `com.android.systemui` (цель с `<overlayable>`) от uid 2000 запрещён `OverlayActorEnforcer` (AOSP 11) → SecurityException. Флаг-оптимизация противоречит «root на весь прогон» при наличии ребутов.

**Починено** (`manage.bat`, обе копии: рабочий + `deepal-HU-translate-3.2.0`): удалён шорт-кат `if defined ADB_ROOT_DONE` и `:ENSROOT_DONE` — перед КАЖДЫМ изменяющим прогоном чинимый блок делает probe-uid:
- уже root (`uid=0`) → no-op (сам `adb root` вызывается ТОЛЬКО при `uid<>0`);
- после ребута (`uid=2000` в том же окне) → снова `adb root` + `wait-for-device` + верификация `[ROOT] uid=0 (было 2000)`.
Цена: два дешёвых `adb shell id -u` на прогон. Логика/строки `[ROOT]`/WARN без изменения, WARN дополнен причиной «ГУ только что ребутнулось». `manage_static.bat` не тронут (там `adb root` без флагов, на каждый прогон).

**Wine-проверка** (реальный блок + mock adb `id -u`/`root`/`wait-for-device`, `.uid`-файл-состояние): RUN1 preboot `uid=2000` → `[ROOT] uid=2000 - adb root...` + `adb root` реально вызван (`.uid→0`). Ключевое: `if not "!UID_NOW!"=="0"` теперь **каждый** прогон, без шорт-ката флагом — т.е. RUN3 after-reboot (`uid=2000`) идёт через тот же код-путь, что RUN1 (проверен выше); RUN2 already-root (`uid=0`) — тривиальный no-op `if not "0"=="0"`. (Wine-лимит: дальше `call :DoOpAdb` не дотягивает до конца блока — знаменитый, на живом Windows 09:04 весь блок отрабатывал.)

**Порядок действий на машине (сейчас):** ГУ уже reboot (18:13) и WtSystemUI установлен, но не включён (`[ ]`). Достаточно: в новом окне `manage.bat enable A` (или `WT_WtSystemUI` отдельно) — блок сам переключит root с нуля (uid 2000 → 0) и включит оверлей. Если окно старое (до ребута) — теперь всё равно работает: флаг убран.

## [2026-09-28] — `collect_report.bat` рвался на [8/9]: `" > "!F_IDMP!" was unexpected at this time.` → блок `if/else` разобран по плоским `goto`

**Симптом.** Живой прогон `report` (репорт `report_2026-09-28_090738`,
папка `c:\auto\deepal s05\app\deepal-HU-translate\dynamic_hide\`): дошёл до
`[8/9]`, после `44_overlay_main.txt` упал:

```
" > "!F_IDMP!" was unexpected at this time.
```

В `raw/` обрыв: есть `00…44`, нет `45/46/summary.txt`/zip. `30_per_package`
в порядке (91/91, `[x]=91`, `---=0`) → в ветки уходил `else`
(`OERRPKGS` пуст), но уже и тот не отработал.

**Корень.** `[8/9]` (26.09 21:50 введён `raw/46_idmap.txt` — первый живой
прогон новой ревизии) строился как многострочный `if defined OERRPKGS (
… ) else ( … )`, внутри которого:
- REM-строки 291–295 с НЕЧЁТНЫМ числом кавычек (`Slog.w "failed to` /
  `<причина>" (тег IdmapManager),` — по 1 `"` на строку) и вложенными
  скобками — внутри блока cmd сканирует `{...}` целиком, и нечётная кавычка
  на строке «переворачивает» цитатное состояние для парсинга блока;
- активная строка 296 `echo ==== … (полный, -d без -t) ====" > "!F_IDMP!"` —
  3 кавычки (нечётных) + скобки; именно её хвост `" > "!F_IDMP!"` и цитирует
  текст ошибки;
- стр. 307 в `else` — та же строка `> "!F_IDMP!"`.

Почему `[6/9]` (тоже блок с кучей REM в скобках) рвался никогда: там
кавычки в REM всегда НАРЯДОМ (парами), а в новом REM-комментарии к 46 —
нет. Wine cmd 9.0 с тем же текстом всё прощает → репро не встанет, виндовый
cmd рвёт (репорты 22.09/24.09 — те же механизмы: незакрытый REM,
неэкранированная `)` внутри блока).

**Починено** (`collect_report.bat`, обе копии: рабочий и `deepal-HU-translate-3.2.0`):
блок `if/else` разобран на **плоские строки + `goto`** (формат как рабочий
`:OPSLOGS_DONE`/`:CRLow` в том же файле) — каждая строка парсится отдельно,
мультистрочное цитатное состояние исчезает:
```
if not defined OERRPKGS goto :IDMP_SKIP
for %%P in (!OERRPKGS!) do ( … overlay dump … )
echo ==== idmap: logcat IdmapManager/Idmap, полный -d ==== > "!F_IDMP!"
adb logcat -d -s IdmapManager:V Idmap:V -v time >> "!F_IDMP!" 2>&1
echo ==== grep наших пакетов в 43_main ==== >> "!F_IDMP!"
findstr /I /C:"!PREFIX!" /C:"idmap" "!F_MAIN!" >> "!F_IDMP!" 2>nul
goto :IDMP_DONE
:IDMP_SKIP
echo Оверлеев в ошибке ^(---^) нет - dump не требуется > "!F_OLDE!"
echo Оверлеев в ошибке нет > "!F_IDMP!"
:IDMP_DONE
```
- REM-комментарий БЕЗ кавычек/скобок-ловушек (текст перенесён/сокращён);
- строка idmap переписана: 2 кавычки, 0 скобок (было 3 `"` + `(...)`);
- **Семантика 45/46 без изменений**: без `---` → `45`-«нет» + `46`-плейсхолдер
  (репорт 27.09 13:12: 45 есть/46 нет — то же поведение); при `---` → dump
  каждого ошибочного оверлея + полный `logcat -d -s IdmapManager:V Idmap:V`.

**Wine-проверка (cmd 9.0):** обе ветки (`OERRPKGS` пусто / заполнено) —
`MARK_END` достигнут, `45`+`46` создаются по семантике, `unexpected` нет;
топ-уровневые REM с кавычками на дальнейшее исполнение не влияют.
Ограничение: wine реально не репро (прощает), финальная проверка — живой
прогон; при повторе обрыва смотреть `raw/44` vs `raw/45` (какой файл
последний — там точка рва).

**Дальше.** На машине `dynamic_hide\` лежит своя копия `collect_report.bat` —
синхронизировать с рабочей, повторить `manage.bat report`.

## [2026-09-27] — Отчёт 13:12 (4 прогона по группам 2→3→1→4 + полный uninstall A до): 91/91 `[x]`, root сам, wt_link исцелился

**Всё зелёное:** 13+28+20+30 = 91 `ok`, `err=0`, `[x]=91`, `---=0`
(упал в отчёт `deepal_dynamic_report_2026-09-27_131258.zip`).
- **Autо-root ПРОВЕРЯЕТСЯ ВЖИВУЮ (без ручного `adb root`):** первый прогон
  group 2 видит `uid=2000`, сам запускает `adb root` (`db: restarting adbd as
  root` в ops-лог), дальше `uid=0` на всё окно; последующие проганы идут от
  уже-root adbd (флаг `ADB_ROOT_DONE` в том же bat-процессе — если через
  меню; отдельными окнами каждый процесс сам видит uid=0). На машине была
  старая ревизия manage.bat (до починки скобок) — в логе виден старый
  артефакт: одновременно `[ROOT] uid=0 (было 2000` И `... не root (было 2000)`
  + обрезанные скобки (обе ветки if/else). Косметика, root реально сработал;
  актуальный manage.bat (в репо) пишет ровно одну честную строку.
- **WT_WtSystemUI `[x]`** (`OK (установлен и включён)`) — SecurityException
  ушёл; root-фикс закрыт.
- **WT_Link исцелился:** вчера `STATE_NO_IDMAP` сегодня `[x]` после чистого
  `uninstall`+`install` от root. Гипотеза: старый idmap (создан до ребута)
  конфликтующий с user-state пакета после reboot; `adb uninstall` удалил
  idmap, установка создала новую. **Lecture для подобных случаев:**
  `uninstall` + `install` того же пакета от root; если повторится —
  `raw/46_idmap.txt` в отчёте даст прямую причину idmap-отказа.
- Порядок групп (2→3→1→4) не важен: группы изолированы, root-состояние
  живёт в adbd-сессии и в bat-процессе, зависимости между группами нет.
- В отчёте нет `46_idmap.txt` — он генерируется только при `---`
  (новое поведение коллектора 26.09).
- `FATAL ×2` (wecarspeech `NumberFormatException ",11"`) и `ANR 1`
  (09-14, `vehiclecenter`) — не наши; vendor-баги, уже заведены.

## [2026-09-26b] — Отчёт 21:44: root ПРОВЕРЕН, wt_link = STATE_NO_IDMAP (диагностика докручена)

**Root: работает.** `ops_install_20260926214453.log`: `uid=0`,
**WT_WtSystemUI: `RESULT: OK (установлен и включён)`** — SecurityException
больше нет. Авто-/ручной root идентичны по коду (если shell уже root —
блок сам не трогает); ручное `adb root` перед запуском обязательно НЕ
после ребута (adbd сбрасывается в shell) — и на этом ГУ блок это
корректно обрабатывает (сам переключит). **Найден и починен баг в моём же
блоке:** `if (…) echo … ^(было !UID_NOW!)` — закрывающая `)` НЕ
экренирована (открывающая — да) → досрочное закрытие блока, ОБЕ ветки
if/else выполнялись → в ops-лог две противоречивые строки (`uid=0 (было 0`
и `…не root`), строка обрывалась. Экренирована (`^(` `^)`); теперь ровно
одна честная `[ROOT]`-строка.
**wt_link: `STATE_NO_IDMAP` — оверлей установлен И ВКЛЮЧЁН**
(`mIsEnabled: true`), но idmap не собрался (перевод не применяется).
Статика чистая: 242 ID RRO ⊂ таргета (0 missing), тип+имя 173/173
совпадают, подписи RRO=таргет (platform c8a2e9bc), priority 1.
КОНТЕКСТ: утром (отчёт 09:13) wt_link был `[x]` — между прогонami был
РЕБУТ (uptime 6758→6675). Причина idmap-падения снаружи; Slog.w
IdmapManager упал из 43_main (буфер 2000 строк, install ~21:44, буфер с
21:50). **Докручено:** `collect_report.bat` — новый `raw/46_idmap.txt`:
`adb logcat -d -s IdmapManager:V Idmap:V` (ПЛЕНЫЙ, не 2000 строк) — в
следующем отчёте будет прямая причина (теги AOSP 11:
IdmapManager.java:84 `failed to generate idmap for … : <msg>`).

## [2026-09-26] — manage.bat: ВЕСЬ прогон от root (инлайн-блок перед циклом пакетов)

**Симптом по живому отчёту 26.09 09:07** (`deepal_dynamic_report_2026-09-26_091344.zip`):
`manage.bat install A` прошёл 91/91 ok, НО WT_WtSystemUI снова
`SecurityException: UID2000 is not allowed to call setEnabled`, и в ops-логе
**НЕТ строки `[ROOT]`** (нет и `restarting adbd`) → та ревизия (подпрограмма
`:EnsureRoot` с вложенными `call :Whoami`/`call :DoOpAdb` + `exit /b`) root
переключить не успела: вложенные `call` с `exit /b` хрупки (в wine рвут стек
возвращения; на Windows могли тихо вернуться до root).
**Решение (блок в потоке `:RUN` ДО цикла пакетов, manage.bat:305-351):**
- логика **ИНЛАЙН, без подпрограмм** (удалены `:EnsureRoot`/`:Whoami`);
- форма ровно как рабочая в живом виде `call :DoOpAdb` (один уровень):
  `call :DoOpAdb root` → `adb wait-for-device` (`adb root` РЕСТАРТит adbd —
  ждём, пока вернётся) → далее весь цикл install/enable/disable/uninstall;
- **весь прогон от root**, не только enable: `ADB_ROOT_DONE` — один раз на
  bat-процесс (перезапуск adbd живёт в том же окне между прогонами);
- uid-верификация best-effort в ops-лог: `adb shell id -u > file` →
  `for /f 'type file'` (та же форма, что заведомо рабочая в `:DoOpAdb`);
  маркеры `[ROOT] uid=… - adb root…` / `[ROOT] uid=0 (было 2000)` /
  `[ROOT] uid=… после adb root - не root` — live-анализ читает: строки
  `[ROOT] uid=0` нет = bat не переключил root (молча продолжить нельзя —
  WARN + явные подсказки в экран и лог).

## [2026-09-25c] — Отказ от reпак-варианта (патч исходных APK): инструменты + данные вынесены в бэкап

**Решение (пользователь).** Путь «перевод перекомпиляцией/бинарным патчем
исходных APK» (шаг 12 WORKFLOW, 56 APK / 1487 CJK-литералов из binary-XML
layout, 22.09) отклонён: проект переводит только через RRO.

**Перенос** в `/home/user/projects/deepal-HU-translate-bak-repack/`:
`axml.py`, `scan_layout_literals.py`, `translate_layout_literals.py`,
`patch_apk_layouts.py`, `verify_patched_apks.py`, `install_patched.bat`,
`revert_patched.bat`, `layout_literals.json` (был в translations/),
`logs/{layout_literals.json, apk_patch_report.json, apk_patch_verify.json,
patched_map.txt, translate_layout_literals.log}`, `__pycache__/`.
**Поправка тот же день:** `original_apks/` (5.0GB) возвращён в проект —
он нужен и RRO-конвейеру (декомпиляция), и, при необходимости,
репак-варианту. `apks_patched/` (4.8GB) на этой машине не собирался —
переносить нечего; на ГУ patched-APK не устанавливались (live-проверка не
проводилась) — откат с машины не требуется.

**Связи после переноса.** RRO-конвейер не трогал эти файлы (использует
`decompiled/` — он на месте). Документация: WORKFLOW шаг 12 → «ОТКЛОНЁН +
архив», версия 5.18; TODO: блок шага 12 свёрнут в «Закрыто», live-проверка
patched-APK и «укоротить 24 кнопки» (referenced бэкапуемый лог) удалены,
assets/raw — отложено. При необходимости вернуться: вернуть файлы из бэкапа.

## [2026-09-25b] — Виджеты шторки: RU-лейблы 2–3 строки / «приклеены к левому краю» → укорочение 28 строк (WT_VehicleCenter + WT_WtSystemUI), APK пересобраны

**Симптом (пользователь).** При свайпе шторки сверху русские подписи
виджетов не влезают в «иконки», перевод на 2–3 строки и прижат к левому
краю. Пример: `widget_seat_memory_drive` «Позиция водителя» [驾驶位置].

**Корень (расклад по layout).** Виджеты шторки = `205px`-боксы
(FrameLayout/LinearLayout gravity=center, иконка 100px + TextView
`wrap_content` @28px, `WTTextStyleCaption1` = **28px не 28sp**,
density ≈ 4.21) либо 183dp-квадраты 2×2 в `seat_switch_widget.xml` /
`light_switch_widget.xml`. ZH-оригинал влезал в 1 строку (驾驶位置 ≈ 108px);
RU 244px → жадный word-wrap на 2–3 строки. «Приклеено к левому краю» —
артефакт wrap: TextView растёт в ширину до левой границы контейнера,
строки выравниваются по левому краю; gravity=center центрирует только
блок целиком. Layout RRO не менять (доказано 22.09) → единственное
лечение в рамках проекта — укорочение строк до zh-длины.

**ФИКС.** 22 строки `WT_VehicleCenter` + 6 строк `WT_WtSystemUI`
(QS-плитки `qs_widget_*_btn`/`qs_screen_off`/`btn_text_copilot_qs_*`)
в `translations/*.json` (источник; overlays руками не трогать): seat
memory = Водитель/Резерв/Отдых/Ещё; light = Выключено/Авт. фары/
Ближний/Габариты; auto_hold=Удержание, brightness_auto=Автояркость,
discharge=Разрядка, emergency=Авар. режим, hdc=Спуск,
hud_adjust=Настр. AR-HUD, child locks=Замок слева/справа,
low_speed_sound=Сигнал низк. скор., rear_belt=Зад. ремни,
sentinel=Охрана, smart_display=Поворот,
wireless_charge=Зарядка, ambient_lighting=Подсветка,
stop_up/stop_down=Остановить (боковые кнопки 120×40px, gravity=center —
там и zh был в 1 строку); SystemUI: tank_cap=Крышка бака,
screen_off=Выкл. экрана, fast_cool=Охлаждение,
copilot close_screen=Выкл. экрана, copilot bluetooth=Не вкл.,
wireless_charge=Зарядка. Макс. RU-ширина теперь ≈110px @28px при
рамке 183–205px — 1 строка, центрируется контейнером.
Изоляция проверена по классу бага «ключевых» строк: каждая строка
 сидит только в виджетовых layout'ах (`grep -rl '@string/N' res/`),
в smali — только `R$string.smali`; `ambient_lighting` в фрагменте не
используется (там `ambient_lighting_switch_off`).

**Сборка/верификация.** `generate_overlays{,_static}.py` (100 app) →
`create_rro_{min,static}.py --only` WT_VehicleCenter + WT_WtSystemUI
(4 APK, resId verify OK: 2031/246) → `validate.py` **0 ОШИБОК** →
aapt2 dump: `()` и `(ru)` = новые значения
(`Водитель`, `Авт. фары`, `Остановить`, `Подсветка`, `Выкл. экрана`).
APK: `apks_rro_{min,static}/WT_VehicleCenter_RRO.apk` (336KB),
`WT_WtSystemUI_RRO.apk` (60KB).

**LIVE-чек (осталось пользователю):** `manage.bat install` (dynamic)
или static-push → reboot → свайпнуть шторку: лейблы 1 строка, по центру.

**Заметка по аудиту.** `audit_fitboxes.py` по этим файлам недостоверен:
dimen'ы `px*` (px!) пересчитаны в условные dp, стиль `WTTextStyleCaption1`
не резолвится (внешний lib) → false @14.0/рамка 28. Для виджетовых
layout проверять вручную в px (density 4.21; см. якорь 25.09e в
MEMORY.md).

## [2026-09-25] — RRO SystemUI «не включается»: SecurityException UID2000 → enable требует ROOT (ФИКС в manage.bat, LIVE-OK)

**Симптом.** В `logs/bat/deepal_dynamic_report_2026-09-25_094516.zip`
(`ops_install_20260925093854.log`): `WT_WtSystemUI_RRO.apk` — `Success`
(установлен!), но `cmd overlay enable --user 0` →
`java.lang.SecurityException: UID2000 is not allowed to call setEnabled
for com.android.systemui` (OverlayActorEnforcer.java:96 /
OverlayManagerService.java:897,600) → `[ ]`, RESULT WARN. 90 других
оверлеев включаются, только он нет.

**Корень.** AOSP 11 (билд `RQ3A.211001.001`, строки стека 1-в-1):
`OverlayManagerService` → `OverlayActorEnforcer.isAllowedActor`:
разрешены ROOT_UID, SYSTEM_UID и «актор» target'а. Target
`com.android.systemui` ОПРЕДЕЛЯЕТ `<overlayable>` —
`decompiled/WT_WtSystemUI/res/values/overlayable.xml` (`car-ui-lib`,
`rotary-ui`) → акторная ветка: shell uid 2000 не актор и без
`CHANGE_OVERLAY_PACKAGES` (signature|privileged) → отказ. У всех других
наших targets `<overlayable>` нет → legacy-ветка, shell проходит.
«Когда-то ставилось» = система шла по СТАТИКЕ `/vendor/overlay`
(`isStatic`, авто-enable, shell-enabler не нужен); префикс пакета
(`com.android.vendor.*`/`com.deepal.*`) к enable отношения не имеет
(актор — shell, не пакет оверлея).

**Фикс.** `manage.bat`: `call :EnsureRoot` в `:RUN` (перед изменяющим
прогоном) → `:Whoami` (whoami в temp-файл, `for /f type`); если shell —
`adb root` через штатный `:DoOpAdb` (вывод + строка `[ROOT]` в
ops-лог). Дальше enable/disable идут от uid 0 → разрешено всегда.
В `[WARN]`-ветке DO_INSTALL — явный хинт «нужен adb root».
Wine-репро: обе ветки + полный цикл DO_INSTALL — OK.
**LIVE (25.09, пользователь):** enable от root → `[x]`, **перевод в
SystemUI заработал** (часть интерфейса ГУ переведена).

**Доп. (тот же день):** анализ «122 декомпилировано → 100 RRO → 91
установлено»: (а) 22 APK без RRO — в `res/values*/` 0 CJK-строк
(системные сервисы/шеймы/фреймворк: Bluetooth, CACertService, CtsShim*,
Qualcomm-vendor и т.п.) → переводить нечего, исключение корректное;
(б) 9 AOSP-target не ставятся `adb install` из-за РАЗНОЙ ПОДПИСИ
таргета (AOSP platform vs заводской ключ), таргеты `<overlayable>` НЕ
объявляют → лечится только statik-путём. **Решение: исключение 9 из
`manage.bat`** — новый список `EXCLUDE9` (manage.bat:51), вычеркнуты из
`GROUP1` (29→20), `auto`-глоб их пропускает (`for %%E in (%EXCLUDE9%)`).
Проверено (wine): пресеты 1/2/3/4/8/A/C/auto → 20/13/28/30/8/**91**/
41/**91**. README: секция «Внимание: 9 AOSP-target…», строка «91 из 100»
в статус-таблице, update заметки 22b-ограничения + строки пресетов.

## [2026-09-25] — «Умное обслуживание»: «Осталось - - км» = строки-КЛЮЧИ в RRO (FIX: 8 строк исключены)

**Симптом.** Ретрим с 23.09: при установленном RRO WT_AutoMaintenance в
«Умном обслуживании» вместо цифр «Осталось - - км / дней»; после удаления RRO
цифры появлялись. Fix resId (23b) не помог (подтверждено live 24.09:
`Осталось - - км` = наш ru-плейсхолдер, оверлей применяется).

**Изоляция (живой A/B на ГУ, 24.09):**
- `WT_AutoMaintenance_RRO_test_zero.apk` (0 ресурсов, тот же target) →
  **данные ЕСТЬ** (UI китайский), logcat: `mAuthState=ACTIVE` →
  `queryMaintainMsg … code:200` (3 объекта: 减速器油 100000км/1768дн, 制动液
  40000/673, 空调滤芯 15000/673) → `observe mMaintainMSGDTO = 0` (успех);
- `WT_AutoMaintenance_RRO_zh269.apk` (те же 269 ID, значения КИТАЙСКИЕ из
  таргета) → **данные ЕСТЬ**;
- т.е. механизм RRO и таблица на 269 безвредны — виноват **КОНТЕНТ RU-строк**.
- Замечание по тесту: t1–t4 (один package) при последовательном `adb install`
  ЗАМЕНЯЮТ друг друга — реально проверялся только последний installed APK
  (t4) — бисект без uninstalls невалиден.

**Корень.** `MainActivity.smali:R()` (~стр. 1502) + `MaintainUtil.
getMaintainMessage()`: **8 строк `dialog_part_*_title_text` используются как
КЛЮЧИ** — код делает `getString(id)` и сравнивает (**`Intrinsics.areEqual`**)
со **`push.getMaintenanceProgram()`** — значением, которое приходит **с
сервера всегда КИТАЙСКИМ** (logcat: `maintenanceProgram:"减速器油"` и т.п.).
С RU-оверлеем `getString` возвращает «Редукторное масло» и т.п. →
сравнение ложно → карточки НЕ заполняются → остаются layout-заглушки
`maintain_mile_normal_default_text` = «Осталось - - км». Статический
`android:text="@string/dialog_part_*_title_text"` в layout при том же
механизме (zero/удаление) просто рисовал китайский заголовок, пока не пришли
данные. Заголовок карточки **всегда** `setText(program)` (кит.) — т.е. RU-
заголовки в «рабочем» состоянии все равно не отображались. Список (8):
jiansuqiyou, zhidongye, kongtiaolvqing, jiyou, kongqilvqing, lengqueye,
lengqueye_title_text2, huohuasai (все `*_title_text`, ID 0x7f0e0054/5a/5c/5d/
56/58/60/53). `order_edit_time_default_text` (TEXTUTILS.equals) — БЕЗОПАСЕН:
этот же `getString` сам пишет значение в footer и сравнивает с ним
(самосогласован после перевода).

**Fix.** Механизм `translations/exclude.json`: `{app: {name: причина}}` —
строки, НЕ пишущиеся в оверлей (на ГУ остаётся исходное zh-значение).
`generate_overlays.py` + `generate_overlays_static.py`: `load_excludes()`/
`filter_excluded()` (до записи values-ru/values); `exclude.json` добавлен в
ignore-списки всех скриптов, читающих `translations/*.json`
(generate_overlays{,_static}, validate, improve_translations,
apply_db_translations). WT_AutoMaintenance: 269 → **261** строка; per-app
реесборка (min+static), `verify resId OK: 261`. **Признак класса бага** (для
поиска в других приложениях): в smali `const/id` → `Activity/Context.
 getString(I)` → `Intrinsics.areEqual`/`.equals`/`TextUtils.equals` (значение —
данные/ключ, а не надпись).
**Авто-скан по 100 приложениям (25.09):** 7 прил / 28 строк.
SAFE (self-consistent — обе стороны сравнения из ОДНОГО ресурса; не трогать):
AdayoLog data_type_*, Fota order_tip_*, SettingsProvider
def_charging_started_sound (файл-путь, не переводится),
WT_IncallPersonalCenter dialog_tip_rke_title/start_bluetoothkey,
WT_InputMethod ok, WT_Wcenter collect/personal_*.
**Починены (25.09), WT_HDCloudCamera:**
- `setting_guardian_mode` (守护模式), `cruise_protect_model` (巡航守护模式) —
  **ключи**: `SpeechOperateCommand.setGuardMode` сравнивает
  `json.optString("Mode")` (ГОЛОСОВАЯ КОМАНДА, внешний источник, zh) с
  `getString` → RU-перевод = команды «режим защиты/патрулирования» не
  срабатывают → `exclude.json` (выключение, как у WT_AutoMaintenance).
- `str_no_filter` — **self-сравнение** (m7/c:223): элемент
  `s_lapse_mode[h[p1]]` == `str_no_filter` (кит. оба 无滤镜). RU разошлись
  («Без фильтр**а**» / «Без фильтр**ов**») → ложно. **Fix: выравнивание
  значений** — `str_no_filter.ru = s_lapse_mode_0.ru = «Без фильтра»`
  (строка-ключ НЕ отображается, отображается элемент массива) — значение в
  `translations/WT_HDCloudCamera.json`, не руками в overlay.
- `str_no_limit` (不限) — self-сравнение с `time_items[6]`: **УЖЕ
  согласовано** («Без ограничений») — НЕ трогать. ЛАТЕНТНЫЙ РИСК:
  `LapseOperateView$a.c` `if (selected==str_no_limit) setCustomDuration(10000)
  else parseInt(selected)` — держать time_items[6] и str_no_limit РАЗНЫМИ или
  числом, иначе `NumberFormatException` (краш) при кастомной интервал-съёмке.
- Пересобрано: regen overlays(+static) (2 ключа исключено), WT_HDCloudCamera
  APK min+static (495 res, verify OK). `validate.py` обоих: 0 ошибок.
(`make_bisect_overs.py`/`make_zh_control.py` — инструменты изоляции, оставить.)

**Валидация на ГУ (СДЕЛАНО 25.09, подтверждено).** Установлены
`WT_AutoMaintenance_RRO.apk` (261 str, min) + `WT_HDCloudCamera_RRO.apk`
(495 res) → reboot → «Умное обслуживание»: **цифры появились** («Осталось
Н км / дн.» по-русски) — основной симптом СНЯТ.

**РЕШЕНИЕ ПО НАЗВАНИЯМ РАЗДЕЛОВ (25.09, утверждено пользователем):
«оставляем как есть» — 空调滤芯/制动液/减速器油 (салонный фильтр/
тормозная жидкость/редукторное масло) остаются КИТАЙСКИМИ, репак НЕ делать.**
Причина: эти имена — **данные с сервера** (`maintenanceProgram` в ответе
`incall.changan.com.cn`, см. logcat 24.09), выводятся прямым
`setText(push.getMaintenanceProgram())` (MainActivity T/L/M/N/P/U), ресурса
нет — RRO принципе не может. Плюс дилемма одного ресурса: ID
`dialog_part_*_title_text` — и КЛЮЧ (compare с program), и `android:text`
layout'а (placeholder карточки); русские заголовки требуют RU-ключа →
сравнение `RU != zh(программа)` снова ломает цифры. Разделение
«ключ/заголовок» возможно только smali-репаком (8 имён → RU перед setText) —
решено **не делать**: имена деталей = данные из каталога производителя
(и те же в сервисной книжке/голосовых), цифры — главное — RU.
Последствия решения учтены: новые/непредусмотренные program с сервера
всегда по-китайски (и без репака тоже); RU-переводы этих 8 строк
ОСТАЮТСЯ в `translations/WT_AutoMaintenance.json` (поле `ru` — полные
переводы), они просто не пишутся в оверлей (см. `exclude.json`).

## [2026-09-24d] — RU-перевод применяется даже когда locale ГУ НЕ ru (дубль в `values/`)

**Симптом.** Перевод жил только в `res/values-ru/` → русский срабатывал строго
при русской локали. Когда locale ГУ ≠ ru (по репорту 24.09: `persist.sys.locale=en-US`),
WT-* приложения показывали китайские строки из default `res/values/`, а AOSP-* —
английский fallback. Перевод «не применялся».

**Решение.** Генераторы пишут RU-строки в ДВЕ папки (значения идентичны):
- `res/values-ru/` — для ru-локали (как раньше);
- `res/values/` — default-конфигурация: при любой другой локали Android берёт
  RU из default вместо zh-строки (WT-*) / en-fallback (AOSP-*).

**Изменено:**
- `generate_overlays.py`, `generate_overlays_static.py` — собирают XML один раз
  (`files` dict) и кладут и в `values-ru/`, и в `values/`.
- `create_rro_static.py` — компилирует ВСЕ `values*/` (было: только `values-ru/`
  + `values/public.xml`); теперь `values/strings|plurals|arrays.xml` тоже в flat.
- `create_rro_min.py` — уже компилировал все `values*/`, поправок не потребовалось.

**Проверено:** перегенерация 100+100, rebuild 100+100 (0 FAIL), `validate.py`
(full, с aapt2 dump 200 APK) = **0 ОШИБОК**. В обоих новых APK (динамический +
статический) `aapt2 dump resources` показывает строку в ДВУХ конфигурациях:
`() "…" (default)` и `(ru) "…"`; plurals — то же. Flat-имена не пересекаются
(`values_strings.arsc.flat` ≠ `values-ru_strings.arsc.flat`) — коллизий в link нет.

**Эффект.** После установки оверлеев русский виден и при zh-локали, и при
en/uk/любой другой — на всех 100 приложениях, без правки locale на ГУ.

---

## [2026-09-24c] — Реальный log из `logs/bat/` (report 093642): `collect_report.bat` рвался на шаге [7/9], отчёт обрывался пополам

**Симптом.** Живой прогон `report` (записан в `logs/bat/collect_log.txt`,
каталог `logs/bat/report_2026-09-24_093642`) дошёл до `[7/9]` и оборвался:
`The system cannot find the file specified.` + `. was unexpected at this time.`
→ в `raw/` нет `42_anr.txt` (с трассой), `43_main/44_overlay_main/45_overlay_dump`,
нет `summary.txt` и zip. Каталог `report_*\raw` остался с неполным набором
файлов (00–41), без вердикта. `install.txt` рядом в этом же прогоне — **в порядке**
(91/100, `[WARN]` WT_WtSystemUI, 9 AOSP-target не ставятся `adb install` — всё
ожиданное, см. 22b-п.4).

**Корень (собрал Вино / wine-репро).** `collect_report.bat:270` — вторая строка
многострочного `REM`, у которой ПРИ ПЕРЕНОСЕ ПЕРЕД ЧИСЛОМ ИМЕНЕМ
**выпала приставка `REM`**:
```
269:     REM подтверждено репортом 22.09 21:26). adb root до дампа (на
270:     НЕ-руттируемом билде вернёт "cannot run as root" - безвредно).   <-- БЕЗ REM!
```
cmd исполняет её как команду: лексема `НЕ-руттируемом` не распознаётся, а
**неэкранированная `)`** в `безвредно)` **закрывает блок `if !N_ANR! GTR 0 (`
досрочно**, и хвостовой одиночный `.` становится командой сам по себе →
`. was unexpected at this time.`. Блок рвётся ДО `adb root`, ДО
`echo ==== содержимое последних 3 ANR…` и ДО `adb shell "for f in $(ls -t
/data/anr …)"` — поэтому `raw/42_anr.txt` содержал только `ls -l` (1 строку
файла), а не трассу, и не было ни шага [8/9], ни [9/9], ни zip.

**Диагностика.** Минимальное wine-репро (wine 9.0 cmd /c + mock adb):
- ДО: блок с `REM`-стр. 269 + выполняемой (без REM) стр. 270 →
  `Can't recognize '…' as an … command` × 3 (`НЕ-руттируемом…` / `.` / `)`),
  исполняется только до `)` — идентично живому обрыву.
- ПОСЛЕ (добавлен `REM` на стр. 270): блок проигрывается полностью,
  `AFTER_BLOCK` печатается, ошибок нет.

**Починено:** `collect_report.bat:270` — добавлен `REM ` в начало.
Блок [7/9] теперь всегда доходит до дампа трассы ANR и [8/9]/[9/9]/zip.

**Второй (неблокирующий) дефект в том же прогоне — `The system cannot find
the file specified.` в `collect_log.txt:8` (до того как collect_report начал
[1/9]).** Корень: `echo`-строки в баннерах `:CollectReport` с литеральным
`<TS>`/`<pkg>` — cmd парсит `<` как input-redirection →
"file not found", но баннер-`echo` на верхнем (не-блочном) уровне **не рвёт**
бат — collect_report всё равно запускается. Экранировано на 3 строках:
- `manage.bat:567` (`:CollectReport`, dynamic) — `^<TS^>.zip`
- `manage_static.bat:540` (`:CollectReport`, static) — `^<TS^>.zip`
- `install_all_static.bat:50` (верхний уровень) — `^<pkg^>/`

**Состояние устройства после прогона 24.09 (из install.txt + report_093642):**
91/100 установлено (те же 9 AOSP-target через `adb install` не ставят:
NetworkStack/MediaProviderLegacy/UserDictionaryProvider/DownloadProvider/
DownloadProviderUi/CompanionDeviceManager/MtpService/CaptivePortalLogin/
ContactsProvider), оверлеев `[x]=90 [ ]=1 ---=0`; `wt_wtsystemui` установлен
но `STATE_DISABLED` (SecurityException `UID2000 is not allowed to call
setEnabled for com.android.systemui` — тот же vendor-баг, что в 22b-п.4, довести
вручную либо через новую логику enable в повторной установке); ANR = 1 шт
09-14 (старый), FATAL в logcat = 1 шт (wecarspeech `NumberFormatException:
For input string: ",11"` — vendor-баг, не наш; см. 22b-п.4).

## [2026-09-24] — Обрезка тайла «ольш» (WT_SmartSoundEffect/«Больше»): fix + аудит влезания всех RU-строк в рамки layout'ов

**Симптом.** В `Звуковые эффекты` → «Рекомендуемые» в правом верхнем углу
секции вместо «Больше» рисовалось «ольше»/«ольш» — перевод шире кнопки,
Android обрезал хвост.

**Корень.** `res/layout/recommend_common_product.xml`: кнопка «更多» —
`LinearLayout` фиксированного размера `recommend_content_item_more_width_full`
= 128 dp; внутри `more_tv` (TextView, `wrap_content`,
`textSize=common_second_content_text_size` = 36 sp) + `iv_arrow` 48 dp
(`recommend_content_item_more_icon_height_full`). На текст остаётся
128−48 = **80 dp**. Шрифт Roboto @36: «Больше» ≈ 132 px > 80 dp →
`View` обрезает справа (текст зажат: wrap-виджет внутри фикс. контейнера с
`gravity=center_vertical`, правый край упирается в контейнер).

**Fix (RRO-оверлей, layout не трогаем).** `text_more` (更多) «Больше» →
**«Ещё»** (≈70 px @36 — влезает в 80 dp с запасом, «Ещё →» — нормальный
UI-паттерн на русском).
- `translations/WT_SmartSoundEffect.json`: `text_more.ru = «Ещё»`;
- перегенерированы `overlays/` + `overlays_static/` (generate_overlays*);
- пересобраны **оба** APK: `apks_rro_min/WT_SmartSoundEffect_RRO.apk` и
  `apks_rro_static/WT_SmartSoundEffect_RRO.apk` (create_rro_{min,static}
  `--only`, verify resId OK: 369 pin 1:1 с таргетом, aapt2 dump:
   `0x7f0f0145 string/text_more = "Ещё"` (canonical-ID таргета, как есть
   в smali R$string)).
- `validate.py --app WT_SmartSoundEffect`: 0 ошибок.
- Одинаковая строка `text_more` есть и в WT_ElectronicDirections
  («Больше» 51 px @16 в `tv_more` 48dp-высокой wrap-кнопке — влезает), не
  трогаем намеренно.

**Аудит «влезает ли перевод в выделенную рамку» (все 100 приложений).**
Новый инструмент `audit_fitboxes.py` → `logs/fitboxes_report.{txt,json}`:
скан `decompiled/*/res/{layout,layout-land,menu}*.xml` на
`TextView|Button|EditText` с `android:text="@string/NAME"`, RU — из
`translations/*.json`. Метрика: ширины/высоты из attr↔@dimen↔style-цепочка
(собственные values; стили внешних lib — default 14sp), размер шрифта attr →
@dimen → style → 14sp (bold/letterSpacing/lineSpacingExtra учитываются),
RU-ширина — PIL + **Roboto ГУ** (шрифт из прошивки, regular/bold), условный px
(dp==sp, density=1), перенос — жадный word-wrap (в т.ч. multi-line `\n`),
рамка = собственная dp-ширина/высота либо ближайший фикс. предок (верхняя
оценка); 0dp+weight в h/v-LinearLayout — доля родителя; h-LinearLayout —
вычитаются фикс. соседи; 1px-стубы и 0dp-constraint без предка исключены.
Вердикты: FAIL (одно слово/1-линия RU шире «своей» рамки ИЛИ рамки фикс.
контейнера-предка — вылезание/обрезка, класс симптома «…ольш»; ИЛИ строк RU
больше, чем вмещает собственная фикс. высота), CLIP (maxLines=1/ellipsize —
сужение по дизайну), WARN (впритык >92%, или RU растянулось на больше строк,
чем ZH).

Результат прогона: **проверено 3374 строки в фикс. рамках / 70 приложений**
— **FAIL: 132** (реальная обрезка/вылезание, **17 приложений**) +
**WARN: 139** (впритык/растянулось, но влезает) + **CLIP: 33**
(обрезка `maxLines=1`/`ellipsize` по дизайну). По типам FAIL:

- **76 — «слово/1-линия шире своего dp» (кнопки/чипы с textSize 24–40sp):**
  Fota `dialog_install_yes` «Подтвердить(%ds)» 348px@40 в 156–168dp,
  `cancel` 142px@40 в 140dp, WT_FiveChess `continue_game`/`exit_game`
  394/334px@48 в 111×72dp, WT_FusionNavigation `navi_res_exhausted`
  322px@24 в 164×48dp, WT_ElectronicDirections `text_video_reload` 250px@36
  в 174dp, AdayoDvr `video_play_orientation_left/right` 114–135px@36 в
  28dp (×3), плюс линейка кнопок WT_LightSoundLab/WT_MultiMediaCenter/
  WT_WTAISceneEngine/WT_HDCloudCamera;
- **42 — «строк RU больше, чем вмещает своя фикс. высота»:** Camera
  `ramp_tip` 5 стр. в 547×96dp, AdayoDvr `add_device_*_context` 2–3 стр. в
  H=48–96dp, WT_VehicleCenter `voice_*_hint` 2 стр. @32 в H=40dp, Fota
  `net_task_request`/`net_rollback_request` 3 стр. в 156×67dp и др.;
- **14 — «класс …ольш» (слово шире, чем даёт контейнер-предок — исходный
  симптом):** WT_VehicleCenter `idd_cost_week_drive_score` 552px@36 в 142dp,
  `widget_light_acc` «Автоматические фары» 300px@28 в 183dp, `idd_report_*`
  «Максимальная скорость/дистанция» 374–396px@32 в 218dp, WT_CarLink
  `title_card_near_use` «Последнее использование» 404px@32 в 168dp,
  WT_Launcher `history` «История направлений» 250px@24 в 128dp,
  `lane_navi_start` «Попробовать» 175px@28 в 152dp, WT_FusionNavigation
  `car_logo_using` «В использовании» 114px@14 в 77dp (×3 layout'а),
  `window_vent_3d» и др. (в JSON-отчёте `why` содержит пометку «класс
  симптома «…ольш»»).

Полный список с px/рамками/линиями — `logs/fitboxes_report.txt` (и `.json`).
В `CLIP: 33` обрезаются по дизайну (maxLines=1/ellipsize): WT_Launcher
`add_title` «Добавить команду» 249px@28 в 120dp (×6), `switch_music_tips`
361px@36 в 240dp (×2), WT_AIAssistant `sound_start_error_info`,
WT_Launcher `tbt_gps_lane_low_tip`, WT_VehicleCenter
`intelligent_remotectrl_tips` (marquee, 18.9K px) и др.
`text_more` (WT_SmartSoundEffect) после фикса: 69.5 px в 80 dp = **87% → ok**
(«Больше» в том же layout'е = 131 px = 165% → FAIL класса «…ольш»).

Порог FAIL по высоте — только для **собственных** фикс. размеров; по зонам
предков (parent-высота) учитываются только «однострочные» зоны
(< 2.5·высоты строки): FAIL, если RU в них не влезает, а ZH влезал;
иначе WARN. ScrollView/RecyclerView-предки исключены из FAIL по высоте.

**Ограничения аудита** (см. docstring): multi-line-`\n` — сегменты;
%s-аргументы — нижняя оценка (аргументы не подставляются); стили внешних
библиотек (wtcl.lib.*) → default 14sp; custom-виджеты (наследники
TextView/Button/EditText, напр. WTButton/WTTitleBar) вне скоупа;
ConstraintLayout-цепочки «0dp↔0dp» не решаются (верхняя оценка предком).
Значения `layout-ldrtl`, `layout-watch` — в прогоне (шум минимален).

## [2026-09-23b] — «Осталось - - км / дней»: рассогласование resId между RRO и таргетом (fix + пересборка всех 100)

Симптом: в `Умное обслуживание` с установленным RRO вместо цифр
пробега/дней показывались заглушки `Осталось - - дней` / `Осталось - - км`;
после удаления RRO цифры появлялись.

**Корень.** `aapt2 link` (`-I android.jar`, без привязки к таргету) выдавал
оверлею **собственные** resource-id: таблица строк под `type id=01` с
алфавитной перенумерацией (269 записей), а у таргета `com.wt.maintenance` —
`type id=0e`, 301 запись (см. `decompiled/…/res/values/public.xml`).
RRO применяется **по ID таргета**: в `MainActivity.smali` подстановка цифр —
`getString(R.string.maintain_mile_normal_text, …)` по `0x7f0e00a8`
(`0x7f0e009c` для дней), а по этим слотам «битый» оверлей отдавал не
`«Осталось %s км»`, а чужие строки (напр. slot `00a8` →
`maintain_mile_normal_default_text` = `- -`), либо в `values` вообще отсутств
ующие значения. `getString` возвращал неформат/пусто → на экране оставался
стартовый `<TextView android:text="@string/maintain_mile_normal_default_
text">` (layout_deepal_ev_view.xml) — те самые `Осталось - -`. Статичные
переводы (заголовки, имена) при этом работали, т.к. совпадали случайно либо
не идут через `getString(id,args)`.

**Fix.** В `create_rro_min.py` и `create_rro_static.py`:
- `write_resource_ids()` — автогенерация `res/values/public.xml` по CANONICAL-ID
  таргета (`decompiled/<app>/res/values/public.xml`) только для ресурсов,
  реально присутствующих в `overlays/(static)/<app>/res/values*/*.xml`
  (string/plurals/string-array/integer/bool/color/dimen/style);
- post-link `_verify_resids()` — `aapt2 dump resources` собранного APK,
  все `expect`-ID сверяются с фактическими, расхождение → **FAIL**
  приложения (защита от регрессии);
- для static `values/public.xml` добавлен в список `aapt2 compile`.

**Результат.** WT_AutoMaintenance: `type string id=0e`, ключевые ID
совпали 1:1 (`0x7f0e009b/9c/a7/a8`). Пересборка мин- и static-наборов:
**100/100 OK** в каждом, 0 `verify FAIL`, 0 предупреждений о ресурсах вне
таргета (все имена присутствуют в `public.xml` таргетов).

## [2026-09-23] — Итерация 2 по живому репорту 22.09 21:26: ops-логи потеряны, FATAL=«----------», ANR no-perm, enable молча

Пользователь прогнал `manage.bat install` на Windows+HU после правок
22.09-22b и прислал НОВЫЙ report-zip
(`deepal_dynamic_report_2026-09-22_212648.zip`).

**Подтверждено: правки 22b работают.** «Установлено: 91 /
ОТСУТСТВУЮТ: 9» (было 0/100), отсутствующих ровно 9 AOSP-target,
`phonelink` установлен и `[x]` (GROUP3-чинка), у пакетов виден
`ver=0` (пarsing versionCode), `---` = 0.

**Починено по новому репорту (5 багов):**

1. **Ops-логи ПОТЕРЯНЫ — в обоих zip «ops_*.log: 0 файла».** Уточнённый
   корень: утром было видно имя `logs\ops_uninstall_OTST:=_.log` →
   replacement ПАРЫ `!OTST:=_!` в `:OpSTs` дало OTST с буквальным хвостом
   `:=...`; имя с `:` незаконно в Windows-имени файла → **файл не
   создавался, все `>>` молча теряли вывод** (отсюда и «0 файла», и
   отсутствие ops-логов в 21:26 zip). Вино воспроизвело падение на этой
   конструкции. **Заменено**: `:OpSTs` = только substrings — wmic
   `localdatetime` → `!DT:~0,14!` → фолбэк powershell `Get-Date
   yyyyMMddHHmmss` → `R%RANDOM%%RANDOM%` (те же идиомы, что дают
   корректное имя report-папки). Имя: `ops_<mode>_YYYYMMDDHHMMSS.log`.
   Вино: имя корректное, лог создаётся, внутри полный `db:`+`RESULT`.
2. **Счётчик FATAL бит**: `find /C /I` печатает `---------- 4 C(S)
   FOUND`, а `for /f` (tokens=1) забирает `----------` (ДЕФИСЫ, не
   число) → в summary `FATAL EXCEPTION в logcat: ----------   0 - всё
   чисто` при 4 РЕАЛЬНЫХ FATAL (wecarspeech), и `if GTR 0` не fires
   (не-число). **Заменено** счётчиком по строкам (тот же idiоm, что
   у N_ANR, который честно дал 1): `for /f %%c in ('find /I "FATAL
   EXCEPTION" !F_FATAL!') do set /a N_FATAL+=1`.
3. **ANR-trace: Permission denied.** `head -150 /data/anr/$f` — предыдущая
   итерация починила путь, но файл `anr_*` = `-rw------- system
   system`: shell-юзер не читает, а `adb root` запрашивался ТОЛЬКО при
   пустом листинге (здесь листинг прошёл). **Запрошен `adb root` ВСЕГДА
   перед дампом** + `|| echo '[не читается: Permission denied?]'`
   fallback. Оговорка: build `eng.Jenkins…user/test-keys` — если
   `adb root` там недоступен, trace так и не достанется (видно будет
   честно в raw/42).
4. **`overlay enable` при install был НЕВИДИМ (`>nul 2>&1`)** —
   WT_WtSystemUI установлен, но `STATE_DISABLED`, и в логах/ops ZERO
   следов почему. **Теперь**: enable через `:DoOpAdb` (вывод виден +
   в ops-лог `db:`), `ping`-пауза + 1 retry; если всё равно rc≠0 →
   экран `[WARN] установлен, НО оверлей НЕ включён - довести:
   manage.bat enable` + `RESULT: WARN` в ops-лог (установка всё равно
   считается OK — apk на месте).
5. **Матч «Success» хрупкий**: точное `"%%I"=="Success"` не прощало
   хвостовой пробел/CR (в живых логах строка захватывалась `Success ` —
   и именно поэтому установка шла в ERROR-ветку даже после 22b, в
   wine-репро `Success `→ERROR). **Заменено** префикс-матчем по первым
   7 символам (`!L:~0,7!=="Success"`, case-insensitive) + `delims=`:
   «Performing…/Failure […]» стартуют иначе, ложных срабатываний нет.

**Доп. (коллектор):** summary при ровно 9 отсутствующих больше не
кричит «ПРОБЛЕМА» вслепую — доп. строка «это 9 AOSP-target… решение:
/vendor/overlay или исключение» + VERDICT `ОЖИДАЕМО: 9 AOSP-target…;
остальные на месте` (9 = известные, см. нижний раздел 22b-п.4).

**Состояние устройства после прогона (22.09 21:26):** 91/100
установлено, 90 `[x]` / 1 `[ ]` (wt_wtsystemui — довести `enable`
вручную либо переустановить: новая логика сама включит и если нет —
напишет `[WARN]` вместо молчания). wecarspeech FATAL: 2→4 в crash
buffer (vendor-баг растёт, не наш), ANR 1 (09-14, старый).

## [2026-09-22b] — Реальные логи с HU: починка manage.bat (false ERROR / двойной счёт) + отчёта (0/100)

Анализ присланных живых логов (`logs/bat/install-apk.log`,
`install-apk1.log`, `uninstall-apk.log`, diag + report-zip
`deepal_dynamic_report_2026-09-22_090630.zip`).

**1. manage.bat: установка была успешной, но лог/сводка писали
`[ERROR] установка не удалась` + «OK: N / Ошибок: N» одновременно
(13/13 и 100/100).**
- Корень: та же класс-бага «неэкранированная `(` в `echo` внутри блока»,
  что чинили 20.09 (12 мест) — но тогда экранировали только ЭКРАННЫЕ
  `echo`, а строки **ops-лога** `if defined OPS_LOG echo   RESULT: OK (…)
  >> "%OPS_LOG%"` остались с живыми скобками. Скобка внутри тела
  `if (...)`-блока ломает парсинг блока → cmd исполняет и ветку `if`, и
  ветку `else`, и «хвост» от `)` как команду (в консоли —
  `Can't recognize '…)' as a command`) → `OK_C+=1` и `FAIL_C+=1` СЧИТАЮТСЯ
  ОБА. Отсюда `OK: 13` И `Ошибок: 13` в одном итоге.
- Wine-репро подтвердило: ДО — `RESULT: OK (…)` → «Can't recognize»,
  счётчики бьются; ПОСЛЕ — `ok=2 err=0`, в ops-логе полная строка.
- **Починено:** экранирование `(`→`^(`, `)`→`^)` во ВСЕХ строках
  `RESULT:` ops-лога: `manage.bat` (6: install OK/ERROR, enable
  ERROR×2, uninstall OK/ERROR) + `manage_static.bat` (5: mkdir/push
  OK×2/ERROR×2/WARN). Тем же патерном были сломаны в обёртках:
  `revert_patched.bat` (3), `uninstall_all_static.bat` (1),
  `install_all_static.bat` (1) — также экранированы.
- Как читать логи: строка `RESULT:` в ops-*.log + финальный
  «конец: всего/ok/err» — теперь достоверный источник.

**2. GROUP3: `WT_PhoneLink` такого APK нет (оба менеджера).**
Файл на самом деле `PhoneLink_RRO.apk`, пакет
`com.android.vendor.translate.rro.phonelink` (map в create_rro_*.py:
`"PhoneLink": "com.adayo.phonelink"`). Группы/`auto`-пресеты и
collect_report искали несуществующий `…_rro.wt_phonelink`.
**Починено** в `manage.bat`/`manage_static.bat`: GROUP3
`WT_PhoneLink` → `PhoneLink`. (В живом log: `adb uninstall
…rro.wt_phonelink → Failure [DELETE_FAILED_INTERNAL_ERROR]` — реально
удалять нечего, пакета никогда не было.)

**3. collect_report.bat: «Установлено: 0 / ОТСУТСТВУЮТ: 100 <=== ПРОБЛЕМА»
— ложная тревога.**
Формат `pm list --show-versioncode` на этом ГУ:
`package:PKG versionCode:0` (ПРОБЕЛ, без `=`), а парсер искал
`package:PKG=` (граница `=` — заведены под новый формат в 19.09d, на
живом не сверяли). **Починено:** основной поиск `package:PKG `
(хвостовой пробел — тоже отделяет `fota` от `fotaservice`), фолбэки
`package:PKG=` и без хвоста; versionCode — `tokens=3 delims=:`
(у обоих форматов ровно два `:`). Проверка на данных реального
репорта: ДО 0/100, ПОСЛЕ **90/10** — т.е. вердикт «ПРОБЛЕМА» в
summary.txt исчезнет, а 10 «НЕТ» — реальные 9 ниже + phonelink.

**4. Настоящие проблемы на устройстве (по report + diag, НЕ баги скрипта):**
- **9 оверлеев, целящих AOSP-пакеты, НА ЭТОЙ сборке НЕ УСТАНАВЛИВАЮТСЯ
  через `adb install`**: NetworkStack, MediaProviderLegacy,
  UserDictionaryProvider, DownloadProvider, DownloadProviderUi,
  CompanionDeviceManager, MtpService, CaptivePortalLogin,
  ContactsProvider (targets `com.android.providers.*`,
  `com.android.networkstack`, `…captiveportallogin`, `com.android.mtp`,
  `…companiondevicemanager`). Доказательства: (а) `adb uninstall` —
  `Failure [DELETE_FAILED_INTERNAL_ERROR]`; (б) diag overlay list —
  **ни одного из 9 нет в списке** (не «выключен», а «не существует») →
  install на них всегда проваливался, а manage.bat из-за п.1 показывал
  им success-строки. Решение (не принято): перевести эти 9 в СТАТИЧЕСКИЕ
  оверлеи /vendor/overlay (manage_static.bat, APK уже собраны в
  apks_rro_static/) либо исключить из 100 целей (в GROUP1) — до решения 9 в dynamic-списке
  бессмысленны.
- **wt_wtsystemui установлен, но STATE_DISABLED** (diag dump). После
  починки п.1 `manage.bat install` сам делает `overlay enable`;
  либо вручную `adb shell cmd overlay enable --user 0
  com.android.vendor.translate.rro.wt_wtsystemui`.
- **FATAL ×2 (за 30 сек) в `com.tinnove.wecarspeech`** — VENDOR-баг,
  не наш: `NumberFormatException: For input string: ",11"` в
  `com.tinnove.wecarspeechtools.i.a(FileSizeUtil.java:3)`
  (crash-buffer, raw/40_crash.txt). Наш WT_TSpeech RRO не трогает эту
  утилиту (FileSizeUtil парсит число из каталога логов). Жалоба
  вендору / следить, после установки не растёт ли.
- **ANR 1 шт. 09-14 12:46** — старше установки оверлеев, сравнения нет;
  от него ещё и trace не достался (см. п.5).
- 44_overlay_main.txt пуст — в logcat-main 0 строк про overlay;
  норм.

**5. collect_report.bat: ANR-dump читал файл НЕ из /data/anr.**
`for f in $(ls -t /data/anr …|head -3); do head -150 $f; done` —
`$f` = имя без пути, shell-кwd на устройстве ≠ `/data/anr` → в raw/42
вместо трассы было `head: …: No such file or directory`. **Починено:**
`head -150 /data/anr/$f`.

**Доки/статус:** TODO-пункт «Live-HU-проверка bat-менеджеров» закрыт:
реальные форматы `pm list`/`cmd overlay list` сверены с живым репортом,
report-zip разобрался, ops-логи прочитаны; wine-ограничения (20.09-20)
подтвердились, как и было задокументировано.

## [2026-09-22] — Перевод ЗАШИТЫХ в layout надписей (RRO их не покрывает)

**Проблема (полина «массажа»):** на ГУ часть надписей оставалась китайской,
хотя переводы есть в наших RRO‑APK. Причина: эти фразы **не ресурсы**
(`values/strings.xml`), а литералы, зашитые прямо в binary‑XML
(`res/layout/*.xml`, `res/menu`, `res/xml`) — `android:text="座椅按摩"`,
`android:contentDescription="提示"` и т.п. RRO переопределяет только
*values*-ресурсы; layout как файл из оверлея **не линкуется** (aapt2 не
резолвит `@dimen/@drawable/@id/attr` target‑пакета из overlay — проверено
минимальным тестом: 1 layout + 1 `@dimen` → `resource ... not found`).

**Решение:** патчить бинарные строки самх APK + переподписывать платформ.
ключом (сертификат `platform.jks` = сертификат заводских APK —
`...c8a2e9bccf...`, схема v3 — совместимо с in‑place обновлением).

**Скринт / модули (новые):**
- `axml.py` — reader/writer бинарного XML (ResStringPool UTF‑8/UTF‑16,
  chunk‑структура, element/attr). **Байт‑в‑байт round‑trip** на 137/137
  файлах (StorageWarn) и 0 parse‑fail на всх 122 APK (3761 строчек‑строк).
  Лимит 32 767 байт aapt2 НЕ распространяется (это values‑ресурсы; string‑pool
  значения хранятся как есть).
- `scan_layout_literals.py` → `logs/layout_literals.json`: авторитативный
  обход **всех 122 APK** (zip‑вход), находит строковые атрибуты (type `0x03`),
  несущие CJK и не являющиеся референсами (`@...`/`?...`).
  **56 приложений, 1487 уникальных литералов, 3761 вхождение.**
- `translate_layout_literals.py` → `translations/layout_literals.json`:
  перевод 1487 фраз (410 — из уже готовых `translations/*.json`; 1077 — LLM,
  batch 25, guard: CJK=0, `^`-варьианты 1:1, плейсхолдеры `%s/%1$s/%%` 1:1).
- `patch_apk_layouts.py` → `apks_patched/*.apk`:
  1) парс каждого AXML‑запсри в APK; заменяет пул‑строку только если CJK‑литерал
     имеет `ru`; 2) **verify_roundtrip**: структура (чужое дерево индексов)
     идентична + в пуле поменяны ТОЛЬКО индексы, значения коих = `ru`;
     иначе приложение = ABORT; 3) surgical zip: копируем APK → `zip -d`
     только затронутые записи → `zip` новые байты → `zipalign -f 4`;
     все остальные записи — побайтово как в оригинале.
- `verify_patched_apks.py` → `logs/apk_patch_verify.json`: для 56/56 —
  zip integrity OK, aapt2 decodable, **cjk_left=0**, сертификат=платформ.
- `install_patched.bat` / `revert_patched.bat` (Windows+adb):
  - **data** (по умолчанию): `adb install -r -d` поверх (тот же
    cert/versionCode — обновляется на месте, данные сохраняются;
    откат: `pm uninstall <pkg>` → системная версия возвращается).
  - **system** (root): `pm path <pkg>` → push поверх системного пути
    → `adb reboot`; откат — push заводского `original_apks/<App>.apk` обратно.

**Итог:** **56 APK сшить + подписать**, **3761 вхождение переведено, cjk_left=0.**
Аудит: `logs/layout_literals.json`, отчёт: `logs/apk_patch_report.json`.
Верификация: `logs/apk_patch_verify.json` — 56/56 OK.
Пользовательские примеры (换挡音/车外音/座椅按摩/闭锁音/按摩类型) подтверждены
в бинарном XML патченных APK ( WT_VehicleCenter, WT_TinnoveSmartScene ).

**Не покрыто (RRO/этот метод не трогают, отдельные решения):**
- `WT_TSpeech` голосовые hotwords (assets `rule-config.json`,
  `command-config*.json`, `public/cfg/*.cfg`) — команды ассистента;
- `WT_MLWecarControl/res/raw/carconfig_*.json` (документы конфигурации авто);
- `WT_TinnoveCoreService/res/raw/virtual_data.xml`; анимационные JSON
  (label‑имена в `lottie` `nm`/`ind` — не пользовательский текст);
- strings, зашитые в `smali`‑константы (аудит: 0 совпадений для всех примеров);
- `values-en` fallback и `unknown/`‑дубликат (`WT_VehicleCenter`) (артефакт
  apktool; к рантайму отношения не имеет).

**Ограничения:** RRO layout override НЕВОЗМОЖЕН (см. выше). Пересборка
всего APK через `apktool b` не проходит (aapt/aapt2: `<id>` vs `item`,
invalid drawable names `$...`), поэтому выбран бинарный патч — не требует
смали/ресурсов, только touch‑ed XML файлов.

## [2026-09-21b] — «Пайплайн с нуля» (исполнение): 5 дыр закрыты + найден и
починен баг первичной ET-версии извлечения

**Задача (якорь):** повторный прогон `extract_cjk → improve → validate →
generate → build` из чистого состояния не должен воспроизводить найденные 20–21.09
баги (потеря строк с разметкой; тихие `STRING_TOO_LARGE` в готовых APK).
Сессия выполнялась по плану из TODO/MEMORY; бэкап исходников (состояние
до работ 21.09) — `/tmp/opencode/bak_pipeline21/`.

**Что закрыто:**
1. **Извлечение через ET с zero-drift** (`extract_cjk.py`):
   - **plain**-строки — прежним raw-regex'ом на битах файла
     (17 832 значения `zh` побайтово как раньше, дрейф от ET-unescape+реэкранга
     — исключён);
   - **markup**-строки (`<b>/<a>/<annotation>/<Data>…`) — ET-путь
     (`_string_inner_xml`), помечаются `"styled": true`
     (перевод — по фрагментам, `improve_translations.py --styled`);
   - **plurals/arrays** — ET/`itertext()` (полный текст, включая вложенное);
   - при `ParseError` файла — fallback на regex, как до 21.09.
   Песочная сверка (read-only, живые данные не трогали): **0 потерь, 13 новых
   `styled`, 0 text-drift** против старого извлечения; 13 записей побайто
   совпадают с текущими `translations/*.json`.
2. **Лимит aapt2 32 767 UTF-8-байт** учтён во всех слоях:
   - `create_rro_min.py` / `create_rro_static.py`: aapt2-предупреждение
     `string too large … written instead as 'STRING_TOO_LARGE'` (rc=0, идёт в
     STDOUT) → **FAIL приложения** (раньше было «100 OK» с заглушкой внутри);
   - `validate.py`: `STRING_TOO_LARGE` в APK (string/array/plurals) = **ОШИБКА**
     (exit 1);
   - `validate.py` (WARN-аудит): `ru` > 32 700 байт (post-`esc` у plain / raw у
     styled) = «не влезет, строка не войдёт в оверлей»;
   - `validate.py` (WARN-аудит): CJK-ресурсы `decompiled/*/res/values|values-zh*`,
     которых нет в `translations/*.json` = «потеря при извлечении»
     (ранний сигнал — как с 13 утраченными).
3. **`improve_translations.py --styled`** — режим перевода rich-text
   по фрагментам (механика `fix_markup3.py`): `TOKEN`-regex + `tokenize` +
   `cjk_frags` (с `pre`/`post` для голых тел сущностей `emsp;`), LLM-перевод
   каждого CJK-фрагмента (`max_tokens=max(5000,3000+4·len)`), verify
   (токены zh↔ru 1:1, CJK=0, ratio≥0.2), атомарная запись в `ru`.
   Кэш — `translations/improve_styled_cache.json` (idempotent), stuck —
   `logs/styled_stuck.txt`. Сеть НЕ гонялась (13 уже переведены); механика
   проверена на 13 реальных строках (round-trip 1:1, CJK сохранён) + в selftest.
4. **`test_improve_selftest.py`** — новые блоки (`test_styled`,
   `test_aapt2_limit`): tokenize (вкл. экранированные теги/сущности по
   реальным паттернам), cjk_frags pre/post, verify per-fragment, reassemble
   1:1 + «модель вставила лишний токен → None», `fmt_plain_checked/
   fmt_styled_xml → None` при >лимите (вкл. Data-фолбэк), детект
   `too large` по stdout в `create_rro_{min,static}`, `parse_aapt2_dump`
   + `check_apk` на реального APK со строкой >32767 → ОШИБКА. **Итог 90/90.**

**Найден и починен баг (важно для повторных сессий):**
Первичная версия ET-извлечения (начало сессии, 10:39) имела ДВА дефекта,
проверенные песочной сверкой:
- **(1)** `ET.tostring(child)` САМ печатает `child.tail` — при ручном
  повторном дописывании tail значение **дублировалось** в конце
  (например, Файл: `…<b>所有</b>用户移除此应用及其数据。</string>` →
  Извлеч.: `…<b>所有</b>用户移除此应用吗？系统将为设备上的<b>所有</b>用户移除此应用及其数据。用户移除此应用及其数据。`);
- **(2)** `ET.tostring` экранирует текст `' > '` → `' &gt; '`, тогда как в
  исходных файлах `&lt;strong>` хранится с **литеральным** `>` — для 9 строк
  WT_FusionNavigation (`<Data>…&lt;</b>`-подразметка) `zh` уехал от файла
  (`&lt;strong>` → `&lt;strong&gt;`), а перенесённые из fix_markup3 переводы
  `ru` были сверены именно с файловой формой → рассинхрон токенов.
Лечение: `_string_inner_xml` — **ручной** обход дерева (без `tostring`),
минимум-escape `&`→`&amp;`, `<`→`&lt;` (`>` в тексте не трогается), self-closing
`<br/>` как в исходнике, `tail` пишется ровно один раз. После лечения:
0 потерь, 13 `styled`, 0 text-drift.

**Решения/ограничения:**
- Лимит байт — **только** в генераторах (единая точка, как решено 21.09);
  `--styled` пишет `ru` даже при переливе — отсечёт генератор.
- 6 перелившихся строк (4 WTN-документа + `road_book_my_agreement_two_content`
  + `WT_GameCenter/str_procotol`) **намеренно не в оверлеях** (на ГУ — текст
  источника, а не «STRING_TOO_LARGE»); полный перевод в `translations/*.json`.
- `extract_cjk.py` **ОБНУЛЯЕТ `ru`** — не гонять после перевода (см.
  предупреждение в WORKFLOW.md шаг 2 и шапку `extract_cjk.py`).
- `fix_markup2.py`/`fix_markup3.py` — эталон механики, удалять не будем.

**Верификация (данные не менялись, только код пайплайна):**
- 100+100 APK пересобраны, **0 FAIL**, md5 всех 200 APK **не изменились**
  (bазовые: `/tmp/opencode/apk_before_{min,static}.txt`);
- `validate.py` (полный, aapt2 dump 200 APK): **0 ошибок** (7 WARN = 6
  переливов + 1 «ru-width», штатные);
- `validate.py`: 17 840 строк, форматтеров 0, CJK-in-ru 0, потерянностей
  (1d) 0;
- `test_improve_selftest.py`: **90/90**.

## [2026-09-21] — 13 «не переведённых» строк + лимит aapt2 32767 (find + fix + rebuild)

**Симптом:** пользователь заметил непереведённые фразы на ГУ.

**Причина 1 — 13 строк с вложенной разметкой никогда не извлекались.**
`extract_cjk.py` строит `<string>` regex'ом `([^<]*)` — обрывает на первом `<`.
Строки с `<b>`, `<a>`, `<annotation>`, `<Data>…&lt;br/&gt;…</Data>` никогда не
попадали в `translations/*.json`, и на ГУ оставались китайскими:
- WT_FusionNavigation: clause_{title,content,time}, clause_{title,content1,time},
  policy_{title,time,content,content1,content2} (уступки и политика Amap);
- Fota/upgrade_service_agreement_content (~3 KB, OTA-соглашение);
- CarService/imsi_protection_warning (`<annotation id="url">`);
- PackageInstaller/uninstall_application_text_all_users (`<b>`);
- ManagedProvisioning/read_more_delete_profile (`<a href>`).

**Причина 2 (скрытая, была ДО сессии) — aapt2 молча режет строку в
`STRING_TOO_LARGE`, если её значение > 32 767 UTF-8-байт.** Проверено
binary-search'ом: 32 767 OK / 32 768 FAIL; одинаково для plain и span/styled.
До 20.09 в ГОТОВЫХ APK было 2 такие строки (аapt2 warning, не error — стройка
«100 OK / 0 FAIL», но значения-заглушки «STRING_TOO_LARGE»):
- `WT_FusionNavigation/road_book_my_agreement_two_content` (33 152 симв → 61 KB);
- `WT_GameCenter/str_procotol` (38 949 симв → 70 KB).
Это и был реальный источник «не переведённых фраз» — на ГУ там читалось
буквально «STRING_TOO_LARGE».

**Лечение:**
1. **Перевод по фрагментам.** `fix_markup2.py` (пер-группа) + `fix_markup3.py`
   (пер-фрагмент с кэшем /tmp/opencode/frag_cache.json, idempotent). Строку
   режут на разметочные токены (теги / экранированные теги / сущности) и
   текстовые фрагменты; модель переводит только текстовые фрагменты;
   пересборка из переводов + исходных токенов = 1:1-разметка гарантированно.
   Верификация: порядок и число токенов zh==ru, CJK-in-ru=0, length-ratio.
   Записи помечены `"styled": true` в `translations/*.json` (7 из 13 влезли,
   5 не влезли — см. ниже).
2. **Генераторы.** `generate_overlays.py` + `generate_overlays_static.py`:
   - новая ветка `fmt_styled_xml(name, ru)`: записи `styled:true` пишутся в
     `values-ru` VERBATIM без `_esc()` (aapt2 сам парсит спаны реальных тегов
     `<b>/<a>/<annotation>` и `<Data>…</Data>`; `formatted="false"`);
   - **лимит `AAPT2_MAX_BYTES = 32700`**: значение строки (post-esc у plain,
     raw у styled), если > лимита, НЕ пишется в оверлей — иначе aapt2 подставит
     «STRING_TOO_LARGE»; на ГУ тогда остаётся исходный текст (лучше). Генератор
     печатает `[WARN] <app>: не влезли в лимит aapt2: …`;
   - обычный (plain) путь через `fmt_plain_checked` — тот же лимит.
3. **`improve_translations.is_target()`** теперь промахивает `styled` записи,
   чтобы `--all`/`--defective`/`--long` не перегенили и не сломали разметку.

**Итог по 13:**
- В оверлей и работают (5): clause_title, clause_time, policy_title, policy_time,
  policy_content2 (все WT_FusionNavigation styled) + Fota/upgrade_service_
  agreement_content styled + CarService imsi styled + PackageInstaller
  uninstall styled + ManagedProvisioning read_more styled.
- **Не влезли в лимит (6, в APK их нет → на ГУ текст источника):**
  4 WT_FusionNavigation документы (clause_content 69 KB, clause_content1 45 KB,
  policy_content 44 KB, policy_content1 70 KB — рус. перевод ~2x китайского по
  байтам) + 2 прежних (road_book_my_agreement_two_content, WT_GameCenter/
  str_procotol). Перевод этих 6 сохраняется в `translations/*.json` (поле
  `ru`, со всеми токенами размéтки); включать в оверлей можно только через
  split на несколько ресурсов + правку приложения (не решено).

**Пересборка:** generate_overlays(±static) → create_rro_min.py (100 OK / 0 FAIL,
2942 KB) + create_rro_static.py (100 OK / 0 FAIL, 2946 KB). `validate.py`:
**0 ОШИБОК** (aapt2 dump 200 APK: 0 STRING_TOO_LARGE, 0 CJK-in-ru, 0
форматтеров). Self-test improve: **59/59**.

## [2026-09-20] — Wine-проверка bat: пойманы и починены 13 багов (не только на бумаге)

**Сделано:** поставлен `wine-9.0` (headless) + mock-`adb` (bash, эмулирует HU:
97/100 пакетов, оверлеи в обоих форматах, 1 `---`, ANR, logcat) и прогнаны
`manage.bat`/`manage_static.bat`/`collect_report.bat` под Wine.

**Подтверждено (работает):**
- синтаксис и структура всех трёх bat — выполняются до конца без parse-ошибок;
- меню появляется без adb (задача 19c);
- `collect_report.bat` собирает все `raw/*.txt` + `summary.txt` (new 43/44/45,
  ops-копия, сборка summary) до `pause`.

**Поймано и ПОЧИНЕНО (13 багов — ломали бы на реальной Windows):**
- **12 × неэкранированные скобки в `echo` ВНУТРИ `if (...)`-блоков.**
  `echo ... (текст) ...` в блоке cmd/бат — `(` после `(` открывает вложенный
  блок → текст после неё исполняется как команда: обрезанный вывод +
  `Can't recognize 'текст'`. Вино это воспроизвело точно. Исправлено
  `(` → `^(`, `)` → `^)` в:
  - `collect_report.bat`: L145 (root запрошу сам), L265 (head -150),
    L276 (сборка шага 6/9), L283 (---);
  - `manage.bat`: `[ERROR] (пакет не установлен?)` ×2 в DO_ENABLE/DO_DISABLE,
    `[ERROR] (не установлен?)` в DO_UNINSTALL, `Overlay status (все)` /
    `(наши, %FILTER%)` в :ShowStatus;
  - `manage_static.bat`: `[WARN] не удалён (папки не было...)` в DO_UNINSTALL,
    `Overlay status (все)` / `(наши, %FILTER%)` в :ShowStatus.
- **«ECHO is OFF» в `summary.txt`** — `echo  !INC_LINE!` (строка без литерала)
  при ПУСТОМ `INC_LINE` (нет adb/getprop) = голый `echo` → cmd печатает
  «ECHO is OFF» внутрь summary. Исправлено: дефолты `MODEL/REL/INC_LINE=?`
  (аналогично уже-existing `LOC_LINE`). Подтверждено: после чинки 0 вхождений.

**Ограничение Wine (НЕ баг скрипта, на реальной Windows работает):**
- mock-`adb` (bash-скрипт) не исполняется `cmd.exe` как внешняя команда →
  adb-зависимые поля (`pm list`, `overlay list`, `logcat`, `getprop`) пусты;
- `findstr /R /C:"…$"` в wine-реализации даёт false-negative (на Windows по
  MS-доку `$` = «конец строки» корректен) → ADB-детект в wine «не видит»
  устройство — это НЕ значит бит в реальном бате;
- `timeout.exe`/`wmic`/`powershell`/`tar` не подняты → `nods_`-таймстемп, zip
  не собрался (на Windows `timeout` есть — это штатно).
- **Вывод:** «сбор логов с устройства» (adb-данные, форматы `pm list` /
  `cmd overlay list`) проверяется ТОЛЬКО на живой Windows + adb + HU.

**Портативность:** `:DoOpAdb` — вместо `for /f "usebackq ... in ("!F!")`
(не расширяется в wine) на `for /f ... in ('type "!F!"')` — расширяется и в
wine, и на Windows; опс-лог и ERRORLEVEL отработали.

## [2026-09-19d] — Логи установки/работы apk: ops-логи + починка парсера отчёта

**Задача:** в `report` (или вручную) видеть логи УСТАНОВКИ (install/enable/
disable/uninstall) и РАБОТЫ APK, а не только итоговый статус. Нашлись и
починены баги в `collect_report.bat`, из-за которых отчёт был неполноценным.

**1. Ops-логи установки (оба менеджера).** Каждое изменяющее действие теперь
пишется в `logs\ops_<mode>_<TS>.log` (dynamic: install/enable/disable/
uninstall; static: install/uninstall): заголовок (date/time, mode, preset,
schema, + root для static), на каждый пакет строки вывода adb-команды и
`RESULT: OK/ERROR/WARN`, финальный итог (всего/ok/err/нет_apk). Консоль не
теряет вывод (дублируется в лог). Хелперы: `:OpSTs` (таймстемп под имя
файла), `:DoOpAdb` (эхо adb-вывода в консоль+лог и сохранение ERRORLEVEL —
`for /f` сам обнуляет код, поэтому вывод идёт в `%TEMPF%`).

**2. `collect_report.bat` — баги (все подтверждены симуляцией форматов adb):**
- **Установленность всегда «НЕ УСТАНОВЛЕНО» (критично).** Поиск
  `findstr /C:"package:PKG "` и парсинг `tokens=2 delims=пробел`: строка
  `pm list --show-versioncode` = `package:PKG=versionCode=N` (БЕЗ пробелов)
  → все 100 пакетов «отсутствуют», versionCode пуст (вердикт всегда ложный).
  **Исправлено:** граница `=` (`package:PKG=`) + `tokens=3 delims==`, фолбэк
  на пробельный формат; БЕЗ `usebackq` (иначе `("строка")` = имя файла).
- **Оверлеи всегда «не в списке».** Строка ищется `findstr /C:"PKG "` (пробел
  после пакета) — а в выводе `cmd overlay list` пакет стоит В КОНЦЕ строки.
  **Исправлено:** двойной поиск — `PKG ` (новый формат «... (base)») И regex
  `^.*PKG$` (конец строки, точки экранированы, `fota`≠`fotaservice`).
- **ANR — только `ls`, без root/trace**, несмотря на обещание в README.
  **Исправлено:** при пустом/недоступном `/data/anr` авто-`adb root` + дамп
  последних 3 trace (`head -150`); число ANR-файлов в summary.

**3. `collect_report.bat` — добавлено в сбор (raw/):**
- `43_main.txt` — `logcat -b main -t 2000` (лог РАБОТЫ apk);
- `44_overlay_main.txt` — из main строки `overlay`/`idmap`/наши пакеты
  (по docs: диагностика ошибки оверлея через idmap);
- `45_overlay_dump.txt` — `cmd overlay dump` по оверлеям в ошибке `---`
  (собираются в шаге 6/9) — ровно то, что recommended в docs;
- **последние 10 `logs\ops_*.log`** — логи установки попадают прямо в zip.
- summary: новые секции «ЛОГИ ОПЕРАЦИЙ (установка/работа)», строка ANR,
  авто-вывод теперь учитывает ANR; нумерация шагов 8→9.

**Доки:** README (состав zip + ops-логи), WORKFLOW (шаг 9, история 5.11),
MEMORY. Все 3 bat: скобки/goto/CRLF проверены.

## [2026-09-19c] — `manage.bat`/`manage_static.bat`: меню появляется ВСЕГДА (починка ADB-блокировки)

**Симптом:** при недоступном adb скрипт «зависал» на подключении — меню не
появлялось вообще (окно либо пустое после `cls`, либо вечные 45-секундные
циклы ожидания авторизации).

**Причины (обе):**
1. `:ADB_CHECK` вызывался через `call` ещё ДО `:MENU`, и его `exit /b 1`
   при ошибке убивал ВЕСЬ скрипт (a `call`ed label's `exit /b` returns to
   caller, but error-ветка выходила из всего bat) — меню не рисовалось.
2. Ветка "unauthorized" делала `set /a A_N-=1` после каждого 45-секундного
   ожидания → счётчик «попыток» не рос → цикл шёл ВЕЧНО, если кнопка
   «АВТОРИЗОВАТЬ» на HU не была нажата.

**Исправлено (оба менеджера, одинаково):**
- Меню показывается СРАЗУ: `:MENU` → `:MENU_SCREEN` с баннером статуса ADB
  от новой `:ADB_CHECK_FAST` (одна `adb devices`, без ожиданий/рестарта
  демона). Статус-строка: `[OK] ADB: устройство подключено` /
  `[!!] ...разрешите отладку... затем [R]`.
- Полный `:ADB_CHECK` (3 попытки + `kill-server`, лимит авторизации
  3×45 сек) теперь — только по [R] или в момент выбора действия.
- Новый пункт меню **[R]** «Проверить ADB заново».
- `:ADB_CHECK` больше никогда не прерывает скрипт (`exit /b 0` + флаг
  `A_OK`); CLI-ветка при отсутствии adb печатает ошибку и совет запустить
  меню.
- Сброс счётчиков при успешном подключении (симметрия со static-вариантом).

## [2026-09-19b] — Сбор логов для анализа: `collect_report.bat` + режим `report` в менеджерах

Задача: одним действием снять «установлена ли вся установка, есть ли сбои,
правильно ли работает перевод» и получить zip для отправки на анализ.

**Код:**
- `collect_report.bat` (новый, вызывается из обоих менеджеров; только-чтение —
  ничего на устройстве не меняет и не удаляет):
  - `raw/00_device.txt` — getprop + uptime + df;
  - `raw/01_adb_host.txt` — scheme/timestamp/adb version;
  - `raw/02_apk_local.txt` — APK на хосте (что планировалось ставить);
  - `raw/10_locale.txt` — system_locales / ro / persist;
  - `raw/20_packages_ours.txt` — `pm list packages --show-versioncode <FILTER>`;
  - `raw/25_overlay_all.txt` — полный `cmd overlay list --user 0`;
  - `raw/26_vendor_overlay.txt` — (static) `ls -l` наших папок в
    `/vendor/overlay/` (сам запрошу `adb root`, если не видно);
  - `raw/30_per_package.txt` — построчная по 100 целевым пакетам:
    `pkg | ver | overlay-state`; state-метка `[x]/[ ]/---` ищется в строке
    (формат `cmd overlay list` варьируется по версиям Android); сравнение
    `PKG! ` с хвостовым пробелом — чтобы `fota` не совпало с `fotaservice`;
  - `raw/40_crash.txt` — logcat `-b crash -t 500`;
  - `raw/41_fatal.txt` — logcat `AndroidRuntime:E System.err:W`;
  - `raw/42_anr.txt` — `/data/anr` (нужен root);
  - `summary.txt` — сводка с авто-выводом «есть ли проблемы»
    (не установлено N / оверлеи выключены / FATAL). **Читают первым.**
  - zip: `logs\deepal_<scheme>_report_<TS>.zip` (powershell Compress-Archive,
    fallback tar; если оба нет — покажу папку `logs/report_<TS>/`).
  - Таймстемп: wmic → powershell Get-Date → `nods_%RANDOM%` (WMIC удалён
    в Windows 11 24H2).
- `manage.bat` — режим `report` (CLI `manage.bat report` / пункт [7] в меню)
  → `call collect_report.bat dynamic`.
- `manage_static.bat` — режим `report` (CLI `manage_static.bat report` /
  пункт [5] в меню) → `call collect_report.bat static`.

**Доки:** README (секция «Сбор логов для анализа (report)»), WORKFLOW
(новый Шаг 9 «Собрать логи для анализа», история 5.9), MEMORY.

## [2026-09-19a] — Короткие UI-строки: аудит ширины + режим `--fit` (борьба с переносом на 2 строки)

**Проблема:** RU-перевод заметно ШИРЕZH-оригинала (CJK-знак ≈ 2 знака ширины) →
в фиксированном виджете (кнопка/лейбл) переведённая строка переносится на
2 строки. RRO трогает только `string`, layout (maxLines/ellipsize/ширина)
не меняется — лечится укорочением перевода. По данным 18.09: **1067 строк /
71 приложение** (краткие UI: zh по display-ширине <= 14, RU > 2.5x ZH и >= 24
юнита; топ — WT_VehicleCenter=211, WT_FusionNavigation=142, WT_MultiMediaCenter=51).

**Код:**
- `strwidth.py` (новый) — общая метрика «ширины строки» (CJK/fullwidth = 2,
  остальное = 1) + пороги `FIT_*` (env-переопределяемые): `is_short_ui`,
  `is_ru_oversized`, `hard_cap` (= min(28, 2·zh_w + 8) символов). Единый
  источник правды для аудита и `--fit`.
- `validate.py` — секция **1c** (WARN, НЕ ошибка): «ширина RU vs ZH коротких
  UI-строк» — сводка + топ-15 приложений; `--length-report FILE` — полный
  список (`app/name | zh[w] | ru[w,cap] | zh | ru`), триаж совпадает с `--fit`.
- `improve_translations.py` — режим **`--fit`**: цель — короткие переширокие
  строки; **два лимита** последовательно: (1) мягкий — guard принимает ответ
  только если он КОРОЧЕ текущего по `display_width` И `len <= hard_cap`;
  (2) жёсткий — повторный запрос с `max_chars` в пайлоаде (cap в символах),
  guard — `len <= max_chars`. Не укоротилось — текущий `ru` НЕ затирается,
  строка в `logs/fit_stuck.txt` (ручное ревью). Плейсхолдеры/wake-word/CJK —
  как в основном guard. Свой прогресс `improve_fit_progress.json`: в done
  попадают только УКРОЧЕННЫЕ (повтор без `--fresh` не укорачивает повторно —
  нет риска переусечения). `build_fit_payload` (soft/hard), `send_fit_batch`,
  `guard(fit=, ru_current=)`.
- `promt.md` — раздел «Краткость коротких строк (UI-виджеты)»: термин вместо
  описания, без скобок и «функция/режим/система», смысл/плейсхолдеры — всегда.
- `test_improve_selftest.py` — +17 проверок (fit-guard: короче/не короче/
  >cap/плейсхолдеры/CJK/is_target, ширины, hard_cap, is_ru_oversized) → **59/59 OK**.

**Сделано:** ПОЛНЫЙ `--fit`-прогон завершён: **1058/1059 укорочено** (1 stuck —
`WT_FusionNavigation/guide_gps_low_subtitle`: модель трижды вернула пустой
content (ответ в `reasoning`), поправлено вручную «Выйдите на открытое
место» в cap 28). Перешироких **1067 → 3** (остаток у cap: Fota/upgrade_leave,
WT_Link/car_control_off_tip, WT_SystemService/alarm_text_arr_55 — 24w при
cap 24, модель короче не ужимает). Пересчёт против бэкапа: ровно 71
приложение / 1067 строк изменено (все укорочены, плейсхолдеры целы).
`validate.py` (full с aapt2) — 0 ошибок; overlays перегенерированы, **оба
набора APK пересобраны** (100+100, 0 FAIL). Остаток в `logs/fit_report_after.txt`.

> См. также [2026-09-19]: `manage_static.bat` (менеджер статических RRO) той же сессии.

## [2026-09-19] — `manage_static.bat`: менеджер СТАТИЧЕСКИХ RRO (двойник manage.bat)

Аналог `manage.bat` для статических оверлеев (`com.deepal.translate.rro.*`,
`apks_rro_static/`, `/vendor/overlay/`). Modes: `install` (adb push +
reboot) | `uninstall` (adb rm -rf + reboot) | `status` | `diag`. Presets та же
сетка (1/2/3/4/8/A/C/auto) + CLI `manage_static.bat <mode> <preset>`.
`ROOT_CHECK` (adb root + remount /vendor) — только до install/uninstall;
`ADB_CHECK` (подключение) — общий, status/diag работают без root. enable/
disable убраны (статика вкл/выкл только через удаление/добавление overlay).
После install/uninstall — подсказка `adb reboot`.

## [2026-09-18e] — Чистка мусора (по утверждённому отчёту)

**Удалено:**
- `__pycache__/` (312K; 18 `.pyc`, из них 6 — от удалённых 18.09 translate_*),
  `scripts/__pycache__/` (40K, вкл. `translate_batch.cpython-312.pyc`);
- `rro_builds/` — пустая директория (артефакт старых экспериментов);
- `scripts/logs/` — лог удалённого `translate_batch.py`.

**Удалено (устаревшие снапшоты; история осталась в `CHANGELOG.md`):**
- `NEW_SESSION.md` (статус 06.09: 111 app, 89%),
- `PROJECT_DATA.md` (сводка 07.09: ~16 900 строк, 0%),
- `REVIEW.md` (ревью 17.09; п.1–5 решено, п.6–9 устарели — скрипты удалены),
- `TRANSLATE_TARGETS.md` (план 18 приложений от 10.09 — всё переведено 100%).

**Перенесено:** `RRO-overlay-howto.md` → `docs/RRO-overlay-howto.md`
(единственная ссыка — комментарий в `create_rro_min.py`).

После чистки в корне проекта только: рабочие скрипты (*.py), bat-менеджеры,
`*.md`-док (`README`/`MEMORY`/`TODO`/`WORKFLOW`/`CHANGELOG`/`promt`),
`docs/`, `keys/`, `logs/`, `database/`, data-каталоги.

## [2026-09-18d] — Ревью bat динамических RRO: весь функционал сведён в manage.bat

**Ревью** (install_8 / install_all / disable_8 / manage / diag):
- `manage.bat` уже имел install/enable/disable/uninstall/status по группам,
  но не хватало: TOP-8-пресета, пресета `C` (2+3) из `install_all.bat`,
  «auto» (авто-список всех `*_RRO.apk`), счётчиков/сводки (Всего/OK/Ошибок/
  Не найдено), `pause`-возврата в меню, и diag-функционала `diag.bat`.
- Дублирование: `install_8.bat` и `disable_8.bat` тащили **хардкод-таблицу**
  `if %%A==...` → `OVERLAY_PKG` для 8 названий, хотя пакет вычисляется из
  lower-case имени (уже было в `install_all`/`manage` в 26 строках `set /i`).
- `install_all.bat` не имел пресета TOP-8; `diag.bat` дублировал adb-проверку.

**Консолидация — `manage.bat` теперь ЕДИНСТВЕНННЫЙ менеджер динамических RRO:**
- CLI: `manage.bat <mode> <preset>` (без аргументов — интерактивное меню).
- modes: `install` (copy в `install_ready/` + `adb install -r` + enable) |
  `enable` | `disable` | `uninstall` (disable-сначала, потом adb uninstall) |
  `status` | `diag`.
- presets: `1` `2` `3` `4` (сохранённые группы) | **`8` TOP-8** (то, что раньше
  было в install_8/disable_8) | `A` все | `C` 2+3 | **`auto`** — все
  `*_RRO.apk` из `apks_rro_min/` (имена без суффикса `_RRO`); `0`/Enter — назад.
- `status`: `overlay list` (все или фильтр `com.android.vendor`); `diag`:
  `system_locales` + наши оверлеи + `overlay dump <пакет>` + `dumpsys
  package <app> | findstr overlay` (пакеты вводятся, пропуск по Enter) —
  вместо хардкода AdayoAPA как в старом diag.bat.
- Счётчики `TOTAL/OK/FAIL/MISS` + сводка после install/enable/disable/uninstall;
  auto-создание `install_ready/`; статус «(пакет не установлен?)» при
  enable/disable ошибки.
- Имена копий сохранены: `install_ready/<App>_Overlay_ru_signed.apk` (для
  совместимости с device-side сценариями).
- Пакет вычисляется только через lower-case (26 `set` в `:Lower`) — хардкод
  8-элементной таблицы убран.
- ADB-проверка одна, в начале (не дублируется в каждом скрипте).

**Обёртки** (один `call "%~dp0manage.bat" <mode> <preset>`):
- `install_8.bat`  → `manage.bat install 8`
- `install_all.bat`→ `manage.bat install A`
- `disable_8.bat`  → `manage.bat disable 8`
- `diag.bat`       → `manage.bat diag`

Статические оверлеи (`/vendor/overlay/`) — отдельная схема, bat статик не
трогали (`install_static_8/36/all`, `uninstall_all_static`).

Проверка: перебором/чтение .bat (goto-цепочки, delayed expansion, параметры
функций, escape `^[`/`^]`, пути с кавычками) — логика выверена; на живой
машине с adb запустить `manage.bat` (меню) и `manage.bat diag`.
Новые/изменённые bat приведены к CRLF (Windows); остальные bat в репо
оставлены как были (LF — работали).

## [2026-09-18c] — improve_translations.py: CJK-ремонт длинных + добиты ВСЕ строки (100%)

Гипотеза «модель перевела, но guard зарезал» подтвердилась частично: модель
ДОСЫЛАЛА нормальный перевод (34 700 симв.), но оставляла 5–6 китайских
фрагментов посреди текста (你应及时, 词条, 展示, 服务协议…). Старый механизм
(полный повтор) не спасал: 5+ минут на один повтор, при этом роняло ДРУГИЕ
фрагменты — готовое 30к-перевод терялось.

**Решение — точечный CJK-ремонт (`_repair_cjk`), без регенерации всего текста:**
- Для каждого CJK-фрагмента в готовом переводе вырезается русское **«окно»
  (±50 символов)**, отправляется модели одним маленьким запросом; модель
  возвращает окно ПОЛНОСТЬЮ исправленным (переводит толькоfragment у границ
  слов/пробелов), окно подставляется обратно в исходный текст (`str.replace`
  по полному окну — нет сдвига, нет потери контекста);
- `REPAIR_MAX_RUNS = 50` (больше — ответ развалился, ремонт не поможет,
  уходит в полный повтор), `REPAIR_CONTEXT = 50`;
- **Lenient-парсер `_parse_repair_response`**: модель часто «портит»
  JSON-ответ из-за живых `\n`/битых escape в 100-символьных окнах.
  Если `json.loads` падает — скан с учётом backslash-экранирования
  (невалидный escape читается как сам символ). 3 попытки на bad-JSON.
  Формат `{n, window_ru}` (n — порядковый номер фрагмента) — порядок/набор
  окон проверяется перед подстановкой, несовпадение → возврат None (не
  рисковать сменой мест).
- Интеграция: в `send_batch` после placeholder-hint, перед полным
  CJK-повтором. Если ремонт вернул 0 CJK → guard → принять. Если не сработал →
  полный повтор с hint (как раньше).
- Новый тест-блок `test_parse_repair` (clean/с живым \n/невалидный escape/
  markdown-fence/пустой массив/мусор). Self-test 37 → **42/42**.

**Результат:** ` WT_FusionNavigation/road_book_my_agreement_two_content`
(9 273 симв.) переведена через CJK-ремонт (6 фрагментов), `ru` = **33 152**
симв. Все 3 длинных юридических документа переведены:
- `WT_GameCenter/str_procotol` (10 774) → 38 949
- `WT_FusionNavigation/road_book_my_agreement_one_content` (4 884) → 17 208
- `WT_FusionNavigation/road_book_my_agreement_two_content` (9 273) → 33 152

**ИТОГОВОЕ СОСТОЯНИЕ (18.09):**
- `generate_summary.py`: **17 827/17 827 (100.0%)**, 100/100 приложений.
- APK: **100 dynamic + 100 static**, 0 FAIL. `validate.py` (с aapt2 dump):
  **0 ошибок**. Self-test **42/42**. Прогресс-файл полный.

## [2026-09-18b] — improve_translations.py: длинные строки по одной + починка `"None"` + защита progress

`improve_translations.py --all` отработал (принято ≈17 729, guard 2, не
переведено 16). Из 16 реально не переведлись 3 **длинных** юридических
документа (ст. соглашения 10 774 / 9 273 / 4 884 симв.); остальные 13 —
субъекты их батчей, которые модель роняла вместе с длинной. Причина — две.

**1. Батчинг: длинная строка роняла весь пакет.** При `API_BATCH_SIZE=8` одна
очень длинная строка в пакете переполняла/обрезала ответ модели → весь батч
терялся (и короткие тоже). Теперь длинные идут **по одной**:
- `API_LONG_THRESHOLD` (по умолч. 200) — длина zh, с которой строка
  отправляется соло;
- `_make_chunks()` — длинные → батч из 1, короткие — группами по `BATCH_SIZE`
  (порядок сохраняется);
- новый режим **`--long`**: только длинные и дефектные; **обходит
  progress-фильтр** (упавшее уже в `done` снова в очереди); в логе помечено `[LONG]`.

**2. 🔴 Реальная причина `"None"` в данных.** Модель не справилась с
длинной строкой и возвращала `ru: null` в JSON, а код делал
`str(res.get("ru", ""))` — ключ есть → `None` → `str(None) = "None"` → guard
принимал как валидный перевод. Три больших строки так ни разу и не переведлись, а в `ru` сохранилось
буквально слово `None` (ГУ показало бы «None»). Починено:
`res.get("ru") or ""` (`or ""` ловит и `null`).

**Guard усилен** (длинный ответ не записывается «пустым»):
- `DEGENERATE_ANSWERS = {none, null, undefined, n/a, nil}` → отклоняются.
  **ВАЖНО**: `нет`/`неизвестно` НЕ в списке — это легитимные переводы
  `无/没有/未知` (было 32 корректные записи с `нет`/`неизвестно`);
- `LONG_RATIO_MIN = 0.25`: для `len(zh)>=LONG_THRESHOLD` требуется
  `len(ru) >= 0.25·len(zh)` (обрезанный/сброшенный ответ отклоняется);
  коротким строкам проверки не мешают;
- `--defective` и `--long` теперь ловят: пустой ru / CJK в ru / битые
  плейсхолдеры / дегенеративные `None` на длинных / слишком короткий ответ
  на длинном — через `_ru_defective()`;
- в `send_batch`, помимо placeholder-hint, добавлен **hint на CJK-остатки**
  («переведи каждое оставшееся китайское слово, CJK=0») для длинных документов.

**🔴 Починка обнуления progress.** Мой `--long` (без `--resume`) сбил
`improve_progress.json` с ~17 729 записей до 1: `run()` всегда делал
`done = set()` (когда не `--resume`) и `save_progress(done)` — перезаписал
накопленный прогресс. Теперь для не-`--all` режимов `done = load_progress()`
(merge-база) и основной прогресс не теряется; для `--all` — исходная
семантика. Проверено эмуляцией: повторный `--long` даёт `done 17746→17747`,
не стирает. Прогресс восстановлен: **17 746 done**, `road_book_my_agreement_two_content`
вынесен из `done` (остаётся переделываемым на `--long`).

**Результат по 3 длинным:**
- `WT_GameCenter/str_procotol` (10 774) → **38 949** симв. ✅
- `WT_FusionNavigation/road_book_my_agreement_one_content` (4 884) → **17 208** ✅
- `WT_FusionNavigation/road_book_my_agreement_two_content` (9 273) — модель в
  5 попытках каждый раз оставляет 5–21 китайских фрагмента в середине
  (качество модели на этом тексте, а не сборка). `ru` сброшен в пустое
  (ГУ потом покажет оригинальный китайский, а не «None»), строка в очереди на
  `--long`. Догнать: `API_KEY=ollama API_MODEL=qwen3.8:27b python3 improve_translations.py --long`.

**Проверки:** `test_improve_selftest.py` 33 → **37/37** (добавлены: дегенеративный
`None`, слишком-короткий для длинного, длинный корректный принят); обе сборки
`100/100 OK, 0 FAIL` (с гигантскими `<string>` — `aapt2` их компилирует,
`WT_FusionNavigation_RRO.apk` 249 K); `validate.py` (включая `aapt2 dump`)
**0 ошибок**. `generate_summary.py`: 17 826/17 827 (100.0%), 99/100 app,
оставшаяся — та одна длинная строка.

## [2026-09-18] — Убран исторический путь translate + синхронизация документации

**Удалены скрипты** (исторический пакетный путь переводов; единственный путь
сейчас — `improve_translations.py`):
- `translate_one.py`, `translate_batch.py` — пакетный Ollama (ornith:35b-bf16);
- `api_translate_ambiguity.py`, `scan_ambiguities.py`, `apply_db_new_ru.py`,
  `revert_cjk_translations.py` — путь неопределённостей `ambiguity_db.json`
  (сама ДБ в проекте отсутствовала);
- `run_pipeline.sh` (зависел от удалённых).

**Добавлены прогонщики:**
- `create_app_pipeline.sh` — полный цикл: create_db → generate_overlays(±static) → create_rro_*;
- `export-to-db.sh` — create_db + validate (+ dry-run apply_db при необходимости).

**Синхронизация документации** с фактическим составом (README / MEMORY / TODO /
WORKFLOW / это CHANGELOG): вычищены ссылки на удалённые скрипты и ornith,
статус обновлён (≈55% improve-прогона, 0 ошибок validate). `REVIEW.md` —
исторический документ, п.6-9 устарели вместе с удалёнными скриптами.
`NEW_SESSION.md` / `PROJECT_DATA.md` — процессные снапшоты (архивы, не source
of truth — актуальный статус в README/MEMORY).

Текущие номера: 100 app / ≈17 827 записей / перевод 100% / APK 100+100 /
validate 0 ошибок; `improve_translations.py --all` идёт фоном
(≈55%, `translations/improve_progress.json`).

## [2026-09-17d] — create_db.py / apply_db_translations.py: аудит и починка (без запуска на данных)

Аудит в песочнице (боевые данные и идущий LLM-прогон не трогались).

**create_db.py:**
- 🔴 `--find` / `--find-ru` падали с `NameError: condition` (строка 131) — починено,
  оба режима работают; дедупликация условий и параметров.
- `build_db`: атомарная транзакция (BEGIN/commit/rollback), `UNIQUE(app_name,name)`
  + `INSERT OR REPLACE`, `mkdir` каталога базы (без него падало на чистой машине).
- `--stats`: `hit_col` для both-ветки.

**apply_db_translations.py:**
- 🔴 Старый матч `WHERE zh = ?` БЕЗ app/name + `ORDER BY app_name LIMIT 50` →
  кросс-апп загрязнение (пустому `ru` в app-B подставлялся перевод app-A на то же zh).
  Новая логика `_resolve_translation`:
  1) `(app_name, name)` → 2) `(app_name, zh)` → 3) кросс-апп по zh
  **только при единственном уникальном** кандидате; иначе `ambiguous` (не применять).
- Add guard: CJK в ru и паритет форматтеров (как в improve/validate) —
  кандидатов с битыми `%s`/`%%` или CJK не подставляет.
- Атомарная запись JSON (tmp + os.replace, как в improve_translations).
- `--all` исключает `index/extraction/ambiguity_db`; `--stats` защищён от total=0.

**validate.py:**
- Новый чек «дубликаты name внутри приложения» (ломают UNIQUE в БД, aapt2
  duplicate resource и матчинг apply). В текущих данных: 0 дубликатов.
- После починки `specs()`: 0 ошибок (проверено без запуска на боевом).

Все правки проверены в /tmp-песочницах на синтетике: изоляция app, unique
cross-app, ambiguous-skip, guard, атомарность, `--find`/`--find-ru`, mkdir-база.

## [2026-09-17c] — improve_translations.py + promt.md + починка specs()

- **Критичная починка `specs()`** в `validate.py` И (новом) `improve_translations.py`:
  regex был `[fd]` — **не ловил `%s` и `%%`**; плюс список сортировался (гасил
  перестановку `%1$s`↔`%2$s`). Это означало, что ранний «0 ошибок форматтеров»
  был частично ложным. Теперь: `%(\d+\$)?(\.?\d*)([dfs])|%%`, упорядоченно по
  вхождению. Новые честные ловы: `WT_Album/accept_or_refuse_file_from_phone`
  (%s/%d переставлены), `WT_TSpeech/agent_do_not_find_surround_keyword`
  (%1$s/%2$s), 5×`WT_TinnoveSmartScene` (потерянные `%%`).
- **`improve_translations.py`** (новый; копия логики api_translate_ambiguity.py
  с изменённым входом/выходом): читает `translations/*.json`, пишет в `ru`
  той же записи (no new_ru, no отдельная ДБ), прогресс — `improve_progress.json`
  (resume/fresh), атомарная запись JSON (tmp+rename), save после каждого батча.
  Modes: `--all` / `--defective` / `--empty` / `--app` / `--limit` / `--dry-run`.
- **`promt.md`** (новый): системный системный промпт для улучшения переводов —
  жёсткие правила по плейсхолдерам (паритет + порядок), HTML-разметке
  (сущности→сущности, raw→raw, запрет double-escape и «перекрёстной» экранировки),
  wake-word `***你好***` не переводить, глоссарий.
- **Guard перед записью** (ru не затирается на сбой): empty; formatters-parity;
  wake-word обязателен; CJK отклоняется (кроме whitelist).
- **Нормализация** (наследование _sanitize_new_ru): double-escape → single → raw;
  `'`→`’` U+2019 (aapt2 отклоняет `'` и `&apos;`); живые \n/\t → литеральные.
- **Подтверждено энд-ту-энд**: 7 дефектных строк (2 реально после финального
  regex) → 2 исправлены моделью (1 pass + hint-повтор на %1$s/%2$s), `validate.py`
  0 ошибок, обе сборки 100/100 OK (2914 + 2926 KB).
- **`test_improve_selftest.py`** (новый, 33/33): guard, specs, wake-word regex,
  и полный round-trip «значение ru → `fmt_string_xml`/`_esc` → aapt2 compile+link
  → `aapt2 dump`, побайтовое сравнение) — подтверждает корректность экранирования
  HTML-тегов через реальную сборку, не только в JSON.

## [2026-09-17b] — Починены 5 багов данных + автопроверка (по REVIEW.md)

- **Префикс пакета**: первоначально сочли конфликт (динамические `com.android.vendor.*`
  против статических `com.deepal.*`) и унифицировали на `com.deepal.*`. ПОТОМ выяснилось,
  что это ошибка: bat динамической схемы (`install_8/all`, `disable_8`, `manage`, `diag`)
  как раз зашиты под `com.android.vendor.*`, а префикс `com.android.*` намеренно скрывает
  oверлеи из лаунчера (LAUNCHER_APP_HIDDEN_DESIGN.md, фильтр №1). **Откат** префикса:
  динамические снова `com.android.vendor.translate.rro.*`, статические —
  `com.deepal.translate.rro.*` (как было). `validate.py` проверяет префикс по схеме.
- **Динамические оверлеи**: `res/values/` → `res/values-ru/` (перевод привязан к русской
  локали, а не ко всем); `create_rro_min.py` компилирует `values*`.
- **string-arrays**: `extract_cjk.py` скидывал пустые `<item></item>` → сдвиг индексов
  (wifi_status и ещё 4 массива в CarService/WT_VehicleCenter). Теперь нумерация по
  исходным позициям; старые JSON патчены (5 массивов). `generate_overlays*` выдают
  элементы 0..N-1 непрерывно (пустые — как пустые).
- **plurals**: пустые `few`/`many` более не генерируются — берётся копия `other`
  (было: пустое «2-4»/«5+» на 88+ приложениях).
- **Данные**: исправлены 12 строк (`fix_placeholders.py`): `%S`→`%s`, проглоченные
  `%s`, `` `s ``→`"%s`, CJK-хвосты в 3 строках WT_TSpeech. Wake-word
  `WT_VehicleCenter/Exterior_voice_interaction_title` (你好) намеренно НЕ переведён —
  это голосовая команда, переведена в whitelist.
- **`validate.py`** (новая автопроверка, exit 1 при ошибках):
  1) форматтеры zh↔ru (parity), 2) CJK-in-ru (с whitelistом), 3) непрерывность индексов
  и размеры array против `decompiled/`, 4) plurals few/many не пустые,
  5) `aapt2 dump resources` APK: package prefix, размеры, CJK.
  Итог после реборда: **100/100 + 100/100 OK, 0 ОШИБОК** (17 827 строк, 151 массив,
  63 plurals, 200 APK проверено).
- Прогресс/aux JSON (`*_progress.json`, `ambiguity_db.json`) исключены из генераторов
  оверлеев (латентный баг).

## [2026-09-17] — apply_db_new_ru + нормализация перевода (aapt2-safe)
- **apply_db_new_ru.py** починён перенос `new_ru` → `ru`.
  - **Баг матча**: `file` из DB (`translations/AdayoAPA.json`) не равнялся `json_path.name` (`AdayoAPA.json`) → `match` всегда `None` → применялось 0 записей. Теперь сравнение по всем формам: basename, `translations/файл`, stem.
  - **Баг `decode_new_ru`**: `json.loads` падал на raw-управляющих символах (переводы строк) → `strict=False` + фолбэк при разворачивании обёртки.
  - **Баг `NameError`**: `args.new_ru_only` внутри `apply_new_ru` → заменён на параметр `new_ru_only`.
  - Итог dry-run: 908 записей / 905 with new_ru / 906 translated.
- **api_translate_ambiguity.py**: добавлена `_sanitize_new_ru()` + вызов в save-шаге (сразу после нового `new_ru`).
  - Нормализует **двойное HTML-экранирование** к одинарному (&amp;, &quot;, &gt;, &lt;, &apos; → символы), чтобы `_esc()` в generate_overlays.png сделал escap **один раз** (aapt2 Rejects `&amp;lt;`).
  - Апострофы → ‘ (U+2019, aapt2 не принимает `'`, `&apos;`, `&#x27;`).
  - **Не трогает** `%s`, `%1$d`, `%`, `\`, URL, «» “” — всё валидно после одноразового esc.
- **Pipeline** (generate_overlays.py → create_rro_min.py): Applied **458 записей**, skipped 211. Сборка **100/100 OK, 0 FAIL** (был: WT_MultiMediaCenter compile FAIL).
  - В previously-failing `dialog_privacy_policy_msg_start_up` теперь `&lt;a href='...'&gt;` (+ ‘ вместо '), compile + link APK OK (72 KB).

## [2026-09-07] — Извлечение + статус
- **extract_cjk.py** исправлен: regex `[^/>]*` → `[^/>]*?` (жидающий), извлечение работает
- 103 декомпилированных APK, 92 с CJK-строками
- **114 JSON-файлов** (включая прогресс-вехи для AdayoDvrLocalService, CertInstaller, DownloadProvider)
- **~16900 CJK-строк** извлечено, **0%** переведено
- 11 APK пропущено (нет `values/strings.xml`)
- **PROJECT_DATA.md** — полная сводка данных проекта

## [2025-09-05] — Full extraction + translation
- **extract_cjk.py** rewritten: now extracts strings/arrays/plurals from all locales (zh/en/ru)
- **translate.py** rewritten: cross-source optimization, batch mode, incremental saves
- 111 apps processed, 25990 entries extracted (zh + en + ru fields)
- AdayoAPA: 181/181 CJK translated (100%) — full test confirmed
- Fixed: `array_idx=0` after `<item>` tag reset, RTL/zero-width Unicode cleanup
- **generate_summary.py** updated: shows zh/en/ru counts per app
