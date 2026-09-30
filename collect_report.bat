@echo off
setlocal EnableDelayedExpansion
chcp 65001 >nul
title Deepal HU Translate - Report Collector

REM ============================================================
REM  collect_report.bat — сборка диагностического пакета (zip) для
REM  отправки на анализ. ВЫЗЫВАЕТСЯ из manage.bat / manage_static.bat:
REM
REM      call collect_report.bat dynamic
REM      call collect_report.bat static
REM
REM  Параметры (наследуются из вызывающего bat, те же переменные):
REM    SCHEME       : dynamic | static (первый аргумент)
REM    PREFIX       : com.android.vendor.translate.rro. / com.deepal...
REM    FILTER       : подстрока для pm list (наши пакеты)
REM    APK_DIR      : папка APK на хосте (что планировалось ставить)
REM    OVERLAY_BASE : /vendor/overlay (только static)
REM    GROUP1..4    : 100 целевых приложений
REM
REM  Что собирается (ТОЛЬКО ЧТЕНИЕ, ничего не удаляем/не меняем):
REM    - какие из 100 пакетов УСТАНОВЛЕНЫ (pm list + versionCode), ОТСУТСТВУЮТ
REM    - state оверлеев: [x] включен / [ ] выключен / --- ошибка
REM    - locale системы
REM    - /vendor/overlay (static; сам запросит adb root, если не видно)
REM    - сбои: logcat crash buffer, FATAL EXCEPTION, /data/anr
REM    - summary.txt с авто-выводом «есть ли проблемы»
REM
REM  Результат (это отправить на анализ):
REM    logs\deepal_<dynamic|static>_report_<TS>.zip
REM  Внутри:
REM    report_<TS>\summary.txt        - читать первым
REM    report_<TS>\raw\*.txt          - сырые дампы
REM ============================================================

set "SCHEME=%~1"
if /i not "!SCHEME!"=="dynamic" if /i not "!SCHEME!"=="static" (
    echo ERROR: collect_report.bat dynamic^|static - пустой scheme
    pause
    exit /b 1
)
if not defined FILTER set "FILTER=com.vendor"
if not defined APK_DIR set "APK_DIR=apks_rro_min"
if /i "!SCHEME!"=="static" if not defined OVERLAY_BASE set "OVERLAY_BASE=/vendor/overlay"
if not defined GROUP1 if not defined GROUP2 if not defined GROUP3 (
    echo ERROR: не переданы GROUP1..4 из manage.bat
    pause
    exit /b 1
)
set "ALL_TARGETS=%GROUP1% %GROUP2% %GROUP3% %GROUP4%"

REM ---------- путь/имя ----------
set "LSC=!SCHEME!"
call :CRLow "!SCHEME!"
set "LSC=!CLOW!"
set "LOGROOT=%~dp0logs"
if not exist "!LOGROOT!" mkdir "!LOGROOT!"
REM таймстемп: wmic (Win<=23H2) -> powershell -> date (фолбэк без времени)
set "TS="
set "DT="
for /f "tokens=2 delims==" %%i in ('wmic os get localdatetime /value 2^>nul') do set "DT=%%i"
if defined DT (
    set "TS=!DT:~0,4!-!DT:~4,2!-!DT:~6,2!_!DT:~8,2!!DT:~10,2!!DT:~12,2!"
)
if not defined TS (
    for /f "delims=" %%i in ('powershell -NoProfile -Command "Get-Date -Format yyyyMMdd_HHmmss" 2^>nul') do set "TS=%%i"
    if defined TS (
        REM yyyyMMdd_HHmmss -> yyyy-MM-dd_HHmmss
        set "TS=!TS:~0,4!-!TS:~4,2!-!TS:~6,2!_!TS:~9,6!"
    )
)
if not defined TS set "TS=nods_%RANDOM%"
set "RDIR=!LOGROOT!\report_!TS!"
set "RDIR_RAW=!RDIR!\raw"
if not exist "!RDIR_RAW!" mkdir "!RDIR_RAW!"

