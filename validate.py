#!/usr/bin/env python3
"""Семантическая автопроверка проекта deepal-HU-translate.

Проверяет (в порядке пайплайна):

1. translations/*.json
    a. placeholder-parity zh<->ru: у переводов те же formatters, что у
       originals (%s, %1$d, %.1f, ...). Нерасходование formatters приводит к
       Android RuntimeException (IllegalFormatException) или к "raw %S" на экране.
    b. CJK in ru (модель вернула оригинал). Whitelist — реальные
       CJK wake-words/ключи, которые переводу НЕ подлежат.
    c. string-arrays: индексы непрерывны 0..N-1 и размер совпадает с
       источником (decompiled/<app>/res/values*/arrays.xml).
    (1c) ширина RU vs ZH коротких UI-строк — WARN-аудит (не ошибка): RU,
        заметно шире ZH, перенесётся на 2 строки в фиксированном виджете.
        Лечится improve_translations.py --fit. --length-report FILE — полный
        список; триаж совпадает с --fit (strwidth.py).

2. overlays/<app>/res/values-ru/  (динамические)
   a. размер string-array == размеру источника;
   b. plurals: few/many не пустые (иначе 2-4/5+ форма сломана);
   c. значения plurals/arrays не содержат CJK (кроме whitelist).

3. apks_rro_min/*.apk, apks_rro_static/*.apk (aapt2 dump resources)
   a. package name == com.android.vendor.translate.rro.<app> (динамические) /
      com.deepal.translate.rro.<app> (статические);
   b. тех же ресурсов, что в translations (ничего не потерялось);
   c. размеры массивов == размеру источника; plurals few/many не пустые;
   d. в дефолтном конфиге нет CJK (кроме whitelist).

Выход: 0 — всё чисто, 1 — найдены ошибки.

Использование:
    python3 validate.py                 # всё
    python3 validate.py --skip-apk      # без разобрки APK (быстрее)
    python3 validate.py --app WT_Launcher
    python3 validate.py --skip-apk --length-report logs/fit_report.txt
"""
from __future__ import annotations

import argparse
import json
import re
import subprocess
import sys
import xml.etree.ElementTree as ET
from collections import defaultdict
from pathlib import Path

from strwidth import display_width, hard_cap, is_ru_oversized

BASE = Path(__file__).resolve().parent
TRANS = BASE / "translations"
DECOMPILED = BASE / "decompiled"
OVERLAYS = BASE / "overlays"
OVERLAYS_STATIC = BASE / "overlays_static"
APK_MIN = BASE / "apks_rro_min"
APK_STATIC = BASE / "apks_rro_static"

CJK_RE = re.compile(r"[\u4e00-\u9fff]")
# formatters: %d %s %.1f %1$s %1$2d, %% — все валидные Android-форматтеры.
FORMATTER_RE = re.compile(r"%(\d+\$)?(\.?\d*)([dfs])|%%")
# aapt2 молча режет значение строки > 32767 UTF-8-байт в «STRING_TOO_LARGE»
# (warning, не error — сборка «100 OK», но на ГУ чтение-заглушка). Запас 32700 —
# то же, что в generate_overlays*.py (AAPT2_MAX_BYTES).
AAPT2_MAX_BYTES = 32700
# Заглушка, которую aapt2 подставляет на переполнившуюся строку (см. check_apk).
STRING_TOO_LARGE = "STRING_TOO_LARGE"


def esc(s: str) -> str:
    """Тот же _esc, что в generate_overlays*.py: значение, которое aapt2 увидит
    в plain-строке (styled — raw, без esc)."""
    return (s or "").replace("&", "&amp;").replace("<", "&lt;").replace(
        ">", "&gt;").replace("'", "&apos;").replace('"', "&quot;")


def effective_value_bytes(e: dict, ru: str) -> int:
    """Байтовый размер значения, которое aapt2 положит в ресурс:
    styled — raw (verbatim), plain — после esc(). Сверка с AAPT2_MAX_BYTES."""
    val = ru if e.get("styled") else esc(ru)
    return len(val.encode("utf-8"))

# Переводу не подлежит (голосовые wake-words / технический CJK):
CJK_WHITELIST = {
    ("WT_VehicleCenter", "Exterior_voice_interaction_title"): "voice wake-word 你好",
}

errors: list[tuple[str, str]] = []
warns: list[tuple[str, str]] = []


def err(tag: str, msg: str) -> None:
    errors.append((tag, msg))
    print(f"  [ERROR] {tag}: {msg}")


