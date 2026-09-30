#!/usr/bin/env python3
"""Build static RRO APKs from pre-generated overlay structures in overlays_static/.

Static RRO APKs are placed into /vendor/overlay/ or /product/overlay/ on the device.
They are activated automatically on boot, hidden from app list, and cannot be
disabled by the user (android:isStatic="true").

Installation on device:
    adb root && adb remount
    adb shell mkdir -p /vendor/overlay/<app_name>
    adb push <app_name>_RRO.apk /vendor/overlay/<app_name>/
    adb reboot
"""
import json, os, re, shutil, subprocess, tempfile
from pathlib import Path
from xml.etree.ElementTree import parse as et_parse

TRANS = Path("./translations")
DECOMPILED = Path("./decompiled")
OVERLAYS = Path("./overlays_static")
BUILD = Path("./overlay_build_static")
APK_OUT = Path("./apks_rro_static")
AAPT2 = "/usr/bin/aapt2"
APKSIGNER = "/usr/bin/apksigner"
ANDROID_JAR = Path("/opt/android-sdk/platforms/android-34/android.jar")
JKS = Path("./keys/platform.jks")

PLURAL_QTYS = ("one", "few", "many", "other")


def get_target_package(app_name):
    """Find package_name from decompiled APK manifest."""
    yml_path = Path(f"decompiled/{app_name}/apktool.yml")
    if yml_path.exists():
        manifest = yml_path.parent / "AndroidManifest.xml"
        if manifest.exists():
            try:
                tree = et_parse(str(manifest))
                return tree.getroot().get("package", "")
            except Exception:
                return ""
    for yml in Path("decompiled").rglob(f"*/*{app_name}*/*/apktool.yml"):
        manifest = yml.parent / "AndroidManifest.xml"
        if manifest.exists():
            try:
                pkg = et_parse(str(manifest)).getroot().get("package", "")
                if pkg: return pkg
            except: pass
    return fallback.get(app_name, "")