set "F_DEV=!RDIR_RAW!\00_device.txt"
set "F_ADB=!RDIR_RAW!\01_adb_host.txt"
set "F_APK_LOCAL=!RDIR_RAW!\02_apk_local.txt"
set "F_LOCALE=!RDIR_RAW!\10_locale.txt"
set "F_PKGS=!RDIR_RAW!\20_packages_ours.txt"
set "F_OVL_ALL=!RDIR_RAW!\25_overlay_all.txt"
set "F_VENDOR=!RDIR_RAW!\26_vendor_overlay.txt"
set "F_PERPKG=!RDIR_RAW!\30_per_package.txt"
set "F_CRASH=!RDIR_RAW!\40_crash.txt"
set "F_FATAL=!RDIR_RAW!\41_fatal.txt"
set "F_ANR=!RDIR_RAW!\42_anr.txt"
set "F_MAIN=!RDIR_RAW!\43_main.txt"
set "F_OVLC=!RDIR_RAW!\44_overlay_main.txt"
set "F_OLDE=!RDIR_RAW!\45_overlay_dump.txt"
set "F_IDMP=!RDIR_RAW!\46_idmap.txt"
set "F_SUMMARY=!RDIR!\summary.txt"

echo ============================================================
echo   Deepal HU Translate - Report ^(!SCHEME!^)
echo   Каталог: !RDIR!
echo ============================================================
echo.

REM ---------- [1/9] устройство ----------
echo [1/9] Сведения об устройстве...
adb shell getprop > "!F_DEV!" 2>&1
(
    echo.
    echo ---- uptime ----
    adb shell uptime
    echo.
    echo ---- df -h ----
    adb shell df -h
) >> "!F_DEV!" 2>&1
(
    echo ==== scheme: !SCHEME! ====
    echo ==== timestamp: !TS! ====
    echo.
    echo ==== adb version - хост ====
    adb version
) > "!F_ADB!" 2>&1

REM ---------- [2/9] APK на хосте ----------
echo [2/9] APK-файлы на хосте: %APK_DIR% ...
if exist "%APK_DIR%" (
    dir /O-D "%APK_DIR%\*_RRO.apk" > "!F_APK_LOCAL!" 2>&1
) else (
    echo Каталог %APK_DIR% на хосте не найден > "!F_APK_LOCAL!"
)

REM ---------- [3/9] локаль ----------
echo [3/9] Локаль системы...
(
    echo ---- system_locales ----
    adb shell settings get system system_locales
    echo ---- ro.product.locale ----
    adb shell getprop ro.product.locale
    echo ---- persist.sys.locale ----
    adb shell getprop persist.sys.locale
) > "!F_LOCALE!" 2>&1

REM ---------- [4/9] pm list + overlay list ----------
echo [4/9] Установленные пакеты + list оверлеев...
adb shell pm list packages --show-versioncode "!FILTER!" > "!F_PKGS!" 2>&1
adb shell cmd overlay list --user 0 > "!F_OVL_ALL!" 2>&1

REM ---------- [5/9] /vendor/overlay для static ----------
set "N_VENDOR_DIRS=0"
if /i "!SCHEME!"=="static" (
    echo [5/9] Наши статические оверлеи на устройстве ^(root запрошу сам^)...
    for /f "delims=" %%d in ('adb shell "ls -d !OVERLAY_BASE!/!PREFIX!* 2>/dev/null"') do set /a N_VENDOR_DIRS+=1
    if !N_VENDOR_DIRS! EQU 0 (
        echo    не найдено без root - пробую adb root...
        adb root >nul 2>&1
        timeout /t 3 >nul
        for /f "delims=" %%d in ('adb shell "ls -d !OVERLAY_BASE!/!PREFIX!* 2>/dev/null"') do set /a N_VENDOR_DIRS+=1
    )
    (
        echo ==== наши папки: !N_VENDOR_DIRS! ====
        adb shell "ls -d !OVERLAY_BASE!/!PREFIX!* 2>/dev/null"
        echo.
        echo ==== содержимое: ls -l ====
        adb shell "ls -l !OVERLAY_BASE!/!PREFIX!*/ 2>/dev/null"
        echo.
        echo ==== весь /vendor/overlay ====
        adb shell "ls /vendor/overlay 2>/dev/null"
    ) > "!F_VENDOR!" 2>&1
) else (
    echo [5/9] Динамические - /vendor/overlay не используется
)