def warn(tag: str, msg: str) -> None:
    warns.append((tag, msg))
    print(f"  [WARN]  {tag}: {msg}")


def has_cjk(t) -> bool:
    return bool(t) and bool(CJK_RE.search(t))


def specs(s: str) -> list[str]:
    """Список Android-форматтеров строки по порядку вхождения: %s, %d, %.1f, %1$s, %1$2d, %%.

    Упорядоченно (не отсортировано): сравнение zh<->ru ловит не только разное
    число/тип форматтера, но и перестановку %1$s <-> %2$s."""
    return [m.group(0) for m in FORMATTER_RE.finditer(s or "")]


def load_translations(app: str | None) -> dict[str, list[dict]]:
    data = {}
    for fp in sorted(TRANS.glob("*.json")):
        if (fp.name.endswith("_progress.json") or fp.name in
                ("index.json", "extraction.json", "ambiguity_db.json", "exclude.json")):
            continue
        if app and fp.stem != app:
            continue
        try:
            d = json.loads(fp.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError) as e:
            err(fp.stem, f"bad JSON: {e}")
            continue
        if isinstance(d, list):
            data[fp.stem] = d
    return data


# --------------------------------------------------------------------------
# 1. translations/*.json
# --------------------------------------------------------------------------

def check_translations(trans: dict[str, list[dict]]) -> None:
    print("== 1. translations/*.json ==")
    n_str = n_bad_spec = n_cjk = n_arr = 0
    for app, entries in trans.items():
        for e in entries:
            name = e.get("name", "?")
            zh = e.get("zh") or ""
            ru = e.get("ru") or ""
            if not zh and not ru:
                continue
            n_str += 1
            # a. placeholder-parity
            if zh and ru and specs(zh) != specs(ru):
                n_bad_spec += 1
                err(
                    f"{app}/{name}",
                    f"форматтеры zh {specs(zh)} != ru {specs(ru)} | zh={zh[:40]!r} ru={ru[:40]!r}",
                )
            # b. CJK in ru
            if has_cjk(ru) and (app, name) not in CJK_WHITELIST:
                n_cjk += 1
                err(f"{app}/{name}", f"CJK in ru (модель вернула оригинал): {ru[:50]!r}")
    # b2. aapt2 byte-size (значение > 32767 байт -> «STRING_TOO_LARGE»).
    #     WARN: намеренно не идёт в оверлей (generate_overlays*.py его отбросит)
    #     -> на ГУ будет исходный текст, а не заглушка. Чтобы всё-таки
    #     перевести — укоротить ru (ru уже в JSON, повторный --styled/ --long).
    n_toobig = 0
    for app, entries in trans.items():
        for e in entries:
            ru = e.get("ru") or ""
            if not ru:
                continue
            if effective_value_bytes(e, ru) > AAPT2_MAX_BYTES:
                n_toobig += 1
                warn(f"{app}/{e.get('name','?')}",
                     f"ru не влезает в аapt2-лимит ({effective_value_bytes(e,ru)} байт)"
                     f" {AAPT2_MAX_BYTES}: оверлей без этой строки (текст источника)")
    if n_toobig:
        print(f"  не влезает в аapt2-лимит ({AAPT2_MAX_BYTES} байт): {n_toobig} (WARN, в оверлей не войдут)")
    print(f"  строк: {n_str}, форматтеров: {n_bad_spec}, CJK-in-ru: {n_cjk}")

    # c0. дубликаты name внутри приложения — ломают матчинг по (app,name),
    #     BД (UNIQUE) и overlay (aapt2: duplicate resource name)
    n_dup = 0
    for app, entries in trans.items():
        names = {}
        for e in entries:
            nm = e.get("name", "")
            if not nm:
                continue
            prev = names.get(nm)
            if prev is None:
                names[nm] = e
                continue
            n_dup += 1
            err(
                f"{app}/{nm}",
                f"дубликат name (zh={prev.get('zh','')[:20]!r} и {e.get('zh','')[:20]!r})",
            )
    if n_dup:
        print(f"  дубликатов name: {n_dup}")

    # c. arrays: непрерывность индексов
    for app, entries in trans.items():
        groups: dict[str, dict[int, dict]] = {}
        for e in entries:
            if e.get("type") != "arrays":
                continue
            m = re.fullmatch(r"(.+)_(\d+)", e.get("name", ""))
            if not m:
                err(f"{app}/{e.get('name')}", "array entry без _<idx> суффикса")
                continue
            groups.setdefault(m.group(1), {})[int(m.group(2))] = e
        for base, items in groups.items():
            idxs = sorted(items)
            if idxs != list(range(max(idxs) + 1)):
                missing = sorted(set(range(max(idxs) + 1)) - set(idxs))
                err(f"{app}/{base}", f"дыры в индексах (пропуски {missing})")
            n_arr += 1
    print(f"  массивов: {n_arr}")


