@echo off
setlocal EnableDelayedExpansion
chcp 65001 >nul
title Deepal HU Translate - RRO Manager

REM ============================================================
REM  Единственная точка управления ДИНАМИЧЕСКИМИ RRO
REM  (пакеты com.android.vendor.translate.rro.* - launcher их
REM   скрывает, LAUNCHER_APP_HIDDEN_DESIGN.md фильтр 1).
REM
REM  Запуск:
REM    manage.bat                       - интерактивное меню
REM    manage.bat <mode>                - режим, группа - меню
REM    manage.bat <mode> <preset>       - сразу выполнять
REM
REM  modes:   install (install+enable) | enable | disable
REM           uninstall (disable+adb uninstall) | status | diag | report
REM           reboot (adb reboot)
REM
REM  ВАЖНО: после install или uninstall перезагрузите ГУ (режим [8] /
REM  `reboot`). Динамические оверлеи обычно применяются без ребута, но
REM  полный перезапуск чистит кэш/состояние PMS и гарантированно применяет
REM  новые/удалённые оверлеи.
REM  presets: 1 | 2 | 3 | 4 | 8 (top-8) | A (все) | C (2+3)
REM           auto (все *_RRO.apk из apks_rro_min) | 0 (меню)
REM
REM  report: неинтерактивный сбор логов (установленность/ошибки/crash/
REM           locale) в .\logs\deepal_report_YYYY-MM-DD_HHMMSS.zip — для
REM           отправки на анализ. Без цели: manage.bat report.
REM
REM  Статические оверлеи (/vendor/overlay) - manage_static.bat.
REM ============================================================

REM ================== список целей (внешний targets.txt) ==================
REM Группы 1-4, TOP-8 и EXCLUDE9 - НЕ в коде скриптов, а в targets.txt
REM (рядом со скриптом): единый источник для manage.bat, manage_static.bat
REM и deepl. Другая прошивка (другие имена/набор APK) -> правится ТОЛЬКО
REM targets.txt, формат секций - в шапке файла.
REM (22b/25.09: 9 AOSP-target из EXCLUDE9 на этой сборке НЕ ставятся
REM adb install - "signed with different certificates, and the overlay lacks
REM <overlay android:targetName>"; для них - manage_static.bat,
REM /vendor/overlay, root.)
set "TARGETS_FILE=%~dp0targets.txt"
call :LoadTargets
if not "!LOAD_OK!"=="1" (
    echo.
    echo [ERROR] Не найден или нечитаем файл со списком целей:
    echo   !TARGETS_FILE!
    echo   Нужны секции: GROUP1 GROUP2 GROUP3 GROUP4 TOP8 EXCLUDE9
    echo   (формат - в шапке файла; копию см. в git-репозитории)
    pause
    exit /b 1
)

set "PREFIX=com.android.vendor.translate.rro."
set "APK_DIR=apks_rro_min"
set "FILTER=com.android.vendor"
set "LOGS_DIR=logs"

REM CLI=1: запущен с аргументами -> после выполнения просто pause+exit,
REM без «вернуться в меню?» (обёртки install_8.bat и т.п.)
set "CLI=0"

REM ================== аргументы ==================
if "%~1"=="" goto MENU
set "CLI=1"
set "MODE=%~1"
if /i not "%MODE%"=="install" if /i not "%MODE%"=="enable"  if /i not "%MODE%"=="disable" if /i not "%MODE%"=="uninstall" if /i not "%MODE%"=="status" if /i not "%MODE%"=="diag" if /i not "%MODE%"=="report" if /i not "%MODE%"=="reboot" goto MENU
call :ADB_CHECK
if not "!A_OK!"=="1" (
    echo.
    echo [ERROR] АDB недоступен - режим "%MODE%" не запускается.
    echo   Меню: manage.bat; пункт [R] - повторная проверка АDB
    exit /b 1
)
if /i "%MODE%"=="status"   set "TARGETS=ok"
if /i "%MODE%"=="diag"     set "TARGETS=ok"
if /i "%MODE%"=="report"   set "TARGETS=ok"
if defined TARGETS goto RUN
set "PRESET=%~2"
if "%PRESET%"=="" goto GROUP_MENU
call :ResolvePreset
if "!TARGETS!"=="" goto GROUP_MENU
goto RUN

