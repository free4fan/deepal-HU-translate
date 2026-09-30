# Как WT_Launcher фильтрует приложения (скрывает от пользователя)

Лаунчер скрывает приложения через 7 уровней фильтрации. Если приложение **не** попадает ни под одно исключение — оно показывается пользователю.

---

## Архитектура

```
LocalAppFragment (UI)
    ↓ AppManager.getAppInfos(1)
    ↓ AppManager.getFilterAppMap() → TreeMap<Character, List<WTAppInfo>>
    ↓ AppManager.alignment() → appsMap (отсортированный JSON-кэш)
    ↓ AppManager.loadApps()
        ↓ CommonUtil.loadAllApps()      ← ГЛАВНЫЙ МЕТОД ФИЛЬТРАЦИИ
            ↓ PackageManager.getInstalledPackages(0)
```

### Ключевые файлы в декомпилированном `WT_Launcher`:
- `smali_classes2/com/tinnove/applist/AppManager.smali` — главный менеджер списка
- `smali_classes2/com/tinnove/comlib/utils/CommonUtil.smali` — фильтрация `loadAllApps()` (строки 2938–3214)
- `smali_classes2/com/tinnove/applist/AppManager$Companion.smali` — статические методы
- `smali_classes2/com/tinnove/applist/AppListActivity.smali` — UI списка (4812 строк)
- `smali_classes2/com/tinnove/applist/fragment/LocalAppFragment.smali` — отображение списка

### Постоянно сохраняемые данные:
- `appsMap` — LinkedHashMap<Integer, List<WTAppInfo>> (key 1 = "все приложения")
- `filterAppsMap` — TreeMap<String, List<WTAppInfo>> (ключ = первая буква пиньиня)
- `apps_cache` — JSON-кэш в SharedPreferences (строка 317)

---

## Уровень 1: Фильтрация в `CommonUtil.loadAllApps()` (строки 2938–3214)

Следующий **пакет пропускается**, если выполняется **любой** из условий:

| # | Фильтр | Строки | Описание |
|---|--------|--------|----------|
| 1 | `name.startsWith("com.android")` | 3030 | Пакеты с `com.android.` в начале |
| 2 | `name.startsWith("com.google")` | 3040 | Все Google-приложения |
| 3 | `name.startsWith("com.qualcomm")` | 3050 | Qualcomm-приложения |
| 4 | `name.startsWith("com.huawei")` | 3061 | Huawei-приложения |
| 5 | `name.startsWith("android.ext")` | 3071 | Android-расширения |
| 6 | `isPresetExcludeApp(pkg)` | 3079 | Из массива `exclude_apps` в ресурсах `com.tinnove.comlib` |
| 7 | `isSuperCar() && high_conf_apps.contains(pkg)` | 3086–3096 | SuperCar — пропускает "высокодоверенные" пакеты |
| 8 | `!hasAPA() && pkg.equals("com.adayo.app.apa")` | 3104–3116 | Без APA — пропускает `com.adayo.app.apa` |
| 9 | `!hasGimbalCamera() && pkg.equals("com.tinnove.cloudcamera")` | 3124–3136 | Без Gimbal Camera — пропускает `com.tinnove.cloudcamera` |
| 10 | Пакет уже есть в `exclude_apps` | 3075–3079 | Дублирование через CommonUtil.excludeApps |

**Все эти исключения работают через `return` / переход на `:cond_0` / переход на `:goto_0`** — они **пропускают** (не добавляют) пакет в список.

---

## Уровень 2: `exclude_apps` (ресурсы `com.tinnove.comlib`)

**Ресурс**: `String[] exclude_apps` → R.array.exclude_apps → id `0x7f030003`

**Чтение** (AppManager.loadPresetApps, строка 1329):
```smali
sget v0, Lcom/tinnove/comlib/R$array;->exclude_apps:I
invoke-virtual {p0, v0}, resources->getStringArray(I)[Ljava/lang/String;
```

**Применение** (CommonUtil.loadAllApps, строка 3075):
```smali
invoke-static {v5}, CommonUtil->isPresetExcludeApp(Ljava/lang/String;)Z
```

**Файл**: `smali_classes2/com/tinnove/comlib/utils/CommonUtil.smali` — строка 2574:
```smali
.method private static isPresetExcludeApp(Ljava/lang/String;)Z
    sget-object v0, Lcom/tinnove/comlib/utils/CommonUtil;->excludeApps:Ljava/util/List;
    invoke-interface {v0, p0}, Ljava/util/List;->contains(Ljava/lang/Object;)Z
    return v0
.end method
```

**Важно**: `exclude_apps` — это `String[]`, читаемый через `getStringArray()`. RRO-оверлей **НЕ может** добавить элемент в `exclude_apps`, потому что ресурс **не перезаписывается** (RRO объединяет, но не добавляет в массив).

**Как добавить — нужно добавить package в `exclude_apps` через:**
1. Пересборку оригинального `com.tinnove.comlib` APK (не рекомендуется)
2. Кастомный `<exclude_apps>` в RRO-оверлей (может не сработать из-за приоритета)