# --------------------------------------------------------------------------
# 1c. аудит ширины RU vs ZH (короткие UI-строки)
# --------------------------------------------------------------------------

def check_ru_width(trans: dict[str, list[dict]],
                   report: str | None) -> None:
    """Предупреждение (НЕ ошибка): короткие UI-строки, у которых RU заметно
    шире ZH. RRO не меняет layout виджетов, поэтому такие переводы в
    фиксированной кнопке/лейбле переносятся на 2 строки вместо одной.
    Триаж = strwidth.is_ru_oversized (согласован с improve_translations --fit).

    report: если задан, полный список пишется в файл (по одной строке
    `app/name | zh[w] | ru[w,cap] | zh | ru`), иначе — только сводка +
    топ-15 приложений."""
    print("== 1c. ширина RU vs ZH (короткие UI-строки) [WARN-аудит] ==")
    n_total = n_bad = 0
    per_app: dict[str, int] = defaultdict(int)
    rows: list[tuple[int, str, str, int, int, int, str, str]] = []
    for app, entries in trans.items():
        for e in entries:
            name = e.get("name", "")
            zh = e.get("zh") or ""
            ru = e.get("ru") or ""
            if not zh or not ru:
                continue
            n_total += 1
            if not is_ru_oversized(zh, ru):
                continue
            zw, rw = display_width(zh), display_width(ru)
            n_bad += 1
            per_app[app] += 1
            rows.append((rw, app, name, zw, rw, hard_cap(zh), zh, ru))
    if n_bad:
        warn("ru-width",
             f"{n_bad} коротких строк шире порога (RU>2.5x ZH и >=24w) — "
             f"риск переноса на 2 строки; лечится improve_translations.py "
             f"--fit. Топ приложений: "
             + ", ".join(f"{a}={c}" for a, c in
                         sorted(per_app.items(), key=lambda x: -x[1])[:15]))
    if report and n_bad:
        rows.sort(key=lambda r: -r[0])
        with open(report, "w", encoding="utf-8") as f:
            f.write(f"# ширина RU vs ZH: {n_bad} строк (триаж strwidth)\n")
            f.write("# формат: app/name | zh[w] | ru[w,cap] | zh | ru\n")
            for rw, app, name, zw, _rw2, cap, zh, ru in rows:
                f.write(f"{app}/{name} | zh[{zw}] | ru[{_rw2},cap={cap}] | "
                        f"{zh} | {ru}\n")
        print(f"  полный список: {report}")
    print(f"  строк: {n_total}, перешироких: {n_bad} ({len(per_app)} приложений)")


# --------------------------------------------------------------------------
# исходные размеры массивов из decompiled/
# --------------------------------------------------------------------------

def source_arrays(app: str) -> dict[str, int]:
    """Размер каждого CJK-массива в источнике (values/ primary, потом zh-*)."""
    size: dict[str, int] = {}
    res = DECOMPILED / app / "res"
    if not res.is_dir():
        return size
    for loc in ("values", "values-zh-rCN", "values-zh-rTW", "values-zh-rHK", "values-zh"):
        p = res / loc / "arrays.xml"
        if not p.exists():
            continue
        try:
            root = ET.parse(str(p)).getroot()
        except ET.ParseError:
            continue
        for arr in root.iter("string-array"):
            name = arr.get("name")
            items = arr.findall("item")
            if not name or name in size or not items:
                continue
            if any(has_cjk(it.text or "") for it in items):
                size[name] = len(items)
    return size