REM ================== ADB (робастное подключение) ==================
REM Старый вариант гонял `adb wait-for-device`, который БЛОКИРОВАЛСЯ ВЕЧНО
REM (пустое окно после cls, меню не появлялось), если демон/устройство
REM было недоступно. Теперь: до 3 циклов `adb devices`, рестарт демона
REM (adb kill-server) при неудаче, отдельный обработчик "unauthorized".
REM Результат: A_OK=1 на успех; на неудачу — сообщение + A_OK=0.
REM ВАЖНО: функция никогда не прерывает весь скрипт — `exit /b` возвращает
REM только из :ADB_CHECK, а вызывающая сторона читает флаг A_OK. Меню
REM показывается ДАЖЕ при недоступном adb (баннер статуса + пункт [R]).
:ADB_CHECK
set "A_OK=0"
set "A_UNAUTH_TOTAL=0"
where adb >nul 2>&1
if !ERRORLEVEL! NEQ 0 (
    echo ERROR: adb not found in PATH.
    echo   Установите Android platform-tools и добавьте папку c adb.exe в PATH.
    exit /b 0
)
set "ADB_KILLED=0"
set "A_N=0"
:ADB_LOOP
set /a A_N+=1
echo ADB: проверка подключения (попытка !A_N!/3)...
set "A_DEV=0"
set "A_UNAUTH=0"
for /f "tokens=2" %%s in ('adb devices 2^>nul ^| findstr /I /R /C:"device$" /C:"unauthorized$"') do (
    if "%%s"=="device" set "A_DEV=1"
    if "%%s"=="unauthorized" set "A_UNAUTH=1"
)
if "!A_DEV!"=="1" (
    set "A_OK=1"
    set "A_N=0"
    set "ADB_KILLED=0"
    set "A_UNAUTH_TOTAL=0"
    echo [OK] ADB connected.
    echo.
    exit /b 0
)
if "!A_UNAUTH!"=="1" (
    REM Ограничиваем ожидание авторизации: старый вариант (A_N-=1) ждал
    REM 45-секундными циклами ВЕЧНО — меню не появлялось.
    set /a A_UNAUTH_TOTAL+=1
    if !A_UNAUTH_TOTAL! GEQ 3 (
        echo   Авторизации нет после 3 ожиданий - остановка проверки.
        echo   Разрешите USB-отладку на устройстве и повторите [R].
        exit /b 0
    )
    echo   Устройство подключено, но USB-отладка НЕ АВТОРИЗОВАНА.
    echo   НА УСТРОЙСТВЕ: нажмите "АВТОРИЗОВАТЬ", желательна галка
    echo   "Всегда разрешать". Жду 45 сек... Ctrl+C - отмена.
    timeout /t 45 >nul
    goto ADB_LOOP
)
echo   Устройство не найдено - нет строки с state=device.
if !A_N! GEQ 3 (
    echo.
    echo ERROR: ADB device not available after 3 attempts.
    echo   Попробуйте:
    echo    - USB-кабель и режим передачи файлов, а не только зарядки;
    echo    - на устройстве: Параметры - Для разработчиков - USB-отладка - вкл;
    echo    - галочка "АВТОРИЗОВАТЬ" на экране устройства;
    echo    - вручную: adb kill-server, затем adb devices
    exit /b 0
)
if /i "%ADB_KILLED%"=="0" (
    echo   Перезапуск adb-демона: adb kill-server, adb start-server
    adb kill-server >nul 2>&1
    adb start-server >nul 2>&1
    ping 127.0.0.1 -n 4 >nul
    set "ADB_KILLED=1"
) else (
    timeout /t 3 >nul
)
goto ADB_LOOP

REM Быстрая проверка (одна `adb devices`, без ожиданий и рестарта демона) —
REM для баннера статуса в меню: меню показывается всегда, независимо от
REM того, подключён ли adb.
:ADB_CHECK_FAST
set "A_OK=0"
set "A_UNAUTH=0"
where adb >nul 2>&1
if !ERRORLEVEL! NEQ 0 exit /b 0
for /f "tokens=2" %%s in ('adb devices 2^>nul ^| findstr /I /R /C:"device$" /C:"unauthorized$"') do (
    if "%%s"=="device" set "A_OK=1"
    if "%%s"=="unauthorized" set "A_UNAUTH=1"
)
exit /b 0

:MENU
REM Меню показывается ВСЕГДА, без блокировки: только быстрая проверка
REM (одна adb devices). Полная проверка (повторы + рестарт демона) —
REM по [R] или в момент выбора действия.
goto MENU_SCREEN

