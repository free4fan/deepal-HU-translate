#!/usr/bin/env python3
"""Extract strings for translation.

Reads CJK strings from values/ and zh-* directories, and corresponding
Russian CJK strings from values-ru. Outputs translations/<app>.json.

ВНИМАНИЕ (21.09): повторный запуск ОБНУЛЯЕТ все `ru` (переводы сбрасываются)
и ПЕРЕЗАПИСЫВАЕТ translations/*.json целиком (main() пишет каждый файл
заново, поле `ru` пустое). Гонять только при ЯВНОМ намерении переизвлечь
(добавились новые приложения/фразы в decompiled/) и ТОЛЬКО ЕСЛИ
improve_progress.json не содержит строк, которых больше нет в decompiled/
(иначе ru потерянных строк не восстановится, improve не узнает их).
Сейчас (21.09) извлечение 1:1 совпадает с текущим JSON: 17 832 plain +
13 styled (см. CHANGELOG [2026-09-21]).
"""
import json
import os
import re
import xml.etree.ElementTree as ET
from pathlib import Path

# Resolve project root relative to script location
SCRIPT_DIR = Path(__file__).resolve().parent
DECOMPILED = SCRIPT_DIR / "decompiled"
TRANS = SCRIPT_DIR / "translations"

def _has_cjk(text):
    return bool(text) and any(0x4E00 <= ord(c) <= 0x9FFF for c in text)

def _is_name_metadata(name):
    # Only exclude package_name (Android system metadata)
    # app_name is now included — it's the visible app title in zh/ru
    if name == "package_name":
        return True
    # Exclude Android class names (Activity/Service with dots)
    if name.lower().endswith("_activity") or name.lower().endswith("_service"):
        if "." in name or "/" in name:
            return True
    return False

def _xml_text_esc(s):
    """Экранирование ТЕКСТОВОЙ части XML (то, что требует парсер)."""
    return (s or "").replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;")

def _xml_esc_min(s):
    """Минимальное экранирование ТЕКСТОВОЙ части по конвенции apktool/aapt2:
    & -> &amp;, < -> &lt;, а '>' в тексте НЕ экранируется (остается литеральным).

    ET.tostring делал бы ' > ' -> ' &gt; ', что для 9 WTN-строк с <Data>
    (&lt;strong> в исходнике с литеральной '>') дало бы дрейф zh
    (&lt;strong> -> &lt;strong&gt;). Поэтому сериализуем вручную."""
    return (s or "").replace("&", "&amp;").replace("<", "&lt;")

def _xml_attr_esc(s):
    return (s or "").replace("&", "&amp;").replace("<", "&lt;").replace(
        ">", "&gt;").replace('"', "&quot;")

def _string_inner_xml(el):
    """Сырая разметка внутри <string>…</string> (спаны/сущности), побайто
    идентичная значению в исходном XML-файле (zero-drift к 17 832 переводам).

    Вручную обходим ET-дерево: каждый дочерний элемент -> <tag attr="..">..</tag>,
    текст/хвосты -> _xml_esc_min. CDATA из decompiled/*/values*/strings.xml НЕ
    встречается (проверено grep'ом), поэтому её не обрабатываем.

    ДВА бага, от которых защищаюсь (найдены песочной сверкой, 21.09):
    (1) ET.tostring(c) САМ печатает c.tail — при ручном повторном дописывании
        tail значение ДУБИРОВАЛОСЬ в конце; здесь tail пишется ровно один раз.
    (2) ET.tostring экранирует ' > ' -> ' &gt; ' в тексте; в исходнике '>' после
        '&lt;' остаётся литеральным — вручную используем _xml_esc_min."""
    out: list[str] = []

    def emit(node):
        # Пустой элемент (без дочек и без текста) -> self-closing `<br/>`
        # (конвенция apktool/aapt2 и ET.tostring по умолчанию short_empty
        # _elements=True). Иначе вышло бы `<br></br>` — дрейф от исходника.
        if len(list(node)) == 0 and not node.text:
            out.append("<" + node.tag)
            for k, v in node.attrib.items():
                out.append(f' {k}="{_xml_attr_esc(v)}"')
            out.append("/>")
            return
        out.append("<" + node.tag)
        for k, v in node.attrib.items():
            out.append(f' {k}="{_xml_attr_esc(v)}"')
        out.append(">")
        if node.text:
            out.append(_xml_esc_min(node.text))
        for c in node:
            emit(c)
            if c.tail:
                out.append(_xml_esc_min(c.tail))
        out.append("</" + node.tag + ">")

    if el.text:
        out.append(_xml_esc_min(el.text))
    for c in el:
        emit(c)
        if c.tail:
            out.append(_xml_esc_min(c.tail))
    return "".join(out)

def _has_nested_markup(el):
    """Значение содержит дочерние элементы (спаны: <b>, <a>, <annotation>,
    <Data>…) — переводить только по фрагментам (см. improve_translations --styled)."""
    for c in el:
        if isinstance(c.tag, str):
            return True
    return False