REM ---------- [6/9] построчная сводка ----------
echo [6/9] Построчная сводка по 100 целевым пакетам...
echo ============================================================ >  "!F_PERPKG!"
echo Scheme: !SCHEME!   Фильтр(pm list): !FILTER!               >> "!F_PERPKG!"
echo Формат: package ^| ver ^| overlay-state                     >> "!F_PERPKG!"
echo ============================================================ >> "!F_PERPKG!"
set "P_TOTAL=0"
set "P_ABS=0"
set "P_MISS=0"
set "ST_ON=0"
set "ST_OFF=0"
set "ST_ERR=0"
set "ST_NA=0"
set "OERRPKGS="
for %%A in (!ALL_TARGETS!) do (
    set /a P_TOTAL+=1
    call :CRLow "%%A"
    set "PKG=!PREFIX!!CLOW!"
    REM Строка pm list этого ГУ: package:PKG versionCode=N (ПРОБЕЛ, без "=") —
    REM проверено по живому репорту 22.09 (raw/20_packages_ours.txt). Граница —
    REM хвостовой пробел (отличает ...rro.fota от ...rro.fotaservice).
    REM Фолбэки: "=" (новые платформы) и без хвоста (версия не показана).
    set "VLINE="
    for /f "usebackq delims=" %%L in (`findstr /I /C:"package:!PKG! " "!F_PKGS!" 2^>nul`) do set "VLINE=%%L"
    if not defined VLINE for /f "usebackq delims=" %%L in (`findstr /I /C:"package:!PKG!=" "!F_PKGS!" 2^>nul`) do set "VLINE=%%L"
    set "VC=?"
    if defined VLINE (
        set /a P_ABS+=1
        REM Оба формата имеют ровно 2 двоеточия:
        REM   package:X versionCode:0  ->  token 3 = "0"
        REM   package:X=versionCode:0  ->  token 3 = "0"
        REM БЕЗ usebackq: in ("строка") = литерал-набор (не команда/файл).
        for /f "tokens=3 delims=:" %%t in ("!VLINE!") do set "VC=%%t"
    ) else (
        set /a P_MISS+=1
    )
    REM state оверлея: берём СТРОКУ с пакетом, затем в этой строке ищем
    REM маркер [x]/[ ]/---. Строка ищется в двух форматах:
    REM   a) "!PKG! " (хвостовой пробел) - новый формат cmd overlay list
    REM      ("  [x] com.pkg (com.base)"; граница пробелом отличает
    REM      ...rro.fota от ...rro.fotaservice);
    REM   b) пакет В КОНЦЕ строки (regex "!RE!$") - прежние версии Android,
    REM      точки экранируем, чтобы "." не была regex-«любой символ».
    set "OL="
    for /f "usebackq delims=" %%L in (`findstr /I /C:"!PKG! " "!F_OVL_ALL!" 2^>nul`) do set "OL=%%L"
    if not defined OL (
        set "OLRE=!PKG:.=\.!"
        for /f "usebackq delims=" %%L in (`findstr /I /R "^.*!OLRE!$" "!F_OVL_ALL!" 2^>nul`) do set "OL=%%L"
    )
    set "OST=?"
    if defined OL (
        echo !OL! | findstr /I /C:"---" >nul 2>&1
        if !ERRORLEVEL! EQU 0 (
            set "OST=---"
            set /a ST_ERR+=1
        )
        if "!OST!"=="?" (
            echo !OL! | findstr /I /C:"[x]" >nul 2>&1
            if !ERRORLEVEL! EQU 0 (
                set "OST=[x]"
                set /a ST_ON+=1
            )
        )
        if "!OST!"=="?" (
            echo !OL! | findstr /I /C:"[ ]" >nul 2>&1
            if !ERRORLEVEL! EQU 0 (
                set "OST=[ ]"
                set /a ST_OFF+=1
            )
        )
    )
    if "!OST!"=="?" set /a ST_NA+=1
    REM оверлеи в ошибке соберём для cmd overlay dump (шаг [8/9])
    if "!OST!"=="---" set "OERRPKGS=!OERRPKGS! !PKG!"
    if defined VLINE (
        echo   !PKG! ^| ver=!VC! ^| overlay=!OST! >> "!F_PERPKG!"
    ) else (
        echo   !PKG! ^| НЕТ ^| overlay=!OST! ^<=== НЕ УСТАНОВЛЕН >> "!F_PERPKG!"
    )
)
echo. >> "!F_PERPKG!"
echo Сводка: всего=!P_TOTAL! установлено=!P_ABS! отсутствует=!P_MISS! >> "!F_PERPKG!"
echo Overlay: [x]=!ST_ON! [ ]=!ST_OFF! ---=!ST_ERR! в списке нет=!ST_NA! >> "!F_PERPKG!"