:MENU_SCREEN
REM Баннер статуса ADB (быстрая проверка, ничего не блокирует меню).
set "A_OK=0"
call :ADB_CHECK_FAST
if "!A_OK!"=="1" (
    set "A_DBANNER= [OK] ADB: устройство подключено"
) else (
    set "A_DBANNER= [!!] ADB: устройство НЕ найдено — подключите USB, разрешите отладку (кнопка «АВТОРИЗОВАТЬ» на экране HU), после исправления - [R]"
)
cls
echo ============================================================
echo   Deepal HU Translate - RRO Manager (динамические)
echo ============================================================
echo.
echo   ADB:!A_DBANNER!
echo.
echo   Действие:
echo     [1] Установить + включить   (install + enable)
echo     [2] Включить                (enable)
echo     [3] Отключить               (disable)
echo     [4] Удалить                 (disable + adb uninstall)
echo     [5] Статус                  (overlay list: все / наши)
echo     [6] Диагностика             (locale, наши оверлеи, dump, dumpsys)
echo     [7] Собрать логи            (report: zip для анализа)
echo     [8] Перезагрузить ГУ        (adb reboot)
echo     [R] Проверить ADB заново    (полная проверка + рестарт демона)
echo     [0] Выход
echo.
set "MODE="
set /p "MODE=Выбор [0-8 или R]: "
if /i "%MODE%"=="0" exit /b 0
if /i "%MODE%"=="R" (
    echo.
    call :ADB_CHECK
    echo.
    goto MENU_SCREEN
)
set "ACT="
if /i "%MODE%"=="1" set "ACT=install"
if /i "%MODE%"=="2" set "ACT=enable"
if /i "%MODE%"=="3" set "ACT=disable"
if /i "%MODE%"=="4" set "ACT=uninstall"
if /i "%MODE%"=="5" set "ACT=status"
if /i "%MODE%"=="6" set "ACT=diag"
if /i "%MODE%"=="7" set "ACT=report"
if /i "%MODE%"=="8" set "ACT=reboot"
if not defined ACT (
    echo Неверный выбор.
    timeout /t 1 >nul
    goto MENU_SCREEN
)
REM Действия с device требуют живого adb (status/diag/report — тоже).
call :ADB_CHECK
if not "!A_OK!"=="1" (
    echo.
    echo [ERROR] ADB недоступен - действие «%ACT%» не выполняется.
    echo   Пункт [R] - повторная проверка ADB.
    timeout /t 5 >nul
    goto MENU_SCREEN
)
set "MODE=!ACT!"
REM status/diag/report/reboot не требуют выбора цели
if /i "%MODE%"=="status" ( set "TARGETS=ok" & goto RUN )
if /i "%MODE%"=="diag"   ( set "TARGETS=ok" & goto RUN )
if /i "%MODE%"=="report" ( set "TARGETS=ok" & goto RUN )
if /i "%MODE%"=="reboot" ( set "TARGETS=ok" & goto RUN )
cls
:GROUP_MENU
echo ============================================================
echo   Режим: %MODE%
echo ============================================================
echo.
echo   Цель:
echo     [1]  CRITICAL       (системные сервисы)
echo     [2]  CORE UI        (основной интерфейс)
echo     [3]  IMPORTANT APPS
echo     [4]  SECONDARY      (второстепенные)
echo     [8]  TOP-8          (критичные 8, как install_8.bat)
echo     [A]  Все группы
echo     [C]  CORE + IMPORTANT (2+3, без системных)
echo     [auto] Все *_RRO.apk из %APK_DIR%
echo     [0]  Назад к действию
echo.
set "PRESET="
set /p "PRESET=Выбор: "
if "%PRESET%"=="" set "PRESET=0"
if "%PRESET%"=="0" goto MENU
call :ResolvePreset
if "!TARGETS!"=="" (
    echo Неверный выбор.
    timeout /t 1 >nul
    goto GROUP_MENU
)
goto RUN

REM ================== выполнение ==================
:RUN
cls
echo ============================================================
echo   %MODE%
echo ============================================================
echo.

if /i "%MODE%"=="status" (
    call :ShowStatus full
    goto ASK_MENU
)
if /i "%MODE%"=="diag" (
    call :Diag
    goto ASK_MENU
)
if /i "%MODE%"=="report" (
    call :CollectReport
    goto ASK_MENU
)
if /i "%MODE%"=="reboot" (
    echo [REBOOT] Перезагрузка ГУ: adb reboot
    adb reboot
    echo   Устройство перезагрузится, adb-подключение прервётся.
    goto ASK_MENU
)