fallback = {
    "WT_Link": "com.tinnove.link.client",
    "WT_WTAISceneEngine": "com.autopai.sceneengine",
    "WT_SweepMine": "com.wt.sweepmine",
    "WT_SystemService": "com.tinnove.systemservice",
    "WT_TSpeech": "com.tinnove.wecarspeech",
    "WT_AISpace": "com.tinnove.aispace",
    "WT_TinnoveCoreService": "com.wt.tinnovecoreservice",
    "WT_TinnoveSmartScene": "com.wt.scene",
    "WT_TinnoveCore3D": "com.tinnove.renderserver",
    "WT_LightSoundLab": "com.wt.lightsound",
    "WT_SmartSoundEffect": "com.autopai.smart.sound.effect",
    "WT_Spacecraft": "com.tinnove.spacecraft",
    "WT_ThemeResourcesDay": "com.autopai.theme.day",
    "WT_ThemeResourcesNight": "com.autopai.theme.night",
    "WT_VisualizationService": "com.tinnove.visualizationservice",
    "WT_VehicleCenter": "com.wt.vehiclecenter",
    "WT_MultiMediaCenter": "com.tinnove.mediacenter",
    "WT_MLWecarControl": "com.tinnove.wecarcontrol",
    "WT_MiniApp": "com.tinnove.miniapp",
    "WT_SpeedRun": "com.tinnove.speedrun",
    "WT_FusionNavigation": "com.tinnove.wecarnavi",
    "WT_InputMethod": "com.tinnove.inputmethod.pinyin",
    "WT_HDCloudCamera": "com.tinnove.cloudcamera",
    "WT_FileManager": "com.wtcl.filemanager",
    "WT_Album": "com.autopai.album",
    "WT_AppStore": "com.changan.appmarket",
    "WT_AutoMaintenance": "com.wt.maintenance",
    "WT_Customer": "com.tinnove.customer",
    "WT_DownloadLog": "com.smart.dvr.download.log",
    "WT_ECall": "com.adayo.app.ecall",
    "WT_ElectronicDirections": "com.wtcl.electronicdirections",
    "WT_FiveChess": "com.tinnove.fivechess",
    "WT_GameCenter": "com.wt.gamecenter",
    "WT_GameZone": "com.tinnove.gamezone",
    "WT_IncallFunBox": "com.wt.funbox",
    "WT_IncallLive": "com.incall.live",
    "WT_IncallPersonalCenter": "com.incall.apps.personalcenter",
    "WT_Launcher": "com.tinnove.launcher",
    "WT_AirConditioner": "com.wt.airconditioner",
    "WT_BTPhone": "com.autopai.car.dialer",
    "WT_AIAssistant": "com.tinnove.aiassistant",
    "WT_AISceneMode": "com.tinnove.scenemode",
    "WT_AccountServer": "com.autopai.accountserver",
    "WT_Wcenter": "com.tencent.wcenter",
    "WT_WtSystemUI": "com.android.systemui",
    "ldm": "com.goa.xlight",
    "fotaservice": "com.adayo.fotaservice",
    "deCoreApp": "com.vecentek.decoreapp",
    "CarService": "com.android.car",
    "CarPlay": "com.adayo.carplay",
    "CarPlayView": "com.adayo.carplay.view",
    "CarActivityResolver": "com.android.car.activityresolver",
    "CarFrameworkPackageStubs": "com.android.car.frameworkpackagestubs",
    "CaptivePortalLogin": "com.android.captiveportallogin",
    "ContactsProvider": "com.android.providers.contacts",
    "DownloadProvider": "com.android.providers.downloads",
    "DownloadProviderUi": "com.android.providers.downloads.ui",
    "DynamicSystemInstallationService": "com.android.dynsystem",
    "ExternalStorageProvider": "com.android.externalstorage",
    "InputDevices": "com.android.inputdevices",
    "MtpService": "com.android.mtp",
    "ManagedProvisioning": "com.android.managedprovisioning",
    "PackageInstaller": "com.android.packageinstaller",
    "SystemUpdater": "com.android.car.systemupdater",
    "BackupRestoreConfirmation": "com.android.backupconfirm",
    "CertInstaller": "com.android.certinstaller",
    "NetworkStack": "com.android.networkstack",
    "NfcService": "com.android.nfc",
    "Fota": "com.incall.apps.softmanager",
    "EMode": "com.adayo.app.emode",
    "HiSight": "com.huawei.hisight",
    "HiViewLite": "com.huawei.hiviewlite",
    "HwDMSDPDevice": "com.huawei.dmsdpdevice",
    "HwNearbyCar": "com.huawei.nearby",
    "AdayoAgnssService": "com.adayo.service.beidou",
    "AdayoAlarm": "com.adayo.service.alarm",
    "AdayoAPA": "com.adayo.app.apa",
    "AdayoDvr": "com.adayo.app.dvr",
    "AdayoDvrLocalService": "com.adayo.app.dvrlocalservice",
    "AdayoLog": "com.adayo.service.log",
    "AutoTest": "com.adayo.app.autotest",
    "Bluetooth": "com.android.bluetooth",
    "Camera": "com.adayo.app.camera",
    "FusedLocation": "com.android.location.fused",
    "KeyChain": "com.android.keychain",
    "MFiService": "com.adayo.mfiservice",
    "NoMicKtvService": "com.adayo.service.nomicktv",
    "PacProcessor": "com.android.pacprocessor",
    "PhoneDetect": "com.adayo.phonedetect.service",
    "PhoneLink": "com.adayo.phonelink",
    "Player857": "com.wali.ca.player857",
    "PowerManager": "com.adayo.app.powermanager",
    "ProxyHandler": "com.android.proxyhandler",
    "Puremic": "com.loostone.puremic",
    "SensetimeAiService": "com.senseauto.intellisense.aiservice",
    "SettingsProvider": "com.android.providers.settings",
    "Shell": "com.android.shell",
    "StatementService": "com.android.statementservice",
    "StorageWarn": "com.adayo.storageobserver",
    "Upgrade": "com.adayo.fotaapp",
    "UserDictionaryProvider": "com.android.providers.userdictionary",
    "VpnDialogs": "com.android.vpndialogs",
    "ExtShared": "android.ext.shared",
    "CACertService": "vendor.qti.hardware.cacert.server",
    "CtsShimPrebuilt": "com.android.cts.ctsshim",
    "CtsShimPrivPrebuilt": "com.android.cts.priv.ctsshim",
    "LocalTransport": "com.android.localtransport",
    "MmsService": "com.android.mms.service",
    "SecureElement": "com.android.se",
    "SharedStorageBackup": "com.android.sharedstoragebackup",
    "TimeService": "com.qualcomm.timeservice",
    "TrustZoneAccessService": "com.qualcomm.qti.qms.service.trustzoneaccess",
    "WallpaperBackup": "com.android.wallpaperbackup",
    "NetworkPermissionConfig": "com.android.networkstack.permissionconfig",
}


