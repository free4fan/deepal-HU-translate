#!/usr/bin/env python3
"""Бисекция симптомов WT_AutoMaintenance: N кусковых RRO с ЧАСТЬЮ строк.

Собирает apks_rro_min/WT_AutoMaintenance_RRO_t<i>.apk (i=1..N): тот же
package/manifest, что у основного RRO, но в values-ru только свой кусок строк
(порядок = порядок в overlays/WT_AutoMaintenance/res/values-ru/strings.xml).
Все ID зачищены canonical-ID таргета (public.xml). Используется для поиска
проблемного ресурса: ставится КУСОК поверх zero-состояния, смотрим, ломает ли
он данные (placeholder - - вместо значений).
"""
import re, shutil, subprocess, sys
from pathlib import Path

APP = "WT_AutoMaintenance"
ROOT = Path(__file__).parent
AAPT2 = "/usr/bin/aapt2"
APKSIGNER = "/usr/bin/apksigner"
ANDROID_JAR = "/opt/android-sdk/platforms/android-34/android.jar"
JKS = ROOT / "keys/platform.jks"
OUT = ROOT / "apks_rro_min"
PKG = "com.android.vendor.translate.rro.wt_automaintenance"
# АРГ: make_bisect_overs.py [K] [A] [B] [NAME]
#   K — на сколько кусков режем (по умолчанию 4)
#   A..B — срезы по СОРТИРОВАННОМУ (ID) списку (по умолчанию весь список, [0,None))
#   NAME — суффикс имени (по умолчанию t) — чтобы куски из разных прогонов не путались
N_CHIPS = int(sys.argv[1]) if len(sys.argv) > 1 else 4
A = int(sys.argv[2]) if len(sys.argv) > 2 else 0
B = int(sys.argv[3]) if len(sys.argv) > 3 else None
NAME = sys.argv[4] if len(sys.argv) > 4 else "t"

ov_xml = (ROOT / "overlays" / APP / "res/values-ru/strings.xml").read_text(encoding="utf-8")
entries = re.findall(r'    <string\b.*?</string>\n', ov_xml)
print(f"total strings: {len(entries)}, range=[{A}:{B}]")
entries = entries[A:B]
assert entries, "no entries"

pub = (ROOT / "decompiled" / APP / "res/values/public.xml").read_text(encoding="utf-8", errors="replace")
pubmap = {}
for m in re.finditer(r'<public\b([^>]+)>', pub):
    a = dict(re.findall(r'(\w+)="([^"]*)"', m.group(1)))
    if a.get("type") == "string" and a.get("id"):
        pubmap[a["name"]] = int(a["id"], 16)

def names_of(ch):
    out = []
    for e in ch:
        nm = re.search(r'name="([^"]+)"', e).group(1)
        out.append(nm)
    return out

size = len(entries) // N_CHIPS
chunks = [entries[i * size:(i + 1) * size] if i < N_CHIPS - 1 else entries[i * size:]
          for i in range(N_CHIPS)]

MANIFEST = f'''<?xml version="1.0" encoding="utf-8"?>
<manifest xmlns:android="http://schemas.android.com/apk/res/android"
    package="{PKG}">
    <application android:hasCode="false" />
    <overlay
        android:targetPackage="com.wt.maintenance"
        android:priority="1" />
</manifest>
'''

work = ROOT / "overlay_build"
for i, ch in enumerate(chunks, 1):
    nm = names_of(ch)
    pins = sorted({(pubmap[n], n) for n in nm if n in pubmap})
    if len(pins) != len(nm):
        missing = [n for n in nm if n not in pubmap]
        print(f"chip{i}: WARN no id for {missing[:5]}... ({len(missing)})")
    b = work / f"bisect_{NAME}{i}"
    if b.exists():
        shutil.rmtree(b)
    b.mkdir(parents=True)
    (b / "AndroidManifest.xml").write_text(MANIFEST, encoding="utf-8")
    res = b / "res"
    (res / "values-ru").mkdir(parents=True)
    (res / "values").mkdir()
    (res / "values-ru/strings.xml").write_text(
        '<?xml version="1.0" encoding="utf-8"?>\n<resources>\n'
        + "".join(ch) + "</resources>\n", encoding="utf-8")
    (res / "values/public.xml").write_text(
        '<?xml version="1.0" encoding="utf-8"?>\n<resources>\n'
        + "".join('    <public type="string" name="%s" id="0x%08x" />\n' % (n, pid)
                  for pid, n in pins)
        + "</resources>\n", encoding="utf-8")
    r = subprocess.run([AAPT2, "compile", "-o", str(b),
                        str(res / "values-ru/strings.xml"),
                        str(res / "values/public.xml")],
                       capture_output=True, text=True)
    if r.returncode != 0:
        print(f"chip{i} compile FAIL: {r.stderr[-300:]}"); continue
    flats = list(b.rglob("*.flat"))
    apk = b / f"WT_AutoMaintenance_RRO_{NAME}{i}.apk"
    r2 = subprocess.run([AAPT2, "link", "-o", str(apk), "-I", ANDROID_JAR,
                         "--manifest", str(b / "AndroidManifest.xml"),
                         "--auto-add-overlay", "--no-resource-removal"]
                        + [str(f) for f in flats], capture_output=True, text=True)
    if r2.returncode != 0:
        print(f"chip{i} link FAIL: {r2.stderr[-300:]}"); continue
    # verify: все id чипа == id таргета
    r3 = subprocess.run([AAPT2, "dump", "resources", str(apk)],
                        capture_output=True, text=True)
    got = {n: int(rid, 16) for rid, n in
           re.findall(r'resource (0x[0-9a-f]+) string/([A-Za-z0-9_.]+)', r3.stdout)}
    bad = [n for pid, n in pins if got.get(n) != pid]
    if bad:
        print(f"chip{i} VERIFY FAIL {len(bad)}: {bad[:5]}"); continue
    signed = OUT / f"WT_AutoMaintenance_RRO_{NAME}{i}.apk"
    r4 = subprocess.run([APKSIGNER, "sign", "--ks", str(JKS),
                         "--ks-key-alias", "androiddebugkey",
                         "--ks-pass", "pass:android", "--key-pass", "pass:android",
                         "--out", str(signed), str(apk)],
                        capture_output=True, text=True)
    if r4.returncode != 0:
        print(f"chip{i} sign FAIL: {r4.stderr[-300:]}"); continue
    shutil.rmtree(b)
    print(f"chip{i}: {len(nm)} строк  {nm[0]} .. {nm[-1]}  -> {signed.name}")
print("done")