REM ---------- [7/9] ошибки: crash, FATAL, ANR ----------
echo [7/9] Логи ошибок: crash buffer, FATAL, /data/anr...
adb logcat -d -b crash -t 500 -v time > "!F_CRASH!" 2>&1
adb logcat -d -s AndroidRuntime:E System.err:W "*:S" -v time > "!F_FATAL!" 2>&1
adb shell "ls -l /data/anr 2>/dev/null" > "!F_ANR!" 2>&1
set "N_ANR=0"
for /f "delims=" %%a in ('adb shell "ls /data/anr 2>/dev/null"') do set /a N_ANR+=1
if !N_ANR! EQU 0 (
    echo   /data/anr пусто или недоступен - пробую adb root... >> "!F_ANR!"
    adb root >nul 2>&1
    timeout /t 3 >nul
    adb shell "ls -l /data/anr 2>/dev/null" >> "!F_ANR!"
    set "N_ANR=0"
    for /f "delims=" %%a in ('adb shell "ls /data/anr 2>/dev/null"') do set /a N_ANR+=1
)
if !N_ANR! GTR 0 (
    REM anr_* лежат как -rw------- system system: пользователь shell НЕ
    REM может читать (ls каталога проходит, а head -> Permission denied;
    REM подтверждено репортом 22.09 21:26). adb root до дампа (на
    REM НЕ-руттируемом билде вернёт "cannot run as root" - безвредно).
    adb root >nul 2>&1
    timeout /t 3 >nul
    echo. >> "!F_ANR!"
    echo ==== содержимое последних 3 ANR ^(head -150^), adb root запрошен ==== >> "!F_ANR!"
    adb shell "for f in $(ls -t /data/anr 2>/dev/null | head -3); do echo ---- $f ----; head -150 /data/anr/$f 2>/dev/null || echo '   [не читается: Permission denied?]'; done" >> "!F_ANR!" 2>&1
)
REM Счётчик FATAL EXCEPTION. find /C НЕ годится: выводит "---------- N
REM C(S) FOUND" и строка localizes (RU: "НАЙДЕНО") - забирали токен 1 =
REM "----------" (дефисы); в summary было "----------   0 - всё чисто"
REM при 4 реальных FATAL (репорт 22.09 21:26), и if GTR 0 не срабатывал
REM (не-число). Тот же счётчик по строк, что работает у N_ANR (там 1).
set "N_FATAL=0"
for /f %%c in ('find /I "FATAL EXCEPTION" "!F_FATAL!" 2^>nul') do set /a N_FATAL+=1

REM ---------- [8/9] работа: main-лог (overlay/idmap/наши пакеты) + dump ошибочных оверлеев ----------
echo [8/9] Логи работы APK: logcat main (overlay/idmap/наши пакеты) + dump оверлеев в ошибке...
adb logcat -d -b main -t 2000 -v time > "!F_MAIN!" 2>&1
findstr /I /C:"overlay" /C:"idmap" /C:"!PREFIX!" "!F_MAIN!" > "!F_OVLC!" 2>nul
REM ВАЖНО: только плоские строки + goto, НЕ блоки if (...) (...): прежний
REM if/else рвался реальным Windows cmd на репорте 2026-09-28
REM (ошибка: " > "!F_IDMP!" was unexpected at this time. - REM-строки с
REM кавычками/«(тег IdmapManager), »/«(буфер 43_main ... install). » внутри
REM блока сбивали состояние парсера; wine cmd 9.0 прощается с тем же текстом
REM и НЕ является референсом для Windows cmd).
REM 46_idmap.txt пишется ТОЛЬКО при ошибочных оверлеях (старая семантика):
REM для STATE_NO_IDMAP и пр. IdmapManager пишет причину в полный logcat
REM этих тегов; буфер 43_main = 2000 последних строк, на шумном ГУ может
REM не дотянуть до момента install.
if "!OERRPKGS!"=="" goto :IDMP_SKIP
for %%P in (!OERRPKGS!) do (
    echo. >> "!F_OLDE!"
    echo ---------- %%P ---------- >> "!F_OLDE!"
    adb shell cmd overlay dump "%%P" >> "!F_OLDE!" 2>&1
)
echo ==== idmap: logcat IdmapManager/Idmap, полный -d ==== > "!F_IDMP!"
adb logcat -d -s IdmapManager:V Idmap:V -v time >> "!F_IDMP!" 2>&1
echo ==== grep наших пакетов в 43_main ==== >> "!F_IDMP!"
findstr /I /C:"!PREFIX!" /C:"idmap" "!F_MAIN!" >> "!F_IDMP!" 2>nul
goto :IDMP_DONE
:IDMP_SKIP
echo Оверлеев в ошибке ^(---^) нет - dump не требуется > "!F_OLDE!"
echo Оверлеев в ошибке нет > "!F_IDMP!"
:IDMP_DONE

