@echo off
setlocal EnableDelayedExpansion
chcp 65001 >nul
title Deepal HU Translate - Static RRO Manager

REM ============================================================
REM  Единственная точка управления СТАТИЧЕСКИМИ RRO
REM  (пакеты com.deepal.translate.rro.* - лежат в /vendor/overlay/
REM   и загружаются системой автоматически после перезагрузки).
REM
REM  Запуск:
REM    manage_static.bat                - интерактивное меню
REM    manage_static.bat <mode>         - режим, группа - меню
REM    manage_static.bat <mode> <preset>- сразу выполнять
REM
REM  modes:   install (adb push в /vendor/overlay + reboot)
REM           uninstall (adb rm -rf + reboot) | status | diag | report
REM           reboot (adb reboot)
REM  presets: 1 | 2 | 3 | 4 | 8 (top-8) | A (все) | C (2+3)
REM           auto (все *_RRO.apk из apks_rro_static) | 0 (меню)
REM
REM  report: неинтерактивный сбор логов (установленность/ошибки/crash/
REM           locale + ls /vendor/overlay) в .\logs\deepal_static_report_*.zip
REM           через collect_report.bat — для отправки на анализ.
REM           Без цели: manage_static.bat report
REM
REM  Требует: adb root + remount /vendor.
REM  Динамические оверлеи (com.android.vendor.translate.rro.*)
REM  управляются через manage.bat.
REM ============================================================

REM ================== данные (общие со статикой) ==================
set "GROUP1=CarService CarActivityResolver CarFrameworkPackageStubs SettingsProvider Shell WT_WtSystemUI PackageInstaller PowerManager InputDevices NetworkStack MediaProviderLegacy ExternalStorageProvider StorageWarn UserDictionaryProvider DownloadProvider DownloadProviderUi CertInstaller KeyChain VpnDialogs CompanionDeviceManager MtpService FusedLocation CaptivePortalLogin BackupRestoreConfirmation DynamicSystemInstallationService ManagedProvisioning ContactsProvider ldm CarPlayView"

set "GROUP2=WT_Launcher WT_MultiMediaCenter WT_VehicleCenter WT_BTPhone WT_AirConditioner Camera AdayoAPA AdayoDvr AdayoDvrLocalService WT_InputMethod WT_SystemService WT_ThemeResourcesDay WT_ThemeResourcesNight"

set "GROUP3=WT_FusionNavigation WT_AppStore WT_Album WT_FileManager WT_AIAssistant WT_CarLink WT_Link PhoneLink WT_TSpeech WT_VisualizationService WT_TinnoveCoreService WT_TinnoveSmartScene WT_AISpace WT_AISceneMode WT_SmartSoundEffect WT_AutoMaintenance WT_ElectronicDirections WT_Customer WT_ECall WT_HDCloudCamera WT_GameCenter WT_GameZone WT_Wcenter WT_AccountServer WT_IncallPersonalCenter WT_LightSoundLab WT_MLWecarControl WT_MiniApp"

set "GROUP4=AdayoAgnssService AdayoAlarm AdayoLog AutoTest deCoreApp DeepalDriveMode DeepalShowCarMode DynoMode EMode Fota fotaservice HiSight HiViewLite HwDMSDPDevice NaviManagerService Player857 Puremic SensetimeAiService SystemUpdater Upgrade WT_BubblePop WT_DownloadLog WT_FiveChess WT_IncallFunBox WT_IncallLive WT_Spacecraft WT_SpeedRun WT_SweepMine WT_TinnoveCore3D WT_WTAISceneEngine"

REM Top-8 критичных (были в install_8.bat / disable_8.bat)
set "TOP8=AdayoAPA AdayoDvr Camera WT_AirConditioner WT_BTPhone WT_Launcher WT_MultiMediaCenter WT_VehicleCenter"

set "PREFIX=com.deepal.translate.rro."
set "APK_DIR=apks_rro_static"
set "OVERLAY_BASE=/vendor/overlay"
set "FILTER=com.deepal.translate"
set "LOGS_DIR=logs"

REM CLI=1: запущен с аргументами -> после выполнения просто pause+exit,
REM без «вернуться в меню?» (обёртки install_8.bat и т.п.)
set "CLI=0"

