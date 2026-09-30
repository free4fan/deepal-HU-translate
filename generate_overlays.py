#!/usr/bin/env python3
"""Generate overlay directory structures from translations/*.json into overlays/<app>/."""
import json, os, re, shutil, subprocess
from pathlib import Path

TRANS = Path("./translations")
OUT = Path("./overlays")

PLURAL_QTYS = ("one", "few", "many", "other")


def get_target_package(app_name):
    from create_rro_min import get_target_package as gtp
    return gtp(app_name)


def load_excludes():
    """translations/exclude.json: {app: {name: причина}} — строки, которые НЕ
    пишутся в оверлей (значения используются приложением как КЛЮЧИ/данные:
    сравнение getString() с кодом/данными, поэтому RU-перевод ломает логику).
    На ГУ у таких строк остаётся исходное (zh) значение."""
    f = TRANS / "exclude.json"
    try:
        return json.load(open(f, encoding="utf-8"))
    except (OSError, ValueError):
        return {}


def filter_excluded(translated, app_name, excludes):
    """Убрать из translated строки, перечисленные в exclude[app]."""
    ex = excludes.get(app_name)
    if not ex:
        return translated
    keep = [e for e in translated if e["name"] not in ex]
    drop = len(translated) - len(keep)
    if drop:
        print(f"  [EXCL] {app_name}: {drop} строк-ключей исключено из оверлея: "
              f"{', '.join(sorted(ex))}")
    return keep


def _esc(s):
    return (s or "").replace("&", "&amp;").replace("<", "&lt;").replace(
        ">", "&gt;").replace("'", "&apos;").replace('"', "&quot;")


def fmt_string_xml(name, ru):
    if '%' in ru or '"' in ru:
        return f'    <string name="{name}" formatted="false">{_esc(ru)}</string>'
    return f'    <string name="{name}">{_esc(ru)}</string>'


AAPT2_MAX_BYTES = 32700
# aapt2: значение строки > 32767 UTF-8 байт -> строка МОЛЧА заменяется на
# "STRING_TOO_LARGE" (aapt2 warning, не ошибка). Проверено: 32767 OK, 32768
# FAIL; одинаковый лимит и для plain, и для span/styled. Запас -> 32700.


def fmt_styled_xml(name, ru):
    """Rich text с разметкой (<b>/<a>/<annotation>/<Data>...&lt;br/&gt;...).

    Записываем ru VERBATIM — БЕЗ _esc(): экранирование превратило бы теги в
    литеральный текст. aapt2 сам парсит спаны. formatted="false" — защита от
    printf-интерпретации '%' (у таких строк форматтеров нет).

    Возвращает None, если строка не влезает в лимит aapt2 — тогда строку НЕ
    пишем в оверлей (на ГУ останется исходный текст, а не "STRING_TOO_LARGE")."""
    if len((ru or "").encode("utf-8")) <= AAPT2_MAX_BYTES:
        return f'    <string name="{name}" formatted="false">{ru}</string>'
    # Обёртка <Data>...</Data> = «внутри лежит литеральный HTML» (внутренние
    # &lt;/&amp; уже XML-экранированы) — храним вложенное содержимое ОБЫЧНОЙ
    # строкой VERBATIM (БЕЗ _esc, иначе двойное экранирование): Resources.
    # getString вернёт те же символы, что span; приложение прогоняет через
    # Html.fromHtml (см. smali: ServiceProtocolPage getString → fromHtml).
    m = re.match(r"^<Data>(.*)</Data>$", ru or "", re.S)
    if m:
        inner = m.group(1)
        if "<" not in inner and len(inner.encode("utf-8")) <= AAPT2_MAX_BYTES:
            return f'    <string name="{name}" formatted="false">{inner}</string>'
    return None


def fmt_plain_checked(name, ru):
    """Обычная строка + проверка лимита aapt2. None — не влезает (не писать)."""
    if len(_esc(ru).encode("utf-8")) > AAPT2_MAX_BYTES:
        return None
    return fmt_string_xml(name, ru)


def get_manifest_target(app_name):
    """For overlay: targetPackage goes in <overlay> tag, not <manifest> package."""
    return get_target_package(app_name)