REM ops-лог: каждое изменяющее действие пишется в %LOGS_DIR%\ops_&lt;mode&gt;_&lt;TS&gt;.log
set "OPS_LOG="
call :OpSTs
if /i "!MODE!"=="install"   set "OPS_LOG=!LOGS_DIR!\ops_install_!OTST!.log"
if /i "!MODE!"=="enable"    set "OPS_LOG=!LOGS_DIR!\ops_enable_!OTST!.log"
if /i "!MODE!"=="disable"   set "OPS_LOG=!LOGS_DIR!\ops_disable_!OTST!.log"
if /i "!MODE!"=="uninstall" set "OPS_LOG=!LOGS_DIR!\ops_uninstall_!OTST!.log"
if defined OPS_LOG (
    if not exist "!LOGS_DIR!" mkdir "!LOGS_DIR!"
    echo ==== %DATE% %TIME% ^| mode=!MODE! preset=!PRESET! ^| schema=dynamic ^| начало ==== > "!OPS_LOG!"
)

REM ============================================================
REM  Root-режим на ВЕСЬ прогон (install+enable+disable+uninstall от uid 0).
REM  Казус WT_WtSystemUI: target com.android.systemui определяет <overlayable>
REM  (car-ui-lib/rotary-ui), и AOSP 11 OverlayActorEnforcer запрещает
REM  'cmd overlay enable/disable' для shell uid 2000 (SecurityException
REM  UID2000 is not allowed to call setEnabled). root (ROOT_UID) разрешён
REM  всегда. 'adb root' -> shell=uid 0; на этом ГУ работает (ro.debuggable=1,
REM  live: enable от root -> оверлей активен, перевод SystemUI работает).
REM
REM  КАК: 'adb root' перед ВЕСЬ прогоном, БЕЗ флага-пропуска:
REM  (2026-09-28 18:14: флаг "один раз на окно" пережил РЕБУТ ГУ, а adb
REM  root - нет: прогон ушёл от shell uid 2000, WT_WtSystemUI не
REM  включился; 2026-09-29 09:03: call :DoOpAdb внутри if-блока рвал
REM  uninstall на ". was unexpected at this time."). Уже root -> no-op:
REM  вызов 'adb root' только при uid <> 0, два дешёвых 'id -u' на прогон.
REM  'adb root' РЕСТАРТИТ adbd -> сразу после него 'adb wait-for-device'.
REM  uid читаем temp-файлом + for /f 'type'. ФОРМА ПЛОСКАЯ (без call и
REM  без if/else в скобках): запуск подпрограммы с for/exit изнутри
REM  блока рвёт state парсера (сл. 26.09 заметка о хрупкости call).
REM  Исход пишется строкой [ROOT] в ops-лог - при live-анализе: строки нет
REM  = этот bat переключать root НЕ умел (старая ревизия).
REM  Только изменяющие режимы: status/diag/report выше ушли до этой точки.
REM ============================================================
set "IDF=%~dp0%RANDOM%_uid.txt"
adb shell id -u >"!IDF!" 2>&1
set "UID_NOW="
for /f "delims=" %%i in ('type "!IDF!"') do set "UID_NOW=%%i"
del "!IDF!" >nul 2>&1
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
REM adbd может перезапуститься - ждём, пока вернётся
adb wait-for-device >nul 2>&1
set "IDF=%~dp0%RANDOM%_uid.txt"
adb shell id -u >"!IDF!" 2>&1
set "UID_NEW="
for /f "delims=" %%i in ('type "!IDF!"') do set "UID_NEW=%%i"
del "!IDF!" >nul 2>&1
if "!UID_NEW!"=="0" (
    echo [OK] root активен: uid=0 - весь прогон идёт от root.
    if defined OPS_LOG echo   [ROOT] uid=0 ^(было !UID_NOW!^) >> "!OPS_LOG!"
) else (
    echo [WARN] root НЕ активен: uid=!UID_NEW! ^(ожидал 0^) - adb root не supported? enable целей с ^<overlayable^> ^(WT_WtSystemUI^) упадёт - довести вручную: adb root и повторить manage.bat enable. Если ГУ только что ребутнулось - root теряется, повторить ПРОГОН.
    if defined OPS_LOG echo   [ROOT] uid=!UID_NEW! после adb root - не root ^(было !UID_NOW!^) >> "!OPS_LOG!"
)

set "TOTAL=0"
set "OK_C=0"
set "FAIL_C=0"
set "MISS_C=0"
for %%A in (%TARGETS%) do (
    set /a TOTAL+=1
    call :DoOne "%%A"
)
if defined OPS_LOG (
    echo ==== %DATE% %TIME% ^| конец: всего=!TOTAL! ok=!OK_C! err=!FAIL_C! нет_apk=!MISS_C! ==== >> "!OPS_LOG!"
    echo   Операции записаны: !OPS_LOG!
)
call :PrintSummary
call :ShowStatus ours