def _esc(s):
    return (s or "").replace("&", "&amp;").replace("<", "&lt;").replace(
        ">", "&gt;").replace("'", "&apos;").replace('"', "&quot;")


# элемент в значениях оверлея -> type в public.xml таргета
XML_ELEM_TO_PTYPE = {
    "string": "string",
    "plurals": "plurals",
    "string-array": "array",
    "integer": "integer",
    "bool": "bool",
    "color": "color",
    "dimen": "dimen",
    "style": "style",
}


def _overlay_resource_names(res_dir):
    """Собрать имена ресурсов оверлея по type из всех values*/.xml."""
    names = {}
    for xml in sorted(res_dir.glob("values*/*.xml")):
        try:
            src = xml.read_text(encoding="utf-8", errors="replace")
        except OSError:
            continue
        for elem, ptype in XML_ELEM_TO_PTYPE.items():
            for m in re.finditer(
                    r'<%s\b[^>]*?\bname="([A-Za-z0-9_.]+)"' % elem, src):
                names.setdefault(ptype, set()).add(m.group(1))
    return names


def _target_public_ids(app_name):
    """Парсить decompiled/<app>/res/values/public.xml -> {(type, name): id}."""
    f = DECOMPILED / app_name / "res" / "values" / "public.xml"
    if not f.exists():
        return None
    ids = {}
    for m in re.finditer(r'<public\b([^>]+)>',
                         f.read_text(encoding="utf-8", errors="replace")):
        attrs = dict(re.findall(r'(\w+)="([^"]*)"', m.group(1)))
        if attrs.get("id"):
            ids[(attrs.get("type"), attrs.get("name"))] = int(attrs["id"], 16)
    return ids


def write_resource_ids(app_name, res_dst):
    """Зафиксировать resId оверлея каноническими ID целевого пакета.

    Без этого aapt2 link (-I android.jar) выдаёт оверлею СВОИ порядковые
    ID (type id=01, алфавитный порядок), а RRO применяется по ID таргета —
    строки не совпадают по слотам, и любая подстановка в коде
    (getString(R.string.x, args)) читает чужое/пустое значение.
    Возвращает {(type, name): id} для post-link верификации (пусто — нет).
    """
    target = _target_public_ids(app_name)
    if target is None:
        print(f"  WARN {app_name}: нет decompiled/{app_name}/res/values/public.xml"
              " — resId НЕ закреплены (возможно рассогласование ID с таргетом)")
        return {}
    used = _overlay_resource_names(res_dst)
    pins = {}
    missing = []
    for ptype, names in used.items():
        for n in sorted(names):
            if (ptype, n) in target:
                pins[(ptype, n)] = target[(ptype, n)]
            else:
                missing.append((ptype, n))
    if not pins:
        print(f"  WARN {app_name}: ни одно имя ресурса не нашлось в public.xml таргета")
        return {}
    values_dir = res_dst / "values"
    values_dir.mkdir(exist_ok=True)
    out = ['<?xml version="1.0" encoding="utf-8"?>', "<resources>"]
    for (t, n), rid in sorted(pins.items(), key=lambda kv: kv[1]):
        out.append('    <public type="%s" name="%s" id="0x%08x" />' % (t, n, rid))
    out.append("</resources>")
    (values_dir / "public.xml").write_text("\n".join(out) + "\n", encoding="utf-8")
    msg = f"  pin resId: {len(pins)} ресурсов закреплено за canonical-ID таргета"
    if missing:
        sample = ", ".join(f"{t}/{n}" for t, n in missing[:5])
        msg += f" | нет в таргете: {len(missing)} ({sample}" + ("…)" if len(missing) > 5 else ")")
    print(msg)
    return pins


def _verify_resids(out_apk, app_name, expected):
    """Post-link: все закрепленные ресурсы в APK реально получили ID таргета."""
    if not expected:
        return True
    r = subprocess.run([AAPT2, "dump", "resources", str(out_apk)],
                       capture_output=True, text=True)
    if r.returncode != 0:
        print(f"  verify FAIL {app_name}: aapt2 dump: {r.stderr[-200:]}")
        return False
    got = {}
    for m in re.finditer(r'resource (0x[0-9a-fA-F]+) ([a-zA-Z-]+)/([A-Za-z0-9_.]+)',
                         r.stdout):
        got[(m.group(2), m.group(3))] = int(m.group(1), 16)
    bad = [(t, n, hex(rid),
            "missing" if got.get((t, n)) is None else hex(got[(t, n)]))
           for (t, n), rid in expected.items() if got.get((t, n)) != rid]
    if bad:
        print(f"  verify FAIL {app_name}: ID ресурсов не совпадают с таргетами"
              f" ({len(bad)}):")
        for t, n, want, have in bad[:10]:
            print(f"    {t}/{n}: ждали {want}, получили {have}")
        return False
    print(f"  verify OK: {len(expected)} resId совпадают с таргетом")
    return True