def check_missing_cjk_resources(trans: dict[str, list[dict]]) -> None:
    """[WARN-аудит] CJK-ресурсы источника (values|values-zh*) , которых нет в
    translations/<app>.json — первый «ранний» сигнал потери при извлечении
    (баг 21.09: regex ([^<]*) extract_cjk.py рвал строки с вложенной разметкой,
    13 строк пропали; теперь извлечение через ET, но аудит не убираем).

    Зеркалим самого extract_cjk (его extract_app), а не переписываем логику:
    всё, что извлекатель находит, ОБЯЗАН быть в JSON."""
    print("== 1d. CJK-ресурсы источника, отсутствующие в JSON [WARN-аудит] ==")
    import extract_cjk  # локальный скрипт проекта; лениво — не тянуть при импорте
    n_apps = 0
    n_miss = 0
    for app_dir in sorted(DECOMPILED.iterdir()):
        if not app_dir.is_dir():
            continue
        app = app_dir.name
        try:
            res = extract_cjk.extract_app(app)
        except Exception:  # noqa: BLE001
            continue
        if not res:
            continue
        zh = res[0]
        if not zh:
            continue
        have = {e.get("name") for e in trans.get(app, [])}
        missing = sorted(n for n in zh if n not in have)
        if missing:
            n_apps += 1
            n_miss += len(missing)
            warn(app,
                 f"{len(missing)} CJK-ресурсов из источника нет в JSON "
                 f"(потеря при extract?): {', '.join(missing[:20])}")
    if n_apps:
        print(f"  приложений с потерями: {n_apps}, всего ресурсов: {n_miss}")
    else:
        print("  потерь нет")


def check_arrays_vs_source(trans: dict[str, list[dict]]) -> None:
    print("== 1b. массивы против источника (decompiled/) ==")
    n = 0
    for app, entries in trans.items():
        src = source_arrays(app)
        if not src:
            continue
        groups: dict[str, int] = defaultdict(int)
        for e in entries:
            if e.get("type") != "arrays":
                continue
            m = re.fullmatch(r"(.+)_(\d+)", e.get("name", ""))
            if m:
                groups[m.group(1)] = max(groups[m.group(1)], int(m.group(2)) + 1)
        for base, want in src.items():
            got = groups.get(base, 0)
            if got != want:
                err(f"{app}/{base}", f"размер в translations={got} != источнику {want}")
                continue
            n += 1
    print(f"  сверено {n} массивов")


# --------------------------------------------------------------------------
# 2. сгенерированные оверлеи
# --------------------------------------------------------------------------

def check_overlay_dir(root: Path, label: str, trans: dict[str, list[dict]]) -> None:
    print(f"== 2. {label} ==")
    if not root.is_dir():
        err(label, f"каталог не найден: {root}")
        return
    n_arr = n_plur = n_cjk = 0
    for app_dir in sorted(root.iterdir()):
        if not app_dir.is_dir():
            continue
        app = app_dir.name
        res = app_dir / "res"
        src = source_arrays(app)
        # arrays.xml
        ax = None
        for p in res.glob("values*/arrays.xml"):
            ax = p
            break
        if ax is not None:
            try:
                r = ET.parse(str(ax)).getroot()
            except ET.ParseError as e:
                err(f"{label}/{app}", f"arrays.xml: {e}")
                continue
            for arr in r.iter("string-array"):
                name = arr.get("name")
                items = arr.findall("item")
                if src and name in src and len(items) != src[name]:
                    err(f"{label}/{app}/{name}",
                        f"в оверлее {len(items)} items != {src[name]} в источнике")
                else:
                    n_arr += 1
                for it in items:
                    if has_cjk(it.text or ""):
                        n_cjk += 1
                        if (app, name) not in CJK_WHITELIST:
                            err(f"{label}/{app}/{name}", f"CJK в item: {(it.text or '')[:40]!r}")
        # plurals.xml
        px = None
        for p in res.glob("values*/plurals.xml"):
            px = p
            break
        if px is not None:
            try:
                r = ET.parse(str(px)).getroot()
            except ET.ParseError as e:
                err(f"{label}/{app}", f"plurals.xml: {e}")
                continue
            for pl in r.iter("plurals"):
                name = pl.get("name")
                qtys = {it.get("quantity"): (it.text or "") for it in pl.findall("item")}
                other = qtys.get("other", "")
                for q in ("few", "many"):
                    if q in qtys and qtys[q] == "":
                        err(f"{label}/{app}/{name}",
                            f'пустое <item quantity="{q}"> — форма сломана')
                for q, t in qtys.items():
                    if has_cjk(t) and (app, name) not in CJK_WHITELIST:
                        err(f"{label}/{app}/{name}", f"CJK в plural[{q}]")
                n_plur += 1
        # strings.xml — CJK check
        for sx in res.glob("values*/strings.xml"):
            try:
                r = ET.parse(str(sx)).getroot()
            except ET.ParseError as e:
                err(f"{label}/{app}", f"strings.xml: {e}")
                continue
            for s in r.iter("string"):
                t = s.text or ""
                if has_cjk(t):
                    nm = s.get("name", "?")
                    n_cjk += 1
                    if (app, nm) not in CJK_WHITELIST:
                        err(f"{label}/{app}/{nm}", f"CJK в ru: {t[:50]!r}")
    print(f"  массивов: {n_arr}, plurals: {n_plur}, CJK: {n_cjk}")


