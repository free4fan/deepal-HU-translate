#!/usr/bin/env python3
"""Deinitialize decompiled/<>, unmount raw images, then decompile fresh APKs from original_apks/.

Usage:
    python3 scripts/decompile_apks.py                    # decompile ALL apps in original_apks/
    python3 scripts/decompile_apks.py --list-only        # show APK list, don't decompile
    python3 scripts/decompile_apks.py --skip-existing    # skip APKs already decompiled (fast resume)
"""
import os
import sys
import json
import shutil
import subprocess
import signal
from pathlib import Path
from concurrent.futures import ThreadPoolExecutor, as_completed

BASE_DIR = Path(__file__).resolve().parent.parent
ORIG_APKS_DIR = BASE_DIR / "original_apks"
DECOMPILED_DIR = BASE_DIR / "decompiled"
MOUNT_VENDOR = BASE_DIR / "extracted" / "mnt_vend"
MOUNT_SYSTEM = BASE_DIR / "extracted" / "mnt_sys"
VENDOR_RAW = BASE_DIR / "extracted" / "vendor.raw"
SYSTEM_RAW = BASE_DIR / "extracted" / "system.raw"

APKTOOL_JAR = Path("/home/user/apktool.jar")
ANDROID_SDK = Path("/opt/android-sdk")
ANDROID_JAR = ANDROID_SDK / "platforms" / "android-34" / "android.jar"
FRAMEWORK_DIR = Path.home() / ".local" / "share" / "apktool" / "framework"

MAX_WORKERS = 1  # apktool must run sequentially (creates dirs in parallel)
FRAMEWORK_APIS = {}
errors = []  # captured via threads
print_lock = None  # will init in main


def find_original_apk(app_name):
    """Find the APK file for a given app name in original_apks/."""
    apk_candidates = [
        ORIG_APKS_DIR / f"{app_name}.apk",
        ORIG_APKS_DIR / f"{app_name.lower()}.apk",
    ]
    for candidate in apk_candidates:
        if candidate.exists():
            return candidate
    return None


def list_apks():
    """List all APK filenames in original_apks/."""
    return sorted([f.replace(".apk", "") for f in os.listdir(ORIG_APKS_DIR)
                    if f.endswith(".apk")])


def ensure_apktool_jar():
    """Download apktool jar if missing."""
    if APKTOOL_JAR.exists():
        return
    print("⏳ apktool.jar missing, download from official site...")
    # Not implemented in this script — user must place apktool.jar manually.
    sys.exit(1)


def ensure_framework_installation(api_version):
    """Ensure Android framework is installed for APKTool."""
    if api_version in FRAMEWORK_APIS:
        return True
    # apktool's framework dir contains the compiled framework.apk
    # Usually already installed; just check that the framework dir exists
    if api_version == 30:
        target = FRAMEWORK_DIR / "1.apk"
    elif api_version == 31:
        target = FRAMEWORK_DIR / "1.apk"
    elif api_version == 33:
        target = FRAMEWORK_DIR / "1.apk"
    else:
        target = FRAMEWORK_DIR / "1.apk"
    if not target.exists():
        print(f"⚠️ apktool framework not installed for API {api_version}, trying install")
        try:
            subprocess.run(
                ["java", "-jar", str(APKTOOL_JAR), "if", str(target),
                 "--force"],
                check=False, capture_output=True, timeout=60
            )
        except Exception as e:
            print(f"⚠️ framework install failed: {e}")
    return True


def get_min_sdk_from_apktool_yml(app_name):
    """Extract minSdkVersion from existing apktool.yml if available."""
    yml_path = DECOMPILED_DIR / app_name / "apktool.yml"
    if not yml_path.exists():
        return None
    try:
        with open(yml_path, "r", encoding="utf-8") as f:
            data = f.read()
        for line in data.splitlines():
            if line.startswith("minSdkVersion:"):
                return int(line.split(":", 1)[1].strip())
    except Exception:
        pass
    return None