:ASK_MENU
if /i "%CLI%"=="1" (
    echo.
    pause
    exit /b 0
)
echo.
set "AGAIN="
set /p "AGAIN=Вернуться в меню? [Y/N]: "
if /i "%AGAIN%"=="Y" goto MENU
exit /b 0

REM ============================================================
REM  :LoadTargets - читает !TARGETS_FILE! (targets.txt): секции
REM  GROUP1..GROUP4, TOP8, EXCLUDE9. Заполняет одноимённые переменные
REM  (имена через пробел); LOAD_OK=1 если найден хотя бы один пакет.
REM  Формат файла (см. шапку targets.txt): имя секции на отдельной
REM  строке, далее имена (по одному, или несколько через пробел);
REM  # в начале строки - комментарий. Заголовок секции определяется
REM  строкой без пробелов целиком (имена пакетов пробелов не имеют).
REM  Чтение через type "!" (как в :DoOpAdb): usebackq in ("!var!")
REM  в wine не экстендирует переменную (заметка 26.09) - портативно.
REM  ФОРМА ПЛОСКАЯ: только set/if в теле for; накопление - ЯВНЫМ
REM  set "VARIABLE=!VARIABLE! ..." на секцию (индиректный
REM  set "!SEC!=..." пишет в переменную с ТЕМ ИМЕНЕМ, что совпало
REM  - проверено wine 30.09).
REM ============================================================
:LoadTargets
set "LOAD_OK=0"
set "GROUP1="
set "GROUP2="
set "GROUP3="
set "GROUP4="
set "TOP8="
set "EXCLUDE9="
set "TGT_N=0"
if not exist "!TARGETS_FILE!" exit /b 0
set "TGT_SEC="
for /f "eol=# delims=" %%i in ('type "!TARGETS_FILE!"') do (
    set "TGT_LINE=%%i"
    set "TGT_H=!TGT_LINE: =!"
    REM TGT_V сбрасываем (флаг «строка-заголовок»); TGT_SEC НЕТ -
    REM текущая секция сохранится до следующего заголовка.
    set "TGT_V="
    if /i "!TGT_H!"=="GROUP1"   set "TGT_V=1"
    if /i "!TGT_H!"=="GROUP1"   set "TGT_SEC=G1"
    if /i "!TGT_H!"=="GROUP2"   set "TGT_V=1"
    if /i "!TGT_H!"=="GROUP2"   set "TGT_SEC=G2"
    if /i "!TGT_H!"=="GROUP3"   set "TGT_V=1"
    if /i "!TGT_H!"=="GROUP3"   set "TGT_SEC=G3"
    if /i "!TGT_H!"=="GROUP4"   set "TGT_V=1"
    if /i "!TGT_H!"=="GROUP4"   set "TGT_SEC=G4"
    if /i "!TGT_H!"=="TOP8"     set "TGT_V=1"
    if /i "!TGT_H!"=="TOP8"     set "TGT_SEC=T8"
    if /i "!TGT_H!"=="EXCLUDE9" set "TGT_V=1"
    if /i "!TGT_H!"=="EXCLUDE9" set "TGT_SEC=E9"
    REM без /i: wine-баг - `if /i` в 3-й вложенности в теле for молча
    REM не срабатывает (проверено 30.09); TGT_SEC - управляемый скриптом
    REM код, case фиксированный, сравнение точное.
    if not defined TGT_V if not "!TGT_LINE!"=="" if "!TGT_SEC!"=="G1" set "GROUP1=!GROUP1! !TGT_LINE!"
    if not defined TGT_V if not "!TGT_LINE!"=="" if "!TGT_SEC!"=="G2" set "GROUP2=!GROUP2! !TGT_LINE!"
    if not defined TGT_V if not "!TGT_LINE!"=="" if "!TGT_SEC!"=="G3" set "GROUP3=!GROUP3! !TGT_LINE!"
    if not defined TGT_V if not "!TGT_LINE!"=="" if "!TGT_SEC!"=="G4" set "GROUP4=!GROUP4! !TGT_LINE!"
    if not defined TGT_V if not "!TGT_LINE!"=="" if "!TGT_SEC!"=="T8" set "TOP8=!TOP8! !TGT_LINE!"
    if not defined TGT_V if not "!TGT_LINE!"=="" if "!TGT_SEC!"=="E9" set "EXCLUDE9=!EXCLUDE9! !TGT_LINE!"
    if not defined TGT_V if not "!TGT_LINE!"=="" set /a TGT_N+=1
)
if !TGT_N! GTR 0 set "LOAD_OK=1"
exit /b 0