# --------------------------------------------------------------------------
# 3. APK
# --------------------------------------------------------------------------

# Схемы RRO (см. generate_overlays*.py):
#   apks_rro_min    — динамические, com.android.vendor.translate.rro.*
#                     (ЛАУНЧЕР СКРЫВАЕТ: filter #1 startsWith("com.android") —
#                     LAUNCHER_APP_HIDDEN_DESIGN.md)
#   apks_rro_static — статические, com.deepal.translate.rro.*
PKG_PREFIXES = {
    "apks_rro_min": "com.android.vendor.translate.rro.",
    "apks_rro_static": "com.deepal.translate.rro.",
}


def expected_app(pkg: str, prefix: str) -> str | None:
    m = re.fullmatch(re.escape(prefix) + r"(.+)", pkg)
    return m.group(1) if m else None


def parse_aapt2_dump(text: str) -> dict:
    """Парсит `aapt2 dump resources`: {type/name: {"values": [...], "array": N, "plurals": {q: v}}}.

    Строковые значения aapt2 печатает в кавычках и РЕЗАЕТ по строке при
   内含 \\n: `() "строка1\\nстрока2"` становится многострочным блоком,
    где последняя строка блока заканчивается `"`. Накопление идёт до
    строки, оканчивающейся на `"` (без учёта отступа).
    """
    out: dict[str, dict] = {}
    cur_res: str | None = None
    lines = text.splitlines()
    i = 0
    n = len(lines)
    while i < n:
        line = lines[i]
        i += 1
        if re.match(r"\s*type \w+ id=\w+ entryCount=", line):
            cur_res = None
            continue
        mr = re.match(r"\s*resource 0x[0-9a-f]+ (\w+)/(\S+)", line)
        if mr:
            cur_res = f"{mr.group(1)}/{mr.group(2)}"
            out[cur_res] = {"values": [], "array": None, "plurals": {}}
            continue
        if cur_res is None:
            continue
        entry = out[cur_res]
        # строковое значение: (qualifier) "начало...
        mv = re.match(r'\s*\([^)]*\) "(.*)$', line)
        if mv:
            buf = mv.group(1)
            # if the line ends with `"` — it's a full value
            while not buf.endswith('"') and i < n:
                buf += "\n" + lines[i].strip()
                i += 1
            if buf.endswith('"'):
                entry["values"].append(buf[:-1])
            else:
                entry["values"].append(buf)
            continue
        ma = re.match(r"\s*\([^)]*\) \(array\) size=(\d+)", line)
        if ma:
            entry["array"] = int(ma.group(1))
            continue
        # элементы массива (могут быть разнесены по строкам)
        stripped = line.strip()
        if stripped.startswith("["):
            buf = stripped
            while not buf.endswith("]") and i < n:
                buf += " " + lines[i].strip()
                i += 1
            mlist = re.match(r"\[(.*)\]$", buf)
            if mlist:
                entry.setdefault("array_items", []).append(mlist.group(1))
            continue
        # plurals: `one="..."`, `other="..."` ...
        mp = re.match(r'\s*(zero|one|few|many|other|dual)="(.*)"\s*$', line)
        if mp:
            entry["plurals"][mp.group(1)] = mp.group(2)
            continue
    return out