---

## Уровень 3: Динамическая фильтрация в AppManager.filterAppsByStatus() (строки 894–1056)

### 3.1 Drive Mode (Settings.Global DriveMode)
**Код** (строки 361–371): Читаем `Settings.Global.getInt("DriveMode")`. Если значение = 1:
- `driveModeAppInfo = com.deepal.ivi.hmi.drivemode` → **не удаляется**
- Остальные пакеты **удаляются** из списка (`removeIf` с lambda `lambda$8`)

### 3.2 Exhibition Mode (CarVirtualManager)
**Код** (строки 376–385): Читаем `CarVirtualManager.getValue(0x31400625, 0)`. Если значение = 3,794317E-9f:
- `exhibitionModeAppInfo = com.deepal.ivi.hmi.showcarmode` → **не удаляется**
- Остальные пакеты **удаляются** из списка (`removeIf` с lambda `lambda$11`)

### 3.3 LauncherApps.Callback (AppManager.appCallback$1)
**Код** (строка 312): Реестрирует `LauncherApps.registerCallback()`. При событии uninstall — вызывает `updateCacheApps()` и удаляет пакет.

---

## Уровень 4: `system_apps` — порядок отображения

`R.array.system_apps` → id `0x7f030008`

**Чтение** (строки 1349–1355):
```smali
sget v1, Lcom/tinnove/comlib/R$array;->system_apps:I
```

**Применение** (sortApps, строки 1429–1560):
```smali
# Системные приложения из system_apps[] ставятся первыми
# Остальные (не из system_apps) — после них
```

---

## Уровень 5: `high_conf_apps` — исключения для SuperCar

`R.array.high_conf_apps` → id `0x7f030005`

**Код** (строки 2958–3096): Если `isSuperCar()` = true && пакет есть в `high_conf_apps` → пропускается.

---

## Уровень 6: Real-time статус через AppStatusManager

**Файл**: `smali_classes2/com/tinnove/comlib/applist/AppStatusManager.smali`
**Сервис**: `com.tinnove.comlib.applist.AppStatusService`

- `ACTION_UPDATE_APP_STATUS` = `com.tinnove.launcher.intent.action_update_app_status`
- `HOT_STATUS_DISABLE = 0` → пакет не показывается
- `LAUNCHER_DEFAULT_APP_STATUS_SETTING_PREFIX = "launcher_default_app_status/"`

**Механизм**: через广播 событий обновляется статус пакета → отображается `isAppDisabled(pkg)`.

---

## Уровень 7: Отправка в RecentTaskManager

**Файл**: `smali_classes2/com/tinnove/dock/views/recent/RecentTaskManager.smali`

Читает тот же `R.array.exclude_apps` для фильтрации вRecent View.

---

## Сводная таблица скрытия RRO-приложений

| Цель | Метод | Файл | Строки | Надёжность |
|------|-------|------|--------|-----------|
| Скрыть из лаунчера | Добавить пакет в `exclude_apps` в ресурсах `com.tinnove.comlib` | String[] @ 0x7f030003 | — | Высокая |
| Скрыть без RRO | Дать пакету префикс `com.qualcomm.*` / `com.huawei.*` / `android.ext.*` | Манифест APK | — | Средняя (костыль) |
| Скрыть из UI | `isSuperCar() && high_conf_apps.contains(pkg)` | String[] @ 0x7f030005 | 3086 | Высокая |

---

## Практический пример для скрытия RRO-пакета от лаунчера

### Вариант A: Простой (префиксы, которые лаунчер пропускает)

**Минифест RRO**:
```xml
<?xml version="1.0" encoding="utf-8"?>
<manifest xmlns:android="http://schemas.android.com/apk/res/android"
    package="com.qualcomm.overlay.adayoapa"
    android:sharedUserId="android.uid.system">
    <application ...>
    </application>
</manifest>
```

→ Лаунчер пропустит пакет в `loadAllApps()` через фильтр №3 (строка 3050).

### Вариант B: Через `exclude_apps` (рекомендуется)

Добавить в `res/values/strings.xml` RRO-оверлея для `com.tinnove.comlib`:
```xml
<resources>
    <string-array name="exclude_apps">
        <item>existing.exclude.package</item>
        <item>com.deepal.translate.rro.adayoapa</item>
    </string-array>
</resources>
```

→ Лаунчер проверит: `isPresetExcludeApp("com.deepal.translate.rro.adayoapa")` = true → пропустит.

**Проблема**: RRO-оверлеи **не добавляют** элементы в `String[]` массив. Они **заменяют** весь массив. Нужно собрать RRO для `com.tinnove.comlib`, а не для отдельных RRO.

---

## Как запущена фильтрация (последовательность)