def _aapt2_too_large(r, where: str, app_name: str):
    """aapt2 молча (rc=0) подставляет STRING_TOO_LARGE строкам >32767 UTF-8
    байт, выводя warning в STDOUT: «error: string too large to encode using
    UTF-8 written instead as 'STRING_TOO_LARGE'». Без детекта сборка
    выглядела бы «100 OK», а на ГУ читалась бы заглушка. Возвращает True,
    если маркер есть — строитель обязан FAIL-нуть приложение."""
    blob = (r.stdout or "") + "\n" + (r.stderr or "")
    if "string too large" in blob.lower() or "STRING_TOO_LARGE" in blob:
        print(f"  {where} STRING_TOO_LARGE в {app_name}: aapt2 подставил "
              f"заглушку (строка >32767 UTF-8-байт).")
        return True
    return False


def build_rro(app_name, apk_output_path):
    """Build and sign a static RRO APK from pre-generated overlay structure in overlays_static/<app>.
    The <overlay> tag MUST have android:isStatic="true".
    """
    app_dir = OVERLAYS / app_name
    if not (app_dir / "AndroidManifest.xml").exists():
        print(f"  SKIP {app_name}: no overlay structure (run generate_overlays_static.py first)")
        return None

    build_dir = BUILD / f"build_{app_name}"
    if build_dir.exists():
        shutil.rmtree(build_dir)
    build_dir.mkdir(parents=True)

    # Copy overlay structure into clean build dir
    res_src = app_dir / "res"
    res_dst = build_dir / "res"
    if res_src.exists():
        shutil.copytree(res_src, res_dst)

    # Copy manifest
    shutil.copy2(app_dir / "AndroidManifest.xml", build_dir / "AndroidManifest.xml")

    # Зафиксировать resId оверлея за canonical-ID таргета
    # (иначе aapt2 link выдаст собственные ID, и resources не совпадут
    #  по слотам с resource-id'ми в smali таргета).
    expected_ids = write_resource_ids(app_name, res_dst)

    # Discover resource XML files: values-ru/ (RU-локаль) + values/ (дубль
    # RU как default-fallback для любой другой локали + закреплённые resId).
    # public.xml — только пины ID, тоже компилируется; остальное из values/
    # идёт как default-конфигурация (локаль не ru -> RU из values/).
    compile_sources = []
    for vdir in sorted(res_dst.glob("values*")):
        if not vdir.is_dir():
            continue
        for xml in sorted(vdir.glob("*.xml")):
            compile_sources.append(str(xml))

    if not compile_sources:
        print(f"  compile FAIL: no XML resources in {app_dir}/res/")
        shutil.rmtree(build_dir)
        return None

    # --- Compile each XML file individually ---
    for src in compile_sources:
        r = subprocess.run(
            [AAPT2, "compile", "-o", str(build_dir), src],
            capture_output=True, text=True)
        if r.returncode != 0:
            print(f"  compile FAIL ({src}): {r.stderr[-500:]}")
            shutil.rmtree(build_dir)
            return None
        if _aapt2_too_large(r, "compile", app_name):
            # rc=0 (warning), но строка не влезает — не «100 OK».
            print(f"  compile FAIL (STRING_TOO_LARGE, строка >32767 байт): {src}")
            shutil.rmtree(build_dir)
            return None

    flat_files = list(build_dir.rglob("*.flat"))
    if not flat_files:
        print(f"  no flat files")
        shutil.rmtree(build_dir)
        return None
    total_sz = sum(f.stat().st_size for f in flat_files)
    print(f"  compile {len(flat_files)} flat files: {total_sz} bytes")

    # --- Link ---
    manifest = build_dir / "AndroidManifest.xml"
    out_apk = build_dir / f"{app_name}_RRO.apk"
    cmd = [AAPT2, "link", "-o", str(out_apk), "-I", str(ANDROID_JAR),
           "--manifest", str(manifest),
           "--auto-add-overlay",
           "--no-resource-removal"]
    cmd.extend(str(f) for f in flat_files)
    r2 = subprocess.run(cmd, capture_output=True, text=True)
    if r2.returncode != 0:
        print(f"  link FAIL: {r2.stderr[-300:]}")
        shutil.rmtree(build_dir)
        return None
    if _aapt2_too_large(r2, "link", app_name):
        print(f"  link FAIL (STRING_TOO_LARGE, строка >32767 байт)")
        shutil.rmtree(build_dir)
        if out_apk.exists():
            out_apk.unlink()
        return None
    if not out_apk.exists() or out_apk.stat().st_size < 100:
        print(f"  link FAIL: empty apk")
        shutil.rmtree(build_dir)
        return None
    print(f"  link APK size: {out_apk.stat().st_size} bytes")

    # --- Verify: resId в собранном APK == ID таргета ---
    if not _verify_resids(out_apk, app_name, expected_ids):
        shutil.rmtree(build_dir)
        return None

    # --- Sign with platform.jks (same signature as target packages) ---
    signed = apk_output_path / f"{app_name}_RRO.apk"
    with tempfile.NamedTemporaryFile(suffix=".jks", delete=False) as tf:
        shutil.copy2(JKS, tf.name)
    r3 = subprocess.run(
        [APKSIGNER, "sign", "--ks", tf.name,
         "--ks-key-alias", "androiddebugkey",
         "--ks-pass", "pass:android", "--key-pass", "pass:android",
         "--out", str(signed), str(out_apk)],
        capture_output=True, text=True)
    os.unlink(tf.name)
    if r3.returncode != 0:
        print(f"  sign FAIL: {r3.stderr[-300:]}")
        shutil.rmtree(build_dir)
        return None
    if not signed.exists() or signed.stat().st_size < 100:
        print(f"  sign FAIL: empty")
        shutil.rmtree(build_dir)
        return None

    shutil.rmtree(build_dir)
    return signed, signed.stat().st_size