REM ---------- [9/9] summary + логи операций в отчёт ----------
echo [9/9] Формирую summary.txt + логи операций (logs\ops_*.log)...
set "N_CPD=0"
for /f "delims=" %%N in ('dir /b /o-d "%LOGROOT%\ops_*.log" 2^>nul') do (
    if !N_CPD! GEQ 10 goto OPSLOGS_DONE
    copy /y "!LOGROOT!\%%N" "!RDIR!\%%N" >nul 2>&1
    set /a N_CPD+=1
)
:OPSLOGS_DONE
echo    логи операций (ops_*.log, последние) в отчёте: !N_CPD!

set "LOC_LINE=нет данных - см. raw\10_locale.txt"
for /f "delims=" %%L in ('adb shell settings get system system_locales 2^>nul') do set "LOC_LINE=%%L"
if not defined LOC_LINE set "LOC_LINE=пусто - см. raw\10_locale.txt"
if "!LOC_LINE!"=="null" set "LOC_LINE=пусто - см. raw\10_locale.txt"
set "MODEL_LINE="
for /f "usebackq delims=" %%L in (`findstr /I /C:"ro.product.model" "!F_DEV!" 2^>nul`) do set "MODEL_LINE=%%L"
set "REL_LINE="
for /f "usebackq delims=" %%L in (`findstr /I /C:"ro.build.version.release" "!F_DEV!" 2^>nul`) do set "REL_LINE=%%L"
set "INC_LINE="
for /f "usebackq delims=" %%L in (`findstr /I /C:"ro.build.fingerprint" "!F_DEV!" 2^>nul`) do set "INC_LINE=%%L"
REM дефолты, чтобы `echo  !INC_LINE!` (строка без литерала) при пустом значении
REM не превратилась в голый `echo` и не печатала "ECHO is OFF" в summary
if not defined MODEL_LINE set "MODEL_LINE=?"
if not defined REL_LINE   set "REL_LINE=?"
if not defined INC_LINE   set "INC_LINE=?"