def check_apk(apk: Path, expect_app_name: str | None, src_sizes: dict[str, int],
              tag: str, prefix: str) -> None:
    r = subprocess.run(["aapt2", "dump", "resources", str(apk)],
                       capture_output=True, text=True)
    if r.returncode != 0:
        err(tag, f"aapt2 dump: {r.stderr[-200:]}")
        return
    m = re.search(r"Package name=(\S+)", r.stdout)
    pkg = m.group(1) if m else ""
    if not pkg.startswith(prefix):
        err(tag, f"package {pkg!r} — не {prefix}*")
        return
    pkg_app = expected_app(pkg, prefix)
    if expect_app_name and pkg_app is not None and pkg_app != expect_app_name.lower():
        err(tag, f"package для приложения не совпадает: {pkg_app} != {expect_app_name}")
    # whitelist сверяется по исходному (camel) имени приложения
    app = expect_app_name or (pkg_app or "")

    try:
        res = parse_aapt2_dump(r.stdout)
    except Exception as e:  # noqa: BLE001
        err(tag, f"ошибка парсинга aapt2 dump: {e}")
        return

    # strings
    n_toolarge = 0
    for name, d in res.items():
        t, nm = name.split("/", 1)
        for v in d["values"]:
            if v == STRING_TOO_LARGE:
                # aapt2 молча подставляет это значение строкам > 32767 UTF-8
                # байт (warning, не error) — в APK это «непереведённый текст»
                # на ГУ. Нормальный перевод так не называется -> ОШИБКА.
                n_toolarge += 1
                err(f"{tag}/{nm}",
                    f"STRING_TOO_LARGE в APK: строка >32767 UTF-8-байт; аapt2 "
                    f"подставил заглушку. Укоротить ru или split ресурса.")
            if has_cjk(v) and (app, nm) not in CJK_WHITELIST:
                err(f"{tag}/{nm}", f"CJK в APK: {v[:50]!r}")
    if n_toolarge:
        print(f"  STRING_TOO_LARGE (заглушка aapt2): {n_toolarge} — ОШИБКА")
    # arrays
    for name, d in res.items():
        t, nm = name.split("/", 1)
        if t == "array" and d["array"] is not None:
            if src_sizes and nm in src_sizes and d["array"] != src_sizes[nm]:
                err(f"{tag}/{nm}", f"размер {d['array']} != {src_sizes[nm]} в источнике")
            for raw_item in d.get("array_items", []):
                for it in re.findall(r'"([^"]*)"', raw_item):
                    if it == STRING_TOO_LARGE:
                        err(f"{tag}/{nm}", f"STRING_TOO_LARGE в item массива {nm}")
                    if has_cjk(it) and (app, nm) not in CJK_WHITELIST:
                        err(f"{tag}/{nm}", f"CJK в item: {it[:40]!r}")
    # plurals
    for name, d in res.items():
        t, nm = name.split("/", 1)
        if d["plurals"]:
            for q in ("few", "many"):
                if q in d["plurals"] and d["plurals"][q] == "":
                    err(f"{tag}/{nm}", f'пустое quantity="{q}" в APK')
            for q, v in d["plurals"].items():
                if v == STRING_TOO_LARGE:
                    err(f"{tag}/{nm}", f"STRING_TOO_LARGE в plural[{q}]")
                if has_cjk(v) and (app, nm) not in CJK_WHITELIST:
                    err(f"{tag}/{nm}", f"CJK в plural[{q}]: {v[:40]!r}")


def check_apks(trans: dict[str, list[dict]], only_app: str | None) -> None:
    for apks_dir, label in ((APK_MIN, "apks_rro_min"), (APK_STATIC, "apks_rro_static")):
        print(f"== 3. {label} ==")
        prefix = PKG_PREFIXES[label]
        if not apks_dir.is_dir():
            err(label, "каталог не найден")
            continue
        for apk in sorted(apks_dir.glob("*_RRO.apk")):
            app = apk.name.replace("_RRO.apk", "")
            if only_app and app != only_app:
                continue
            check_apk(apk, app, source_arrays(app), label, prefix)


# --------------------------------------------------------------------------

def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--skip-apk", action="store_true")
    ap.add_argument("--app", default=None, help="только одно приложение")
    ap.add_argument("--length-report", metavar="FILE", default=None,
                    help="выгрузить полный список перешироких RU-строк "
                         "(секция 1c) в FILE")
    args = ap.parse_args()

    print()
    trans = load_translations(args.app)
    check_translations(trans)
    check_ru_width(trans, args.length_report)
    check_arrays_vs_source(trans)
    check_missing_cjk_resources(trans)
    check_overlay_dir(OVERLAYS, "overlays", trans)
    check_overlay_dir(OVERLAYS_STATIC, "overlays_static", trans)
    if not args.skip_apk:
        check_apks(trans, args.app)

    print()
    print("=" * 70)
    print(f"Результат: {len(errors)} ОШИБОК, {len(warns)} предупреждений")
    if errors:
        print("Список ошибок:")
        for tag, msg in errors:
            print(f"  - {tag}: {msg}")
    print("=" * 70)
    return 1 if errors else 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except KeyboardInterrupt:
        sys.exit(130)