REM ============================================================
REM  :ResolvePreset - заполняет TARGETS по PRESET (0/пусто -> "")
REM ============================================================
:ResolvePreset
set "TARGETS="
if /i "!PRESET!"=="1" set "TARGETS=%GROUP1%"
if /i "!PRESET!"=="2" set "TARGETS=%GROUP2%"
if /i "!PRESET!"=="3" set "TARGETS=%GROUP3%"
if /i "!PRESET!"=="4" set "TARGETS=%GROUP4%"
if /i "!PRESET!"=="8" set "TARGETS=%TOP8%"
if /i "!PRESET!"=="A" set "TARGETS=%GROUP1% %GROUP2% %GROUP3% %GROUP4%"
if /i "!PRESET!"=="C" set "TARGETS=%GROUP2% %GROUP3%"
if /i "!PRESET!"=="auto" (
    REM Без кавычек: glob '"..\*_RRO.apk"' не работал в wine (0 файлов);
    REM путь без пробелов - кавычки не нужны, на Windows поведение то же.
    for %%F in (%APK_DIR%\*_RRO.apk) do (
        set "F=%%~nF"
        set "F=!F:_RRO=!"
        set "SK="
        for %%E in (%EXCLUDE9%) do if /i "%%E"=="!F!" set "SK=1"
        if not defined SK set "TARGETS=!TARGETS! !F!"
    )
)
if /i "!PRESET!"=="0" set "TARGETS=none"
REM status/diag не требуют TARGETS
if /i "!MODE!"=="status" set "TARGETS=ok"
if /i "!MODE!"=="diag"   set "TARGETS=ok"
exit /b 0

REM ============================================================
REM  :OpSTs -> %OTST% - таймстемп YYYYMMDDHHMMSS (только цифры) для
REM  имён ops-логов. ВАЖНО: БЕЗ строковых подстановок вида
REM  !X:=_! (замена ПАРЫ символов) — на живой Windows/22.09 она дала
REM  битое имя файла «ops_uninstall_OTST:=_.log» (файл с ":" в имени не
REM  создаётся → ops-лог пропал, «ops_*.log: 0 файла» в отчёте).
REM  Здесь — только substrings по индексу, те же идиомы, что дают
REM  корректное имя report-записи (wmic → powershell → TIME-фолбэк).
REM ============================================================
:OpSTs
set "OTST="
set "DT="
for /f "tokens=2 delims==" %%i in ('wmic os get localdatetime /value 2^>nul') do set "DT=%%i"
if defined DT set "OTST=!DT:~0,14!"
if not defined OTST for /f "delims=" %%i in ('powershell -NoProfile -Command "Get-Date -Format yyyyMMddHHmmss" 2^>nul') do set "OTST=%%i"
if not defined OTST set "OTST=R%RANDOM%%RANDOM%"
if not defined OTST set "OTST=R0"
exit /b 0

REM ============================================================
REM  :DoOne <name>  (режим в %MODE%)
REM ============================================================
:DoOne
set "NAME=%~1"
call :Lower "!NAME!"
set "PKG=%PREFIX%%LOWER%"
set "SRC=%APK_DIR%\!NAME!_RRO.apk"

if /i "%MODE%"=="install" goto DO_INSTALL
if /i "%MODE%"=="enable" goto DO_ENABLE
if /i "%MODE%"=="disable" goto DO_DISABLE
if /i "%MODE%"=="uninstall" goto DO_UNINSTALL
exit /b 0