echo ============================================================ >  "!F_SUMMARY!"
echo  Deepal HU Translate - Report ^(!SCHEME!^)                    >> "!F_SUMMARY!"
echo  Дата: !TS!                                                  >> "!F_SUMMARY!"
echo  Устройство: !MODEL_LINE!  /  !REL_LINE!                    >> "!F_SUMMARY!"
echo  !INC_LINE!                                                  >> "!F_SUMMARY!"
echo. >> "!F_SUMMARY!"
echo  Локаль: !LOC_LINE!                                          >> "!F_SUMMARY!"
if /i "!SCHEME!"=="static" (
    echo  Статических оверлеев в /vendor/overlay: !N_VENDOR_DIRS!  >> "!F_SUMMARY!"
)
echo. >> "!F_SUMMARY!"
echo  ---- УСТАНОВЛЕНО / НЕ УСТАНОВЛЕНО ----                     >> "!F_SUMMARY!"
echo  Всего целевых пакетов:   !P_TOTAL!                          >> "!F_SUMMARY!"
echo  Установлено:             !P_ABS!                            >> "!F_SUMMARY!"
if !P_MISS! EQU 0 (
    echo  ОТСУТСТВУЮТ:            0                                >> "!F_SUMMARY!"
) else (
    echo  ОТСУТСТВУЮТ:            !P_MISS!  ^<=== ПРОБЛЕМА         >> "!F_SUMMARY!"
    REM 9 AOSP-target (networkstack/providers/mtp/...) на этой сборке через
    REM adb install не ставятся (см. CHANGELOG 2026-09-22b) - если ЧИСЛО
    REM отсутствующих совпадает, это ожидаемо, а не свежий регресс.
    if !P_MISS! EQU 9 echo     (это 9 AOSP-target оверлеев - на этом ГУ они через adb install не ставятся; решение: /vendor/overlay или исключение из групп) >> "!F_SUMMARY!"
)
echo. >> "!F_SUMMARY!"
echo  ---- STATE ОВЕРЛЕЕВ ----                                    >> "!F_SUMMARY!"
echo  Включено [x]:   !ST_ON!                                     >> "!F_SUMMARY!"
echo  Выключено [ ]:  !ST_OFF!                                    >> "!F_SUMMARY!"
echo  Ошибка ---:     !ST_ERR!                                    >> "!F_SUMMARY!"
echo  Не в списке:    !ST_NA!                                     >> "!F_SUMMARY!"
echo. >> "!F_SUMMARY!"
echo  ---- СБОИ ----                                              >> "!F_SUMMARY!"
echo  FATAL EXCEPTION в logcat: !N_FATAL!   0 - всё чисто         >> "!F_SUMMARY!"
echo  ANR-файлов в /data/anr:   !N_ANR!                            >> "!F_SUMMARY!"
echo  Crash buffer: см. raw/40_crash.txt   ANR: raw/42_anr.txt    >> "!F_SUMMARY!"
echo. >> "!F_SUMMARY!"
echo  ---- ЛОГИ ОПЕРАЦИЙ (установка/работа) ----                  >> "!F_SUMMARY!"
echo  ops_*.log из logs\:    !N_CPD! файла ^- см. корень архива   >> "!F_SUMMARY!"
echo  raw/43_main.txt        logcat main -t 2000 (работа APK)     >> "!F_SUMMARY!"
echo  raw/44_overlay_main.txt main: строки overlay/idmap/наши     >> "!F_SUMMARY!"
if !ST_ERR! GTR 0 (
    echo  raw/45_overlay_dump.txt dump оверлеев в ошибке           >> "!F_SUMMARY!"
)
echo. >> "!F_SUMMARY!"
echo  ---- ВЫВОД ----                                             >> "!F_SUMMARY!"
set "VERDICT=ВСЕ В ПОРЯДКЕ: все пакеты на месте, оверлеи включены, падений нет."
if !P_MISS! GTR 0 set "VERDICT=ПРОБЛЕМА: !P_MISS! пакет(ов) НЕ УСТАНОВЛЕНО, см. raw\30_per_package.txt"
REM ровно 9 AOSP-target не ставится adb install на этом ГУ - это ожидаемо
REM (CHANGELOG 2026-09-22b), не регресс
if !P_MISS! EQU 9 set "VERDICT=ОЖИДАЕМО: 9 AOSP-target оверлеев не ставятся adb install (NetworkStack/Providers/…); остальные на месте"
if !ST_OFF! GTR 0 set "VERDICT=ПРОБЛЕМА: !ST_OFF! оверлей(ей) выключен, см. raw\30_per_package.txt"
if !ST_ERR! GTR 0 set "VERDICT=ПРОБЛЕМА: !ST_ERR! оверлей(ей) в ошибке ---, см. raw\45_overlay_dump.txt"
if !N_FATAL! GTR 0 set "VERDICT=ПРОБЛЕМА: найдено FATAL EXCEPTION: !N_FATAL!, см. raw\40_crash.txt"
if !N_ANR! GTR 0 set "VERDICT=ПРОБЛЕМА: найдено ANR: !N_ANR!, см. raw\42_anr.txt"
echo  !VERDICT! >> "!F_SUMMARY!"
echo. >> "!F_SUMMARY!"
echo  ---- СОДЕРЖИМОЕ ----                                        >> "!F_SUMMARY!"
echo  summary.txt              этот файл                          >> "!F_SUMMARY!"
echo  ops_*.log                логи установки/операций (последние) >> "!F_SUMMARY!"
echo  raw/00_device.txt        getprop + uptime + df              >> "!F_SUMMARY!"
echo  raw/01_adb_host.txt      timestamp + adb version            >> "!F_SUMMARY!"
echo  raw/02_apk_local.txt     APK на хосте, что ставилось        >> "!F_SUMMARY!"
echo  raw/10_locale.txt        локаль                             >> "!F_SUMMARY!"
echo  raw/20_packages_ours.txt pm list наших пакетов + versionCode >> "!F_SUMMARY!"
echo  raw/25_overlay_all.txt   cmd overlay list --user 0 ^- весь  >> "!F_SUMMARY!"
if /i "!SCHEME!"=="static" echo  raw/26_vendor_overlay.txt  /vendor/overlay - ls -l       >> "!F_SUMMARY!"
echo  raw/30_per_package.txt   построчная сводка 100 пакетов      >> "!F_SUMMARY!"
echo  raw/40_crash.txt         logcat -b crash -t 500             >> "!F_SUMMARY!"
echo  raw/41_fatal.txt         AndroidRuntime:E из main           >> "!F_SUMMARY!"
echo  raw/42_anr.txt           /data/anr + trace (root)           >> "!F_SUMMARY!"
echo  raw/43_main.txt          logcat main -t 2000                >> "!F_SUMMARY!"
echo  raw/44_overlay_main.txt  main: overlay/idmap/наши пакеты    >> "!F_SUMMARY!"
echo  raw/45_overlay_dump.txt  cmd overlay dump ошибочных оверлеев >> "!F_SUMMARY!"
echo  raw/46_idmap.txt         logcat IdmapManager/Idmap (полный) >> "!F_SUMMARY!"
echo ============================================================ >> "!F_SUMMARY!"