def _iter_string_entries(root):
    """(name, text, styled) по всем <string> в паршенном root."""
    for el in root.iter("string"):
        name = el.get("name")
        if not name:
            continue
        if _has_nested_markup(el):
            # raw БЕЗ strip(): побайто идентичен значению в исходном XML
            # (Fota/upgrade_service_agreement_content начинается с пробела —
            # текущий zh в JSON и перенесённые ru-переводы сверены именно
            # с этой формой; strip менял бы zh и рассинхронил бы токены ru).
            inner = _string_inner_xml(el)
            visible = "".join(el.itertext()).strip()
            if inner and visible:
                yield name, inner, True
        else:
            # Plain-строка: возвращаем file-форму (эксаны &amp; &lt; &gt; как
            # в исходном XML) — побайтово так, как это хранили старые
            # regex-извлечение и текущие 17 832 переводов. Иначе повторный
            # extract_cjk менял бы zh существ. строк (сыр. <strong> вместо
            # &lt;strong>) и вёл бы к лишним перезначениям/guard-расхожд.
            # (Путь styled отдельный: _string_inner_xml -> file-форма со spanами.)
            text = _xml_text_esc((el.text or "")).strip()
            if text:
                yield name, text, False

def _extract_strings(xml_path):
    """Extract name/text from <string> elements only (not array/plural items).

    Гибрид (21.09, zero-drift):
    (1) PLAIN-строки — старый raw-regex `([^<]*)` НА БИТАХ ФАЙЛА. ET (unesc
        &lt;/&amp;) + re-esc даёт дрейф для 19 строк (литеральные '>' ->
        '&gt;' и т.п.), поэтому plain-путь НЕ трогаем: 17 832 существующих
        значений zh побайтово как раньше.
    (2) MARKUP-строки (дочерние элементы: <b>/<a>/<annotation>/<Data>…) —
        regex (1) их РВЁТ/СКИБАЕТ ([^<]* на первом '<'): раньше такие строки
        НИКОГДА не извлекались (13 шт.: WT_FusionNavigation clause_*/policy_*,
        Fota upgrade_service_agreement_content, CarService imsi_protection_
        warning, PackageInstaller uninstall_application_text_all_users,
        ManagedProvisioning read_more_delete_profile). Забираем через ET:
        text = RAW-разметка значения (file-форма, _string_inner_xml) и
        `styled: True` (переводить по фрагментам, improve_translations --styled).
    При ParseError файла — только (1), как до 21.09.
    """
    if not xml_path.exists():
        return []
    try:
        content = xml_path.read_text(encoding="utf-8")
    except Exception:
        return []
    results = []
    seen = set()
    for m in re.finditer(r'<string\b[^>]*?name="([^"]+)"[^>]*?>([^<]*)</', content):
        name, text = m.group(1), m.group(2).strip()
        if text and not text.startswith('<'):
            results.append({"name": name, "type": "string", "text": text})
            seen.add(name)
    try:
        root = ET.fromstring(content)
    except ET.ParseError:
        return results
    for name, text, styled in _iter_string_entries(root):
        if not styled or name in seen:
            continue
        if text:
            results.append({"name": name, "type": "string", "text": text,
                            "styled": True})
            seen.add(name)
    return results

def _extract_arrays(xml_path):
    """Extract array items with index suffixes using ElementTree.

    Extracts ALL items from any string-array that contains at least one CJK item.
    This ensures that numbers like "12:30" alongside CJK "关闭" are all extracted.

    Indices follow SOURCE positions (0..N-1), including empty <item></item>.
    Dropping empty items would shift every later index and misalign the
    generated overlay with the target app's array.

    21.09: текст пункта — через itertext() (полный, включая вложенное); пункт
    со спанами получает `styled`.
    """
    if not xml_path.exists():
        return []
    results = []
    try:
        root = ET.parse(str(xml_path)).getroot()
        for arr in root.iter("string-array"):
            arr_name = arr.get("name")
            if not arr_name:
                continue
            all_items = arr.findall("item")
            if not all_items:
                continue
            def _item_cjk(item):
                return _has_cjk((item.text or "").strip()) or _has_cjk("".join(item.itertext()))
            if not any(_item_cjk(item) for item in all_items):
                continue
            for idx, item_child in enumerate(all_items):
                text = "".join(item_child.itertext()).strip()
                entry = {"name": f"{arr_name}_{idx}",
                         "type": "arrays",
                         "text": text}
                if _has_nested_markup(item_child):
                    entry["styled"] = True
                results.append(entry)
    except Exception:
        pass
    return results

def _extract_plurals(xml_path):
    """Extract plural items with quantity suffixes.

    21.09: ET-парсинг (был regex `([^<]*)` на items — рвал вложенную разметку);
    text — itertext(); item со спанами получает `styled`.
    """
    if not xml_path.exists():
        return []
    try:
        content = xml_path.read_text(encoding="utf-8")
        root = ET.fromstring(content)
        results = []
        for pl in root.iter("plurals"):
            plural_name = pl.get("name")
            if not plural_name:
                continue
            for item in pl.findall("item"):
                qty = item.get("quantity", "other")
                text = "".join(item.itertext()).strip()
                if not text:
                    continue
                entry = {"name": f"{plural_name}_{qty}", "type": "plurals", "text": text}
                if _has_nested_markup(item):
                    entry["styled"] = True
                results.append(entry)
        return results
    except Exception:
        pass
    return []