:DO_INSTALL
if not exist "!SRC!" (
    echo [WARN] !NAME!: нет !SRC!
    set /a MISS_C+=1
    echo.
    exit /b 0
)
echo [INSTALL] !NAME! ^[!PKG!^]
if defined OPS_LOG echo ---- [INSTALL] !NAME! ^[!PKG!^] src=!SRC! >> "!OPS_LOG!"
set "OKF=0"
REM Матч "Success" по ПЕРВЫМ 7 символам + delims= : точное сравнение
REM "%%I"=="Success" хрупко к хвостовому пробелу/CR (зависит от adb-codepage/
REM wine/Windows) - в реальных логах 22.09 строка захватывалась как
REM "Success " и установка шла в ERROR-ветку. Префикс "Success" не
REM перепутать: "Performing..." и "Failure [..]" стартуют иначе.
for /f "tokens=* delims= " %%I in ('adb install -r "!SRC!" 2^>^&1') do (
    echo   %%I
    if defined OPS_LOG echo   db: %%I >> "!OPS_LOG!"
    set "L=%%I"
    if /i "!L:~0,7!"=="Success" set "OKF=1"
)
if !OKF! EQU 1 (
    REM enable видимым (а не >nul): сразу после install PMS иногда ещё не
    REM просканировал пакет -> enable падает (кейс WT_WtSystemUI, 22.09:
    REM стоял [ ] без единой строки в логe). Короткая пауза + 1 retry.
    ping 127.0.0.1 -n 2 >nul
    call :DoOpAdb shell cmd overlay enable --user 0 !PKG!
    set "ENC=!ERRORLEVEL!"
    if not !ENC! EQU 0 (
        echo   [INFO] enable не с первого раза - повтор
        ping 127.0.0.1 -n 3 >nul
        call :DoOpAdb shell cmd overlay enable --user 0 !PKG!
        set "ENC=!ERRORLEVEL!"
    )
    if !ENC! EQU 0 (
        echo   [OK] установлен и включён
        if defined OPS_LOG echo   RESULT: OK ^(установлен и включён^) >> "!OPS_LOG!"
        set /a OK_C+=1
    ) else (
        echo   [WARN] установлен, НО оверлей НЕ включён - довести: `manage.bat enable` [menu 2]
        echo   [WARN] если enable падает с "UID2000 is not allowed" - нужен adb root ^(строки [ROOT] выше^)
        if defined OPS_LOG echo   RESULT: WARN ^(установлен, оверлей не включён; rc=ENC см. выше; SecurityException UID2000 ^=> нужен adb root^) >> "!OPS_LOG!"
        set /a OK_C+=1
    )
) else (
    echo   [ERROR] установка не удалась
    if defined OPS_LOG echo   RESULT: ERROR ^(adb install: см. строки "db:" выше^) >> "!OPS_LOG!"
    set /a FAIL_C+=1
)
echo.
exit /b 0

REM Вывод adb-команды: экранный эхо + запись в ops-лог + сохранение в
REM %TEMPF%, чтобы после for /f (!ERRORLEVEL! фор обнуляет) проверить код.
:DoOpAdb
set "TEMPF=%~dp0%RANDOM%_tmp.txt"
adb %~1 %~2 %~3 %~4 %~5 %~6 %~7 %~8 %~9 >"!TEMPF!" 2>&1
set "ADBRC=!ERRORLEVEL!"
REM 'type "!F!"' (без usebackq): портативно — переменная расширяется в
REM команде; usebackq+in ("!F!") не расширяет в wine и капризничает.
for /f "tokens=* delims=" %%I in ('type "!TEMPF!"') do (
    echo   %%I
    if defined OPS_LOG echo   db: %%I >> "!OPS_LOG!"
)
del "!TEMPF!" >nul 2>&1
exit /b !ADBRC!

:DO_ENABLE
echo [ENABLE] !NAME! ^[!PKG!^]
if defined OPS_LOG echo ---- [ENABLE] !NAME! ^[!PKG!^] >> "!OPS_LOG!"
call :DoOpAdb shell cmd overlay enable --user 0 !PKG!
if !ERRORLEVEL! EQU 0 (
    echo   [OK]
    if defined OPS_LOG echo   RESULT: OK >> "!OPS_LOG!"
    set /a OK_C+=1
) else (
    echo   [ERROR] ^(пакет не установлен?^)
    if defined OPS_LOG echo   RESULT: ERROR ^(пакет не установлен?^) >> "!OPS_LOG!"
    set /a FAIL_C+=1
)
echo.
exit /b 0

:DO_DISABLE
echo [DISABLE] !NAME! ^[!PKG!^]
if defined OPS_LOG echo ---- [DISABLE] !NAME! ^[!PKG!^] >> "!OPS_LOG!"
call :DoOpAdb shell cmd overlay disable --user 0 !PKG!
if !ERRORLEVEL! EQU 0 (
    echo   [OK]
    if defined OPS_LOG echo   RESULT: OK >> "!OPS_LOG!"
    set /a OK_C+=1
) else (
    echo   [ERROR] ^(пакет не установлен?^)
    if defined OPS_LOG echo   RESULT: ERROR ^(пакет не установлен?^) >> "!OPS_LOG!"
    set /a FAIL_C+=1
)
echo.
exit /b 0