REM ================== аргументы ==================
if "%~1"=="" goto MENU
set "CLI=1"
set "MODE=%~1"
if /i not "%MODE%"=="install" if /i not "%MODE%"=="uninstall" if /i not "%MODE%"=="status" if /i not "%MODE%"=="diag" if /i not "%MODE%"=="report" if /i not "%MODE%"=="reboot" goto MENU
call :ADB_CHECK
if not "!A_OK!"=="1" (
    echo.
    echo [ERROR] ADB недоступен - режим не запускается.
    echo   Запустите manage_static.bat без аргументов: меню покажет статус
    echo   ADB, повторная проверка - пункт [R].
    exit /b 1
)
if /i "%MODE%"=="status"   set "TARGETS=ok"
if /i "%MODE%"=="diag"     set "TARGETS=ok"
if /i "%MODE%"=="report"   set "TARGETS=ok"
if /i "%MODE%"=="reboot"   set "TARGETS=ok"
if defined TARGETS goto RUN
set "PRESET=%~2"
if "%PRESET%"=="" goto GROUP_MENU
call :ResolvePreset
if "!TARGETS!"=="" goto GROUP_MENU
goto RUN

REM ================== ADB (робастное подключение) ==================
REM Как в manage.bat: `adb devices` с retry/рестартом демона (не вечный
REM wait-for-device), обработчик unauthorized. ВАЖНО: функция никогда не
REM прерывает весь скрипт (exit /b возвращает только из :ADB_CHECK) —
REM вызывающая сторона читает флаг A_OK.
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
    REM Ограничиваем ожидание авторизации: без лимита цикл 45-секундных
    REM ожиданий идёт ВЕЧНО (меню не появляется).
    set /a A_UNAUTH_TOTAL+=1
    if !A_UNAUTH_TOTAL! GEQ 3 (
        echo   Авторизации нет после 3 ожиданий - остановка проверки.
        echo   Разрешите USB-отладку на устройстве и повторите проверку.
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

REM Быстрая проверка (одна `adb devices`, без ожиданий) — для баннера
REM статуса в меню: меню показывается всегда.
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

REM ================== ROOT + REMOUNT (нужно только для install/uninstall) ==================
REM RC_OK=1 - /vendor доступен; 0 - ошибка (см. RC_FAIL). Вызов через
REM `call :ROOT_CHECK` - `exit /b` внутри не остановит сценарий, читается
REM только RC_OK после возврата.
:ROOT_CHECK
set "RC_OK=0"
echo Obtaining root access...
adb root
if !ERRORLEVEL! NEQ 0 (
    echo ERROR: adb root failed. Device may not support root.
    goto :RC_FAIL
)
ping 127.0.0.1 -n 3 >nul
adb shell getprop ro.vendor.build.fingerprint >nul 2>&1
if !ERRORLEVEL! NEQ 0 (
    echo ERROR: Device not rooted or adb shell not accessible.
    goto :RC_FAIL
)
echo Remounting /vendor as read-write...
adb shell mount -o rw,remount /vendor 2>nul
if !ERRORLEVEL! NEQ 0 (
    adb remount
    if !ERRORLEVEL! NEQ 0 (
        echo ERROR: Cannot remount /vendor as read-write.
        goto :RC_FAIL
    )
)
echo [OK] /vendor is writable.
echo.
set "RC_OK=1"
exit /b 0
:RC_FAIL
pause
exit /b 1

:MENU
REM Меню показывается ВСЕГДА, без блокировки: только быстрая проверка.
REM Полная проверка (повторы + рестарт демона) — по [R] или при выборе действия.
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
echo   Deepal HU Translate - RRO Manager (статические)
echo ============================================================
echo.
echo   ADB:!A_DBANNER!
echo.
echo   Действие:
echo     [1] Установить   (adb push в !OVERLAY_BASE! + reboot)
echo     [2] Удалить      (adb rm -rf из !OVERLAY_BASE! + reboot)
echo     [3] Статус       (overlay list: все / наши)
echo     [4] Диагностика  (locale, наши оверлеи, dump, dumpsys)
echo     [5] Собрать логи  (report: zip для анализа)
echo     [6] Перезагрузить (adb reboot) - ТОЛЬКО нужно после install/uninstall
echo     [R] Проверить ADB заново  (полная проверка + рестарт демона)
echo     [0] Выход
echo.
set "MODE="
set /p "MODE=Выбор [0-6 или R]: "
if /i "%MODE%"=="0" exit /b 0
if /i "%MODE%"=="R" (
    echo.
    call :ADB_CHECK
    echo.
    goto MENU_SCREEN
)
set "ACT="
if /i "%MODE%"=="1" set "ACT=install"
if /i "%MODE%"=="2" set "ACT=uninstall"
if /i "%MODE%"=="3" set "ACT=status"
if /i "%MODE%"=="4" set "ACT=diag"
if /i "%MODE%"=="5" set "ACT=report"
if /i "%MODE%"=="6" set "ACT=reboot"
if not defined ACT (
    echo Неверный выбор.
    timeout /t 1 >nul
    goto MENU_SCREEN
)
REM Действия с device требуют живого adb.
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
REM install/uninstall пишут в /vendor - нужен root + remount
call :ROOT_CHECK
if not "!RC_OK!"=="1" (
    if /i "%CLI%"=="1" (
        echo.
        pause
        exit /b 1
    )
    goto MENU
)

REM ops-лог: каждое изменяющее действие пишется в %LOGS_DIR%\ops_&lt;mode&gt;_&lt;TS&gt;.log
set "OPS_LOG="
call :OpSTs
if /i "!MODE!"=="install"   set "OPS_LOG=!LOGS_DIR!\ops_install_!OTST!.log"
if /i "!MODE!"=="uninstall" set "OPS_LOG=!LOGS_DIR!\ops_uninstall_!OTST!.log"
if defined OPS_LOG (
    if not exist "!LOGS_DIR!" mkdir "!LOGS_DIR!"
    echo ==== %DATE% %TIME% ^| mode=!MODE! preset=!PRESET! ^| schema=static ^| root+remount ok ^| начало ==== >  "!OPS_LOG!"
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
    for %%F in ("%APK_DIR%\*_RRO.apk") do (
        set "F=%%~nF"
        set "F=!F:_RRO=!"
        set "TARGETS=!TARGETS! !F!"
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
REM  корректное имя report-записи (wmic → powershell → фолбэк).
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
REM  Статический оверлей лежит в %OVERLAY_BASE%/<package>/
REM ============================================================
:DoOne
set "NAME=%~1"
call :Lower "!NAME!"
set "PKG=%PREFIX%%LOWER%"
set "SRC=%APK_DIR%\!NAME!_RRO.apk"
set "DST=%OVERLAY_BASE%/!PKG!"

if /i "%MODE%"=="install" goto DO_INSTALL
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
if defined OPS_LOG echo ---- [INSTALL] !NAME! ^[!PKG!^] src=!SRC! dst=!DST! >> "!OPS_LOG!"
adb shell mkdir -p "!DST!"
if !ERRORLEVEL! NEQ 0 (
    echo   [ERROR] не удалось создать папку !DST!
    if defined OPS_LOG echo   RESULT: ERROR ^(mkdir !DST! не удался / нет root?^) >> "!OPS_LOG!"
    set /a FAIL_C+=1
    echo.
    exit /b 0
)
adb push "!SRC!" "!DST!/" >nul
if !ERRORLEVEL! EQU 0 (
    echo   [OK] push в !DST!
    if defined OPS_LOG echo   RESULT: OK ^(push в !DST!; применить - adb reboot^) >> "!OPS_LOG!"
    set /a OK_C+=1
) else (
    echo   [ERROR] push не удался
    if defined OPS_LOG echo   RESULT: ERROR ^(adb push не удался^) >> "!OPS_LOG!"
    set /a FAIL_C+=1
)
echo.
exit /b 0

:DO_UNINSTALL
echo [UNINSTALL] !NAME! ^[!PKG!^]
if defined OPS_LOG echo ---- [UNINSTALL] !NAME! ^[!PKG!^] dst=!DST! >> "!OPS_LOG!"
adb shell rm -rf "!DST!"
if !ERRORLEVEL! EQU 0 (
    echo   [OK] удалён
    if defined OPS_LOG echo   RESULT: OK ^(удалён !DST!; применить - adb reboot^) >> "!OPS_LOG!"
    set /a OK_C+=1
) else (
    echo   [WARN] не удалён ^(папки не было или нет прав^)
    if defined OPS_LOG echo   RESULT: WARN ^(не удалён: папки не было или нет прав^) >> "!OPS_LOG!"
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
if /i "%MODE%"=="install" (
    echo   Перезагрузите для применения:  adb reboot
)
if /i "%MODE%"=="uninstall" (
    echo   Перезагрузите для применения:  adb reboot
)
exit /b 0

REM ============================================================
REM  :Diag (бывший diag.bat)
REM ============================================================
:Diag
echo ========================================
echo Deepal RRO Diagnostic (статические)
echo ========================================
echo.
echo 1. System language:
adb shell settings get system system_locales
echo.
echo 2. Содержимое %OVERLAY_BASE% (наши):
adb shell "ls -d %OVERLAY_BASE%/com.deepal.translate.rro.* 2>/dev/null"
echo.
call :ShowStatus ours
echo.
set "DPKG="
set /p "DPKG=Пакет для dump (com.deepal.translate.rro.xxx, Enter - пропустить): "
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
REM  :CollectReport — сборка zip-отчёта для анализа (статические RRO)
REM  Дополнительно к общим: ls -lR /vendor/overlay (нужен root -
REM  collector сам попробует adb root).
REM ============================================================
:CollectReport
echo.
echo ============================================================
echo   Report collector: static (com.deepal.translate.rro.*)
echo   Файл для отправки появится в: logs\deepal_static_report_^<TS^>.zip
echo ============================================================
echo.
call "%~dp0collect_report.bat" static
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
