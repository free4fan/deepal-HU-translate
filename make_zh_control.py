#!/usr/bin/env python3
"""Контроль «структура vs контент» для WT_AutoMaintenance.

Собирает RRO с ТАКИМИ ЖЕ 269 IDтаргета, но значениями ИЗ ЦЕЛИ (values/strings.xml
таргета = то, что было в zero-прогоне). Если данные появятся — виноват КОНТЕНТ
наших RU-строк (бисекция по строкам). Если нет — виновата сама таблица оверлея
(размер/структура) → приложение из RRO выносится.

Арг: make_zh_control.py [N]  — первые N строк (по умолчанию все 269);
     имя вывода: WT_AutoMaintenance_RRO_zh<N>.apk
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

N = int(sys.argv[1]) if len(sys.argv) > 1 else None

ov = (ROOT / "overlays" / APP / "res/values-ru/strings.xml").read_text(encoding="utf-8")
names = [re.search(r'name="([^"]+)"', e).group(1)
         for e in re.findall(r'    <string\b.*?</string>\n', ov)]
if N:
    names = names[:N]

tgt_default = (ROOT / "decompiled" / APP / "res/values/strings.xml").read_text(encoding="utf-8", errors="replace")
zh = {}
for m in re.finditer(r'<string\b[^>]*\bname="([^"]+)"[^>]*>(.*?)</string>', tgt_default, re.S):
    zh[m.group(1)] = m.group(2)
miss = [n for n in names if n not in zh]
assert not miss, f"нет значений у цели: {miss[:5]}"

pub = (ROOT / "decompiled" / APP / "res/values/public.xml").read_text(encoding="utf-8", errors="replace")
pubmap = {}
for m in re.finditer(r'<public\b([^>]+)>', pub):
    a = dict(re.findall(r'(\w+)="([^"]*)"', m.group(1)))
    if a.get("type") == "string" and a.get("id"):
        pubmap[a["name"]] = int(a["id"], 16)
pins = sorted((pubmap[n], n) for n in names)

def _esc(s):
    return (s or "").replace("&", "&amp;").replace("<", "&lt;").replace(
        ">", "&gt;").replace("'", "&apos;").replace('"', "&quot;")

# строки с % или " в zh-значении — с formatted="false" (как у generate_overlays)
lines = []
for n in names:
    v = zh[n]
    attr = ' formatted="false"' if ("%" in v or '"' in v) else ""
    lines.append(f'    <string name="{n}"{attr}>{_esc(v)}</string>')

valru = '<?xml version="1.0" encoding="utf-8"?>\n<resources>\n' + "\n".join(lines) + "\n</resources>\n"
pubxml = ('<?xml version="1.0" encoding="utf-8"?>\n<resources>\n'
          + "".join('    <public type="string" name="%s" id="0x%08x" />\n' % (n, p) for p, n in pins)
          + "</resources>\n")
man = f'''<?xml version="1.0" encoding="utf-8"?>
<manifest xmlns:android="http://schemas.android.com/apk/res/android"
    package="{PKG}">
    <application android:hasCode="false" />
    <overlay
        android:targetPackage="com.wt.maintenance"
        android:priority="1" />
</manifest>
'''

tag = f"zh{N}" if N else "zh269"
b = ROOT / "overlay_build" / f"zhctrl_{tag}"
if b.exists():
    shutil.rmtree(b)
(b / "res/values-ru").mkdir(parents=True)
(b / "res/values").mkdir()
(b / "AndroidManifest.xml").write_text(man, encoding="utf-8")
(b / "res/values-ru/strings.xml").write_text(valru, encoding="utf-8")
(b / "res/values/public.xml").write_text(pubxml, encoding="utf-8")

r = subprocess.run([AAPT2, "compile", "-o", str(b),
                    str(b / "res/values-ru/strings.xml"), str(b / "res/values/public.xml")],
                   capture_output=True, text=True)
if r.returncode != 0:
    sys.exit(f"compile FAIL: {r.stderr[-400:]}")
flats = list(b.rglob("*.flat"))
apk = b / "out.apk"
r2 = subprocess.run([AAPT2, "link", "-o", str(apk), "-I", ANDROID_JAR,
                     "--manifest", str(b / "AndroidManifest.xml"),
                     "--auto-add-overlay", "--no-resource-removal"]
                    + [str(f) for f in flats], capture_output=True, text=True)
if r2.returncode != 0:
    sys.exit(f"link FAIL: {r2.stderr[-400:]}")
r3 = subprocess.run([AAPT2, "dump", "resources", str(apk)], capture_output=True, text=True)
got = {n: int(rid, 16) for rid, n in
       re.findall(r'resource (0x[0-9a-f]+) string/([A-Za-z0-9_.]+)', r3.stdout)}
bad = [n for p, n in pins if got.get(n) != p]
if bad:
    sys.exit(f"VERIFY FAIL {len(bad)}: {bad[:5]}")
signed = OUT / f"WT_AutoMaintenance_RRO_{tag}.apk"
r4 = subprocess.run([APKSIGNER, "sign", "--ks", str(JKS), "--ks-key-alias", "androiddebugkey",
                     "--ks-pass", "pass:android", "--key-pass", "pass:android",
                     "--out", str(signed), str(apk)], capture_output=True, text=True)
if r4.returncode != 0:
    sys.exit(f"sign FAIL: {r4.stderr[-400:]}")
shutil.rmtree(b)
print(f"OK {signed.name}: {len(names)} строк, значения = таргет {names[0]} .. {names[-1]}")