:DO_UNINSTALL
echo [UNINSTALL] !NAME! ^[!PKG!^]
if defined OPS_LOG echo ---- [UNINSTALL] !NAME! ^[!PKG!^] >> "!OPS_LOG!"
REM Сначала disable, потом uninstall - иначе overlay может остаться активным
adb shell cmd overlay disable --user 0 !PKG! >nul 2>&1
call :DoOpAdb uninstall !PKG!
if !ERRORLEVEL! EQU 0 (
    echo   [OK] удалён
    if defined OPS_LOG echo   RESULT: OK ^(удалён^) >> "!OPS_LOG!"
    set /a OK_C+=1
) else (
    echo   [ERROR] ^(не установлен?^)
    if defined OPS_LOG echo   RESULT: ERROR ^(не установлен?^) >> "!OPS_LOG!"
    set /a FAIL_C+=1
)
echo.
exit /b 0

REM ============================================================
REM  :ShowStatus [full|ours]
REM ============================================================
:ShowStatus
echo.
echo ========================================
if /i "%~1"=="full" (
    echo Overlay status ^(все^):
    adb shell cmd overlay list --user 0
) else (
    echo Overlay status ^(наши, %FILTER%^):
    (adb shell cmd overlay list --user 0) | findstr /i /c:"!FILTER!"
)
echo   [x] - включён  [ ] - отключён  --- - ошибка
exit /b 0

REM ============================================================
REM  :PrintSummary
REM ============================================================
:PrintSummary
echo.
echo ========================================
echo   Итог: %MODE%
echo ========================================
echo   Всего:      !TOTAL!
echo   OK:         !OK_C!
echo   Ошибок:     !FAIL_C!
echo   Не найдено: !MISS_C!
exit /b 0

REM ============================================================
REM  :Diag (бывший diag.bat)
REM ============================================================
:Diag
echo ========================================
echo Deepal RRO Diagnostic (динамические)
echo ========================================
echo.
echo 1. System language:
adb shell settings get system system_locales
echo.
call :ShowStatus ours
echo.
set "DPKG="
set /p "DPKG=Пакет для dump (com.android.vendor.translate.rro.xxx, Enter - пропустить): "
if not "!DPKG!"=="" (
    echo.
    echo 3. Overlay dump:
    adb shell cmd overlay dump "!DPKG!"
)
echo.
set "TPKG="
set /p "TPKG=Целевое приложение для dumpsys (напр. com.adayo.app.apa, Enter - пропустить): "
if not "!TPKG!"=="" (
    echo.
    echo 4. Target app overlays:
    (adb shell dumpsys package "!TPKG!") | findstr -i "overlay"
)
echo.
exit /b 0

REM ============================================================
REM  :CollectReport — сборка zip-отчёта для анализа (динамические RRO)
REM  Все данные наследуем из env (PREFIX, FILTER, APK_DIR, GROUP1-4).
REM  Ничего не меняет на устройстве — только чтение.
REM ============================================================
:CollectReport
echo.
echo ============================================================
echo   Report collector: dynamic (com.android.vendor.translate.rro.*)
echo   Файл для отправки появится в: logs\deepal_dynamic_report_^<TS^>.zip
echo ============================================================
echo.
call "%~dp0collect_report.bat" dynamic
exit /b 0

REM ============================================================
REM  :Lower <name> -> %LOWER%
REM ============================================================
:Lower
set "LOWER=%~1"
set "LOWER=%LOWER:A=a%"
set "LOWER=%LOWER:B=b%"
set "LOWER=%LOWER:C=c%"
set "LOWER=%LOWER:D=d%"
set "LOWER=%LOWER:E=e%"
set "LOWER=%LOWER:F=f%"
set "LOWER=%LOWER:G=g%"
set "LOWER=%LOWER:H=h%"
set "LOWER=%LOWER:I=i%"
set "LOWER=%LOWER:J=j%"
set "LOWER=%LOWER:K=k%"
set "LOWER=%LOWER:L=l%"
set "LOWER=%LOWER:M=m%"
set "LOWER=%LOWER:N=n%"
set "LOWER=%LOWER:O=o%"
set "LOWER=%LOWER:P=p%"
set "LOWER=%LOWER:Q=q%"
set "LOWER=%LOWER:R=r%"
set "LOWER=%LOWER:S=s%"
set "LOWER=%LOWER:T=t%"
set "LOWER=%LOWER:U=u%"
set "LOWER=%LOWER:V=v%"
set "LOWER=%LOWER:W=w%"
set "LOWER=%LOWER:X=x%"
set "LOWER=%LOWER:Y=y%"
set "LOWER=%LOWER:Z=z%"
exit /b 0