def decompile_apk(app_name, android_version=None):
    """Decompile a single APK via apktool."""
    app_dir = DECOMPILED_DIR / app_name
    apk = ORIG_APKS_DIR / f"{app_name}.apk"
    if not apk.exists():
        return app_name, f"APK not found: {apk}"

    # Check if already decompiled - if so but corrupted, delete and re-decompile
    if app_dir.exists():
        yml = app_dir / "apktool.yml"
        if yml.exists():
            # Try reading version - if broken, remove and re-decompile
            try:
                with open(yml, "r", encoding="utf-8") as f:
                    head = f.read(200)
                if "apkFileName" not in head and "version" not in head:
                    print(f"⚠️ corrupt yml, remove: {app_name}", flush=True)
                    shutil.rmtree(app_dir)
            except Exception:
                print(f"⚠️ can't read yml, remove: {app_name}", flush=True)
                shutil.rmtree(app_dir)
        elif app_dir.is_dir():
            # Has a directory but no apktool.yml - leave as missing
            pass

    # Try aapt2 to get minSdkVersion if apktool.yml not available
    if android_version is None:
        # Try quick scan with aapt2
        try:
            r = subprocess.run(
                ["aapt2", "dump", "badging", str(apk)],
                capture_output=True, text=True, timeout=15
            )
            for line in r.stdout.splitlines():
                if "min-sdk-version:" in line.lower() and "-->" in line:
                    part = line.split("-->")[1].strip('"').strip()
                    try:
                        android_version = int(part.split(",")[0])
                    except (ValueError, IndexError):
                        pass
        except Exception:
            pass
        if android_version is None:
            android_version = 30  # safe default

    # Install framework for this version (best effort)
    ensure_framework_installation(android_version)

    app_dir.mkdir(parents=True, exist_ok=True)
    print(f"⏳ decompile: {app_name} (API {android_version})")

    out_dir = str(app_dir)
    try:
        r = subprocess.run(
            ["java", "-jar", str(APKTOOL_JAR), "d",
             str(apk), "-o", out_dir, "-f"],
            capture_output=True, text=True, timeout=300
        )
        if r.returncode == 0:
            # Check that we got apktool.yml
            yml = Path(out_dir) / "apktool.yml"
            if yml.exists():
                return app_name, "ok"
            return app_name, "no_apktool_yml"
        return app_name, f"apktool_err {r.stderr[:200]}"
    except subprocess.TimeoutExpired:
        return app_name, "timeout"
    except Exception as e:
        return app_name, f"exception: {e}"


def main():
    import argparse
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--list-only", action="store_true",
                        help="just show APK list")
    parser.add_argument("--skip-existing", action="store_true",
                        help="skip APKs that are already decompiled")
    parser.add_argument("--api", type=int, default=None,
                        help="force Android API version for all APKs")
    parser.add_argument("--workers", type=int, default=MAX_WORKERS,
                        help=f"parallel workers (default: {MAX_WORKERS})")
    args = parser.parse_args()

    ensure_apktool_jar()
    DECOMPILED_DIR.mkdir(parents=True, exist_ok=True)

    apps = list_apks()
    print(f"=== Found {len(apps)} APKs in {ORIG_APKS_DIR} ===\n")

    if args.list_only:
        for a in apps:
            print(f"  {a}")
        return

    # Decide: decompile all, or skip existing
    pending = []
    for a in apps:
        app_dir = DECOMPILED_DIR / a
        if args.skip_existing and app_dir.exists() and (app_dir / "apktool.yml").exists():
            print(f"⏩ skip (existing): {a}")
            continue
        pending.append(a)

    if not pending:
        print("Nothing to decompile — all done!")
        return

    print(f"=== Decompiling {len(pending)} APKs with {args.workers} workers ===\n")

    ok = 0
    skip = 0
    failed = []

    with ThreadPoolExecutor(max_workers=args.workers) as pool:
        futures = {}
        for a in pending:
            f = pool.submit(decompile_apk, a, args.api)
            futures[f] = a

        completed = 0
        for f in as_completed(futures):
            completed += 1
            app_name, status = f.result()
            if status == "ok":
                ok += 1
                print(f"  ✅ {app_name}")
            elif status == "already_decompiled":
                skip += 1
            else:
                failed.append((app_name, status))
                print(f"  ❌ {app_name}: {status}")

            if completed % 20 == 0:
                print(f"  [{completed}/{len(pending)}] "
                      f"ok={ok} fail={len(failed)}")

    print(f"\n=== DONE: {ok} ok, {skip} skip, {len(failed)} fail ===")
    if failed:
        print("\nFailed:")
        for a, s in failed:
            print(f"  {a}: {s}")


if __name__ == "__main__":
    main()