# Таргеты, которые в КОДЕ (smali) принудительно ставят Locale.CHINA на свои
# Resources (Configuration.setLocale / createConfigurationContext) и тем самым
# сбрасывают выбор локали при любом config-change/перезапуске окна: RU из
# values/ (default) и values-ru/ для них НЕ видна — ресурс-система берёт
# значение из values-zh-rCN/ (точное совпадение zh-rCN выше default), а если
# его нет — из source values/ (кит. по умолчанию). Чтобы у таких таргетов
# перевод не «слетал на китайский», mirror-копируем RU-файлы также в
# res/values-zh-rCN/ (значения идентичны, ID те же).
# Список пополнять только по доказанному smali-следствию (Audit-скан:
# grep "Locale;->CHINA" smali/** + Configuration;->setLocale/createConfigurationContext).
ZH_MIRROR_APPS = ("WT_AirConditioner",)


def generate_app(app_name, translations_data):
    """Generate files for one app in overlays/<app>/."""
    app_dir = OUT / app_name
    if app_dir.exists():
        shutil.rmtree(app_dir)
    
    strings_data = []
    plurals_data = {}
    arrays_list = []
    
    for e in translations_data:
        name = e["name"]
        ru = e.get("ru", "")
        t = e.get("type", "")
        
        if t == "plurals":
            # plurals from extract_cjk.py are keyed as name_<qty>
            # but we may also get them grouped — try to split
            if "_" in name:
                base = "_".join(name.split("_")[:-1])
                qty = name.rsplit("_", 1)[-1]
            else:
                base = name
                qty = "other"
            plurals_data.setdefault(base, {})[qty] = ru
        elif t == "arrays":
            arrays_list.append(e)
        else:
            strings_data.append(e)
    
    res_dir = app_dir / "res"
    values_ru_dir = res_dir / "values-ru"
    values_ru_dir.mkdir(parents=True, exist_ok=True)
    # values/ (default config) — ДУБЛЬ тех же строк без локали:
    # RU-перевод применяется даже когда locale ГУ НЕ ru (fallback на default
    # config вместо zh-строки в values/ у WT-* и вместо en-fallback у AOSP-*).
    values_dir = res_dir / "values"
    values_dir.mkdir(parents=True, exist_ok=True)

    has_arrays = bool(arrays_list)
    has_plurals = bool(plurals_data)
    has_strings = bool(strings_data)

    files = {}
    skipped_toolong = []

    # strings.xml
    # (RU в values-ru/ для ru-локали + в values/ как default-fallback на
    # любую другую локаль; значения одинаковые)
    # Строки, не влезавшие в лимит aapt2 (>32700 bytes), НЕ пишем — иначе
    # aapt2 подставит "STRING_TOO_LARGE"; на ГУ тогда останется исходный текст.
    if has_strings:
        ru_lines = ['<?xml version="1.0" encoding="utf-8"?>', '<resources>']
        for e in strings_data:
            if e.get("styled"):
                line = fmt_styled_xml(e["name"], e.get("ru", ""))
            else:
                line = fmt_plain_checked(e["name"], e.get("ru", ""))
            if line is None:
                skipped_toolong.append(e["name"])
                continue
            ru_lines.append(line)
        ru_lines.append("</resources>")
        files["strings.xml"] = "\n".join(ru_lines)

    # plurals.xml
    # Only write quantity items we actually translated. Missing few/many must
    # NOT be empty: an empty <item quantity="few"> overrides the source's
    # fallback and the app shows an empty string for 2-4/5+ items. Copy the
    # "other" text as the fallback value.
    if has_plurals:
        plurals_lines = ['<?xml version="1.0" encoding="utf-8"?>', '<resources>']
        for base, qtys in plurals_data.items():
            other = qtys.get("other", "")
            plurals_lines.append(f'    <plurals name="{base}">')
            for q in PLURAL_QTYS:
                text = qtys.get(q) or other
                plurals_lines.append(f'    <item quantity="{q}">{_esc(text)}</item>')
            plurals_lines.append('    </plurals>')
        plurals_lines.append("</resources>")
        files["plurals.xml"] = "\n".join(plurals_lines)

    # arrays.xml
    # Emit items contiguously from 0..max_index (empty <item></item> kept):
    # a missing index would shift all later items relative to the target app.
    if has_arrays:
        arrays_lines = ['<?xml version="1.0" encoding="utf-8"?>', '<resources>']
        array_groups = {}
        for e in arrays_list:
            m = re.match(r'^(.+?)_(\d+)$', e["name"])
            base, idx = (m.group(1), int(m.group(2))) if m else (e["name"], None)
            array_groups.setdefault(base, {})[idx] = e
        for base_name, items in array_groups.items():
            arrays_lines.append(f'    <string-array name="{base_name}">')
            if None in items:
                # array stored as a single entry without index — emit as-is
                e = items[None]
                text = (e.get("ru") or e.get("zh") or "").strip()
                arrays_lines.append(f'        <item>{_esc(text)}</item>')
            else:
                for idx in range(max(items) + 1):
                    e = items.get(idx)
                    text = ""
                    if e is not None:
                        text = (e.get("ru") or "").strip() or (e.get("zh") or "").strip()
                    arrays_lines.append(f'        <item>{_esc(text)}</item>')
            arrays_lines.append('    </string-array>')
        arrays_lines.append("</resources>")
        files["arrays.xml"] = "\n".join(arrays_lines)

    for fname, content in files.items():
        (values_ru_dir / fname).write_text(content, encoding="utf-8")
        (values_dir / fname).write_text(content, encoding="utf-8")
    if app_name in ZH_MIRROR_APPS:
        # зеркалируем RU в values-zh-rCN/ — см. ZH_MIRROR_APPS выше
        values_zh_dir = res_dir / "values-zh-rCN"
        values_zh_dir.mkdir(parents=True, exist_ok=True)
        for fname, content in files.items():
            (values_zh_dir / fname).write_text(content, encoding="utf-8")
    if skipped_toolong:
        print(f"  [WARN] {app_name}: не влезли в лимит aapt2 (оставлен источник): "
              f"{', '.join(skipped_toolong)}")
    
    # Write AndroidManifest.xml (RRO format).
    # Package prefix com.android.vendor.* is INTENTIONAL (LAUNCHER_APP_HIDDEN_DESIGN.md,
    # filter #1 in CommonUtil.loadAllApps(): name.startsWith("com.android") -> skip):
    # it keeps the dozen+ installed overlay APKs out of the user's app list.
    # The dynamic-scheme bat scripts (install_8/all, disable_8, manage, diag)
    # resolve the overlay package as com.android.vendor.translate.rro.<app>.
    # NOTE: the static scheme deliberately uses a different prefix
    # (com.deepal.translate.rro.*, see generate_overlays_static.py).
    target = get_manifest_target(app_name)
    manifest = f"""<?xml version="1.0" encoding="utf-8"?>
<manifest xmlns:android="http://schemas.android.com/apk/res/android"
    package="com.android.vendor.translate.rro.{app_name.lower()}">
    <application android:hasCode="false" />
    <overlay
        android:targetPackage="{target}"
        android:priority="1" />
</manifest>
"""
    (app_dir / "AndroidManifest.xml").write_text(manifest, encoding="utf-8")
    
    return {
        "has_strings": has_strings,
        "has_plurals": has_plurals,
        "has_arrays": has_arrays,
        "target": target,
    }