echo.
type "!F_SUMMARY!"
echo.

REM ---------- zip ----------
set "ZIPBASE=deepal_!LSC!_report_!TS!.zip"
set "ZIPLOG=!LOGROOT!\!ZIPBASE!"
set "ZOK=0"
where powershell >nul 2>&1
if !ERRORLEVEL! EQU 0 (
    pushd "!LOGROOT!" >nul
    powershell -NoProfile -ExecutionPolicy Bypass -Command "Compress-Archive -LiteralPath 'report_!TS!' -DestinationPath '!ZIPBASE!' -Force" >nul 2>&1
    popd >nul
    if exist "!ZIPLOG!" set "ZOK=1"
)
if not defined ZOK (
    where tar >nul 2>&1
    if !ERRORLEVEL! EQU 0 (
        pushd "!LOGROOT!" >nul
        tar -a -c -f "!ZIPBASE!" "report_!TS!" >nul 2>&1
        popd >nul
        if exist "!ZIPLOG!" set "ZOK=1"
    )
)

echo.
if defined ZOK (
    echo ============================================================
    echo   ГОТОВО - файл для отправки:
    echo     !ZIPLOG!
    echo ============================================================
) else (
    echo ============================================================
    echo   ОТЧЁТ СОБРАН, но zip не удалось ^- нет powershell/tar.
    echo   Передайте папку вручную, сжав в zip:
    echo     !RDIR!
    echo ============================================================
)
pause
exit /b 0

REM ============================================================
REM  :CRLow «имя» -> %CLOW% нижний регистр
REM ============================================================
:CRLow
set "CLOW=%~1"
set "CLOW=!CLOW:A=a!"
set "CLOW=!CLOW:B=b!"
set "CLOW=!CLOW:C=c!"
set "CLOW=!CLOW:D=d!"
set "CLOW=!CLOW:E=e!"
set "CLOW=!CLOW:F=f!"
set "CLOW=!CLOW:G=g!"
set "CLOW=!CLOW:H=h!"
set "CLOW=!CLOW:I=i!"
set "CLOW=!CLOW:J=j!"
set "CLOW=!CLOW:K=k!"
set "CLOW=!CLOW:L=l!"
set "CLOW=!CLOW:M=m!"
set "CLOW=!CLOW:N=n!"
set "CLOW=!CLOW:O=o!"
set "CLOW=!CLOW:P=p!"
set "CLOW=!CLOW:Q=q!"
set "CLOW=!CLOW:R=r!"
set "CLOW=!CLOW:S=s!"
set "CLOW=!CLOW:T=t!"
set "CLOW=!CLOW:U=u!"
set "CLOW=!CLOW:V=v!"
set "CLOW=!CLOW:W=w!"
set "CLOW=!CLOW:X=x!"
set "CLOW=!CLOW:Y=y!"
set "CLOW=!CLOW:Z=z!"
exit /b 0