def main():
    import argparse
    parser = argparse.ArgumentParser(
        description="Build static RRO APKs from overlays_static/ and place them in apks_rro_static/.\n\n"
                    "To install on device:\n"
                    "  adb root && adb remount\n"
                    "  adb shell mkdir -p /vendor/overlay/<app_name>\n"
                    "  adb push <app_name>_RRO.apk /vendor/overlay/<app_name>/\n"
                    "  adb reboot\n"
                    "Or use /product/overlay/ instead of /vendor/overlay/.\n\n"
                    "Static overlays: activated on boot, hidden from app list, cannot be disabled by user.",
        formatter_class=argparse.RawDescriptionHelpFormatter
    )
    parser.add_argument("--only", default=None,
                        help="build only this app")
    args = parser.parse_args()

    print("=== Static RRO APK Builder (from overlays_static/) ===\n")
    BUILD.mkdir(parents=True, exist_ok=True)
    apk_output_path = APK_OUT
    apk_output_path.mkdir(parents=True, exist_ok=True)

    if not ANDROID_JAR.exists():
        print(f"ERROR: {ANDROID_JAR} not found")
        return
    if not OVERLAYS.exists():
        print(f"ERROR: {OVERLAYS} not found (run generate_overlays_static.py first)")
        print(f"  python3 generate_overlays_static.py")
        return

    apps = []
    for app_dir_name in sorted(OVERLAYS.iterdir()):
        if not app_dir_name.is_dir():
            continue
        app_name = app_dir_name.name
        if (app_dir_name / "AndroidManifest.xml").is_file():
            if args.only and app_name != args.only:
                continue
            apps.append(app_name)

    print(f"Apps to build: {len(apps)}\n")

    ok = 0
    failed = 0
    for app_name in apps:
        print(f"--- {app_name} ---")
        result = build_rro(app_name, apk_output_path)
        if result is not None:
            signed, sz = result
            if signed.exists():
                print(f"  OK: {sz / 1024:.0f} KB  (static: install via adb into /vendor/overlay/)")
                ok += 1
            else:
                failed += 1
        else:
            failed += 1

    total = sum(f.stat().st_size for f in apk_output_path.glob("*_RRO.apk")) / 1024
    files_count = len(list(apk_output_path.glob("*_RRO.apk")))
    print(f"\n=== DONE: {ok} OK, {failed} FAIL ===")
    print(f"Total RRO: {files_count} files, {total:.0f} KB")
    print(f"\nInstallation on device:")
    print(f"  adb root && adb remount")
    print(f"  adb shell mkdir -p /vendor/overlay/<app_name>")
    print(f"  adb push {apk_output_path}/<app_name>_RRO.apk /vendor/overlay/<app_name>/")
    print(f"  adb reboot")


if __name__ == "__main__":
    main()