def extract_app(app_name):
    """Extract zh CJK + ru CJK strings from all resources XML files."""
    app_dir = DECOMPILED / app_name
    res_dir = app_dir / "res"
    if not res_dir.exists():
        return None

    # zh: values/ first (primary), then zh-rCN, zh-rTW, zh-rHK, zh
    zh_dirs = [
        res_dir / "values",
        res_dir / "values-zh-rCN",
        res_dir / "values-zh-rTW",
        res_dir / "values-zh-rHK",
        res_dir / "values-zh",
    ]

    zh_texts = {}
    names_seen = set()

    # strings.xml — only <string> elements
    for xml_file in ("strings.xml",):
        for loc in zh_dirs:
            xml_path = loc / xml_file
            if not xml_path.exists():
                continue
            for entry in _extract_strings(xml_path):
                name, text = entry["name"], entry["text"]
                if _has_cjk(text) and not _is_name_metadata(name):
                    if name not in names_seen:
                        zh_texts[name] = {"text": text, "type": entry["type"],
                                          "styled": entry.get("styled", False)}
                        names_seen.add(name)

    # arrays.xml — <string-array> items
    # Arrays are extracted in full (not just CJK items) after _extract_arrays
    # determines the array has at least one CJK item
    for xml_file in ("arrays.xml",):
        for loc in zh_dirs:
            xml_path = loc / xml_file
            if not xml_path.exists():
                continue
            for entry in _extract_arrays(xml_path):
                name = entry["name"]
                if _is_name_metadata(name):
                    continue
                if name not in names_seen:
                    zh_texts[name] = {"text": entry["text"], "type": entry["type"],
                                      "styled": entry.get("styled", False)}
                    names_seen.add(name)

    # plurals.xml — <plurals> items
    for xml_file in ("plurals.xml",):
        for loc in zh_dirs:
            xml_path = loc / xml_file
            if not xml_path.exists():
                continue
            for entry in _extract_plurals(xml_path):
                name, text = entry["name"], entry["text"]
                if _has_cjk(text) and not _is_name_metadata(name):
                    if name not in names_seen:
                        zh_texts[name] = {"text": text, "type": entry["type"],
                                          "styled": entry.get("styled", False)}
                        names_seen.add(name)

    # ru: only keep entries with CJK (real translations, not metadata)
    ru_texts = {}
    loc_ru = res_dir / "values-ru"
    if loc_ru.exists():
        for xml_file in ("strings.xml",):
            xml_path = loc_ru / xml_file
            if xml_path.exists():
                for entry in _extract_strings(xml_path):
                    name = entry["name"]
                    text = entry["text"]
                    if name in zh_texts and _has_cjk(text):
                        ru_texts[name] = text
        for xml_file in ("arrays.xml",):
            xml_path = loc_ru / xml_file
            if xml_path.exists():
                for entry in _extract_arrays(xml_path):
                    name = entry["name"]
                    text = entry["text"]
                    if name in zh_texts and _has_cjk(text):
                        ru_texts[name] = text
        for xml_file in ("plurals.xml",):
            xml_path = loc_ru / xml_file
            if xml_path.exists():
                for entry in _extract_plurals(xml_path):
                    name = entry["name"]
                    text = entry["text"]
                    if name in zh_texts and _has_cjk(text):
                        ru_texts[name] = text

    return zh_texts, ru_texts

def main():
    TRANS.mkdir(parents=True, exist_ok=True)
    apps = 0
    total_zh = 0
    total_ru = 0

    for app_name in sorted(os.listdir(DECOMPILED)):
        app_path = DECOMPILED / app_name
        if not app_path.is_dir():
            continue
        # Check if truly decompiled (has apktool.yml in decompiled/<app>/)
        yml_path = app_path / "apktool.yml"
        if not yml_path.exists():
            continue

        result = extract_app(app_name)
        if result is None:
            continue

        zh_texts, ru_texts = result
        if not zh_texts:
            continue

        items = []
        for name, zh_data in zh_texts.items():
            zh = zh_data["text"]
            item = {"name": name, "zh": zh, "ru": "", "type": zh_data["type"]}
            if zh_data.get("styled"):
                item["styled"] = True
            if name in ru_texts:
                item["ru"] = ru_texts[name]
            items.append(item)

        output_path = TRANS / f"{app_name}.json"
        with output_path.open("w", encoding="utf-8") as f:
            json.dump(items, f, ensure_ascii=False, indent=2)
            f.write("\n")

        total_zh += len(zh_texts)
        total_ru += len(ru_texts)
        apps += 1
        print(f"{app_name}: {len(zh_texts)} zh, {len(ru_texts)} already with ru")

    print(f"\nTotal: {apps} apps, {total_zh} zh entries, {total_ru} already translated")

if __name__ == "__main__":
    main()