def main():
    print("=== Overlay Generator ===\n")
    OUT.mkdir(parents=True, exist_ok=True)
    excludes = load_excludes()
    
    apps = []
    count = 0
    skipped = 0
    not_found = 0
    
    for fname in sorted(os.listdir(TRANS)):
        if not fname.endswith(".json") or fname in ("index.json", "extraction.json", "ambiguity_db.json", "exclude.json") or fname.endswith("_progress.json"):
            continue
        app_name = fname.replace(".json", "")

        data = json.load(open(os.path.join(TRANS, fname), encoding="utf-8"))
        if not isinstance(data, list):
            continue
        
        translated = [e for e in data if e.get("ru") and e["ru"].strip()]
        translated = filter_excluded(translated, app_name, excludes)
        if not translated:
            skipped += 1
            continue
        
        target = get_target_package(app_name)
        if not target:
            print(f"WARN {app_name}: no package target")
            not_found += 1
            continue
        
        result = generate_app(app_name, translated)
        apps.append(app_name)
        count += 1
        
        files = len(list((OUT / app_name).rglob("*")))
        print(f"  {app_name}: {files} files, target={target}")
    
    print(f"\n=== DONE: {count} apps generated, {skipped} skipped, {not_found} missing target ===")


if __name__ == "__main__":
    main()
