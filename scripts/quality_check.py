#!/usr/bin/env python3
"""Quality check: find untranslated CJK, duplicate translations, CJK in ru."""
import json, re, sys
from pathlib import Path
from collections import Counter

TRANS_DIR = Path("/home/user/projects/deepal-HU-translate/translations")

def is_cjk(t):
    if not t: return False
    return any(0x4E00 <= ord(c) <= 0x9FFF for c in t)

def has_cyrillic(t):
    if not t: return False
    return any(0x0400 <= ord(c) <= 0x052F for c in t)

def is_shortcut(t):
    """Identify hardware shortcut strings (should not be translated)."""
    s = t.strip()
    if not s: return False
    # Alt+, Ctrl+, Fn+, Meta+, Shift+, Sym+, Menu+ (with/without space, with/without + at end)
    if re.match(r'^[A-Z]+\s*[+-]?\s*\+?\s*$', s): return True
    if re.match(r'^[A-Z]+\s+\+?\s*$', s): return True
    if re.match(r'^[A-Z]+\+$', s): return True
    # Pure number with optional #
    if re.match(r'^\d+[+#]?\s*$', s): return True
    # Hex color
    if re.match(r'^#[0-9A-Fa-f]+$', s): return True
    # Pure Android package
    if re.match(r'^com\.[a-zA-Z][a-zA-Z0-9_.]*$', s): return True
    # Short service-like names (2-10 chars, all caps)
    if re.match(r'^[A-Z]{2,10}$', s) and len(s) < 12: return True
    return False

def is_format_string(t):
    if not t: return False
    return bool(re.search(r'%[0-9]*[sd].*', t))

issues = []
stats = {"apps": 0, "total": 0, "zh_only": 0, "en_only": 0, "ru_only": 0,
         "zh_ru": 0, "en_ru": 0, "all_three": 0, "cjk_in_ru": 0,
         "bad_translations": 0, "untranslated_cjk": 0,
         "non_translatable_skip": 0}

for fname in sorted(TRANS_DIR.glob("*.json")):
    if fname.name.endswith("_progress.json") or fname.name == "index.json":
        continue
    app_name = fname.stem
    with open(fname, encoding="utf-8") as f:
        data = json.load(f)
    if not isinstance(data, list):
        print(f"[WARN] {app_name}: bad JSON")
        continue
    
    stats["apps"] += 1
    app_zh_only = []
    app_buggy = []
    
    for i, e in enumerate(data):
        zh = (e.get("zh") or "").strip()
        en = (e.get("en") or "").strip()
        ru = (e.get("ru") or "").strip()
        
        stats["total"] += 1
        
        zh_has = bool(zh)
        en_has = bool(en)
        ru_has = bool(ru)
        
        if zh_has and not en_has and not ru_has:
            stats["zh_only"] += 1
        elif en_has and not zh_has and not ru_has:
            stats["en_only"] += 1
        elif ru_has and not zh_has and not en_has:
            stats["ru_only"] += 1
        elif zh_has and ru_has and not en_has:
            stats["zh_ru"] += 1
        elif en_has and ru_has and not zh_has:
            stats["en_ru"] += 1
        elif zh_has and en_has and ru_has:
            stats["all_three"] += 1
        
        if ru and is_cjk(ru):
            stats["cjk_in_ru"] += 1
            app_buggy.append((i, f"CJK in ru: {ru[:40]}"))
        
        if zh_has and not ru:
            if is_shortcut(zh) or is_format_string(zh):
                stats["non_translatable_skip"] += 1
            else:
                app_zh_only.append(i)
                stats["untranslated_cjk"] += 1
                if len(app_zh_only) <= 5:
                    issues.append((app_name, i, f"zh untranslated: {zh[:60]}"))
        
        if zh_has and ru_has and is_cjk(ru) and not has_cyrillic(ru):
            stats["bad_translations"] += 1
            app_buggy.append((i, f"not_cyrillic: {ru[:40]}"))
    
    if app_zh_only:
        print(f"[INFO] {app_name}: {len(app_zh_only)} untranslated CJK entries")
    if app_buggy:
        print(f"[WARN] {app_name}: {len(app_buggy)} buggy ru entries")
        for i, desc in app_buggy[:3]:
            print(f"  - {desc}")

print()
print("=" * 80)
print("TOTALS:")
print(f"  Apps: {stats['apps']}")
print(f"  Total entries: {stats['total']}")
print()
print("  Distribution:")
print(f"    zh_only:        {stats['zh_only']}")
print(f"    en_only:        {stats['en_only']}")
print(f"    ru_only:        {stats['ru_only']}")
print(f"    zh+ru:          {stats['zh_ru']}")
print(f"    en+ru:          {stats['en_ru']}")
print(f"    all three:      {stats['all_three']}")
print()
print(f"  Untranslated CJK: {stats['untranslated_cjk']}")
print(f"  Non-translatable skipped: {stats['non_translatable_skip']}")
print(f"  CJK in ru (bad): {stats['cjk_in_ru']}")
print(f"  Bad translations (not cyrillic): {stats['bad_translations']}")
print("=" * 80)

# Save detailed report
import json as json_mod
report = {
    "stats": stats,
    "sample_zh_only": issues[:50],
}
with open("/tmp/quality_report.json", "w", encoding="utf-8") as f:
    json_mod.dump(report, f, ensure_ascii=False, indent=2)

print("\nDetailed report saved to /tmp/quality_report.json")