```
AppManager.init()
    ↓
AppManager.loadPresetApps(context)
    sget R.array.exclude_apps → excludeApps (List<String>)
    sget R.array.system_apps   → systemApps (List<String>)
    sget R.array.high_conf_apps→ highConfApps (List<String>)

AppManager.loadApps()
    ↓
CommonUtil.loadAllApps()
    ↓
PackageManager.getInstalledPackages(0)    # FLAG_SYSTEM | FLAG_INSTALLED | FLAG_DISABLED | FLAG_UNAVAILABLE | FLAG_UPDATED | FLAG_INCLUDE_RUNNING | FLAG_MATCH_DEBUGGED | FLAG_MATCH_UNINSTALLED_PACKAGES | FLAG_MATCH_GIDS
    
    for each PackageInfo:
        pkg = PackageInfo.packageName
        if startswith("com.android")      → skip
        if startswith("com.google")      → skip
        if startswith("com.qualcomm")    → skip
        if startswith("com.huawei")      → skip
        if startswith("android.ext")     → skip
        if isPresetExcludeApp(pkg)       → skip    ← exclude_apps
        if isSuperCar() && high_conf_apps contains pkg → skip
        if !hasAPA() && pkg=="com.adayo.app.apa"   → skip
        if !hasGimbalCamera() && pkg=="com.tinnove.cloudcamera" → skip
        else: WTAppInfo(pkg, name, label, icon, ...)  → addToList
        
    return allAppsList

AppManager.alignment(allAppsList)
    ↓
    if apps_cache empty:
        sortApps(allAppsList) → system_apps first, rest last
        filterAppsByStatus → remove drivemode/showcarmode apps
        sput appsMap[1] → allAppsList
    else:
        cached = GsonUtils.strToList(apps_cache, WTAppInfo.class)
        remove apps NOT in new list  # удаляем старые
        add new apps to list  # добавляем новые
        filterAppsByStatus
        updateCacheApps(newList)  # с сохранением в SharedPreferences
        sput appsMap[1]

AppManager.filterAppsByStatus(allAppsList)
    ↓
    driveModeVal = Settings.Global.getInt("DriveMode", 0)
    if driveModeVal == 1:
        # добавить driveModeAppInfo, если нет
        # удалить com.deepal.ivi.hmi.drivemode (если != driveModeAppInfo)
    else:
        removeIf pkg == "com.deepal.ivi.hmi.drivemode"  → lambda$8
    exhibitionVal = CarVirtualManager.getValue(0x31400625, 0)
    if exhibitionVal == 3.794317E-9f:
        # добавить exhibitionModeAppInfo, если нет
        # удалить com.deepal.ivi.hmi.showcarmode (если != exhibitionModeAppInfo)
    else:
        removeIf pkg == "com.deepal.ivi.hmi.showcarmode"  → lambda$11
    return filteredAllAppsList
```

---

## Дополнительные скрытия

### 1. Через `persistent="true"` + `coreApp="true"` (не скрывает, но защищает)
- **16 приложения** имеют `persistent="true"` — невозможно остановить
- **11 приложений** имеют `coreApp="true"` — защищается от удаления

### 2. Через `KEY_HIDE_SYSTEMBAR=true` (imersive mode)
- **18 приложений** скрывают статус-бар и навигацию в процессе

### 3. Через `android:enabled="false"` на `<activity>`
- Не скрывает приложение полностью, но скрывает отдельные действия

### 4. Через `sharedUserLabel` (generic label)
- `ContactsProvider` использует `@string/sharedUserLabel` — generic label вместо имени приложения

---

## Практические выводы

**Чтобы скрыть RRO-приложение:**
1. Наиболее надёжно: добавить package в `exclude_apps` в ресурсах `com.tinnove.comlib`
2. Проще: дать пакету префикс `com.qualcomm.*` / `com.huawei.*` / `android.ext.*`
3. Альтернатива: использовать `isSuperCar()` + `high_conf_apps` (для конкретных версий авто)
4. Можно комбинировать: скрыть из лаунчера + оставить активным через broadcast (сигнал из настроек)

**Чтобы проверить, скрыто ли приложение:**
```shell
adb shell am broadcast -a com.tinnove.launcher.intent.action_update_app_status
```
Проверяет через `AppStatusManager.notifyAppStatusChanged()`.

**Определить, какой пакет скрыт:**
```shell
adb shell cmd activity resolve --user 0 "android.intent.action.MAIN" "android.intent.category.HOME"
```

---

## Полезные команды для отладки

```shell
# Посмотреть все установленные пакеты (без filter)
adb shell pm list packages --user 0

# Посмотреть, что видит Launcher (через intent filter)
adb shell cmd package resolve-activity --user 0 \
  --query "android.intent.action.MAIN" "android.intent.category.LAUNCHER" 2>/dev/null | head

# Посмотреть текущий state DriveMode
adb shell settings get global DriveMode

# Посмотреть state ExhibitionMode
adb shell settings get global launcher_default_app_status/

# Посмотреть статус через AppStatusService
adb shell service list | grep app

# Посмотреть кэш приложений Launcher
adb shell run-as com.tinnove.launcher cat shared_prefs/apps_cache.xml
# или
adb shell cat /data/data/com.tinnove.launcher/shared_prefs/apps_cache.xml
```
