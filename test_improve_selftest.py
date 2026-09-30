#!/usr/bin/env python3
"""Self-test improve_translations.py: guard, sanitize и aapt2 round-trip.

Проверяет НЕ сам API, а всё остальное: что ответ модели, принятый guard'ом и
нормализованный sanitize_ru, корректно пропустит generate_overlays._esc и
скомпилируется aapt2 (compile+link), и что значение, прочитанное из APK
aapt2 dump, равно ожидаемому «сырому» тексту. XML в тесте строится через
fmt_string_xml как в реальном пайплайне. exit 0 — всё ок.
"""
from __future__ import annotations

import json
import re
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

BASE = Path(__file__).resolve().parent
sys.path.insert(0, str(BASE))

from improve_translations import guard, specs, WAKE_WORD_RE, is_target  # noqa: E402
from improve_translations import (  # noqa: E402
    _styled_cjk_frags, _styled_check, _styled_reassemble, _styled_toks,
    _styled_tokenize,
)
from generate_overlays import _esc, fmt_string_xml, fmt_styled_xml  # noqa: E402
from generate_overlays import AAPT2_MAX_BYTES, fmt_plain_checked  # noqa: E402
from strwidth import display_width, hard_cap, is_ru_oversized  # noqa: E402

ANDROID_JAR = "/opt/android-sdk/platforms/android-34/android.jar"
results: list[tuple[str, bool, str]] = []


def check(name: str, cond: bool, info: str = "") -> None:
    results.append((name, bool(cond), info))
    print(f"  [{'OK ' if cond else 'FAIL'}] {name}" + (f" — {info}" if info and not cond else ""))


# ---------------------------------------------------------------------------
# 1. specs
# ---------------------------------------------------------------------------
def test_specs() -> None:
    print("\n== specs ==")
    check("%s", specs("продолжить %s с") == ["%s"])
    check("%d + %s", specs("%d и %s") == ["%d", "%s"])
    # упорядоченно по вхождению (не отсортировано)
    check("numbered", specs("%1$s и %2$d и %1$2d") == ["%1$s", "%2$d", "%1$2d"])
    check("precision", specs("высота %.1f м") == ["%.1f"])
    check("percent", specs("100%%") == ["%%"])
    check("mixed all", specs("%s %d %.1f %1$s %2$d %%") ==
          ["%s", "%d", "%.1f", "%1$s", "%2$d", "%%"])


# ---------------------------------------------------------------------------
# 2. guard
# ---------------------------------------------------------------------------
def test_guard() -> None:
    print("\n== guard ==")
    zh = "继续行程\xa0%sS"
    check("ok: простой перевод с %s",
          guard("A", "n", zh, "Продолжить маршрут %s с")[1] is None)
    check("empty -> отклонено", guard("A", "n", zh, "   ")[1] == "empty")
    v, r = guard("A", "n", zh, "Продолжить маршрут")
    check("потерян %s -> отклонено", v is None and r and "формatters" in r, str(r))
    v, r = guard("A", "n", "a%s%s", "Первый %s, второй %s %s")
    check("лишний %s -> отклонено", v is None and r and "формatters" in r, str(r))
    check("numbered ок", guard("B", "m", "到%1$s全程%2$s花费%3$s",
                               "До %1$s путь %2$s время %3$s")[1] is None)
    v, r = guard("B", "m", "到%1$s全程%2$s", "До %2$s путь %1$s")
    check("переставлены номера -> отклонено", v is None and r and "формatters" in r, str(r))

    zh_wake = '“开启后，可通过 “***你好” 在车外唤醒语⾳助⼿”'
    v, r = guard("WT_VehicleCenter", "Exterior_voice_interaction_title",
                 zh_wake, "После включения сказав «***你好»» разбудите помощника")
    check("wake-word сохранён (whitelist)", v is not None, str(r))
    v, r = guard("WT_VehicleCenter", "Exterior_voice_interaction_title",
                 zh_wake, "После включения сказав «привет» разбудите помощника")
    check("wake-word потерян -> отклонено", v is None and r and "wake-word" in r, str(r))
    v, r = guard("WT_TSpeech", "x", "前方有%s", "Впереди %s，预计需要")
    check("CJK в ru -> отклонено", v is None and r and "CJK" in r, str(r))
    check("чистый ru принят", guard("WT_TSpeech", "y", "前方有%s", "Впереди %s")[0] == "Впереди %s")

    v, r = guard("C", "s", "开启%s功能", '&lt;a href=&quot;https://x.y&quot;&gt;Вкл&lt;/a&gt; %s')
    check("двойное экранирование -> сырой текст",
          v == '<a href="https://x.y">Вкл</a> %s', f"v={v!r} r={r}")
    v, r = guard("C", "t", "它's", "It's ok")
    check("' -> U+2019", v == "It\u2019s ok", f"v={v!r}")
    v, r = guard("C", "u", "第一行%s", "Строка1\n%s")
    check("живой \\n -> литеральный \\\\n", v == "Строка1\\n%s", f"v={v!r} r={r}")
    v, r = guard("C", "f", "得分%s%%", "Оценка %s%%")
    check("%% сохранён", v == "Оценка %s%%", f"v={v!r} r={r}")

    long_zh = "很" * 500  # len(zh)=500 >= LONG_THRESHOLD(200) → длинная
    v, r = guard("D", "g", long_zh, "None")
    check("дегенеративный None -> отклонено", v is None and r and "дегенератив" in r, str(r))
    v, r = guard("D", "h", long_zh, "короткий перевод")
    check("слишком короткий для длинного -> отклонено",
          v is None and r and "короткий" in r, str(r))
    v, r = guard("D", "i", long_zh, "Перевод " + ("x" * 400))
    check("длинный корректный ру (len>=0.25*zh) -> принят", v is not None, str(r))
    v, r = guard("D", "j", "短文本", "None")
    check("None у короткой строки тоже отклонён", v is None and r and "дегенератив" in r, str(r))


# ---------------------------------------------------------------------------
# 3. wake-word regex
# ---------------------------------------------------------------------------
def test_wake() -> None:
    print("\n== wake-word regex ==")
    zh = '“开启后，可通过 “***你好” 在车外唤醒语⾳助⼿”'
    check("находит ***你好", WAKE_WORD_RE.findall(zh) == ["你好"],
          f"{WAKE_WORD_RE.findall(zh)}")
    check("обычный CJK без *** не ловит",
          WAKE_WORD_RE.findall("开启设置") == [])


# ---------------------------------------------------------------------------
# 4. aapt2 round-trip (fmt_string_xml как в пайплайне)
# ---------------------------------------------------------------------------
def test_aapt2_roundtrip() -> None:
    print("\n== aapt2 round-trip (JSON ru -> fmt_string_xml -> compile+link -> dump) ==")
    cases = {
        "tags_raw":   '<a href="https://y.qq.com/y/static/protocol/car_service.html">Соглашение</a> и <br/>далее',
        "tags_quote": '"Вам будет предложено <b>подтвердить</b> подключение"',
        "apostrophe": "оно’s здесь и 100% готово",
        "newlines":   "Строка1\\nСтрока2\\tвот табуляция",
        "fmt_s_d":    "Продолжить маршрут %s с, уровень %d%%",
        "fmt_numbered": "Уровень %1$d, путь %2$s",
        "wake":       'После включения сказав «***你好»»',
        "nbsp":       "继\xa0程 %s S",
        "amp":        "R&D &amp; Co, цена %s руб.",
    }
    tmp = Path(tempfile.mkdtemp(prefix="selftest_"))
    res_dir = tmp / "res" / "values-ru"
    res_dir.mkdir(parents=True)
    xml = ['<?xml version="1.0" encoding="utf-8"?>', '<resources>']
    for k, v in cases.items():
        xml.append("    " + fmt_string_xml(f"s_{k}", v))
    xml.append("</resources>")
    (res_dir / "strings.xml").write_text("\n".join(xml), encoding="utf-8")
    (tmp / "AndroidManifest.xml").write_text(
        '<?xml version="1.0" encoding="utf-8"?>\n'
        '<manifest xmlns:android="http://schemas.android.com/apk/res/android" '
        'package="com.test.selftest" android:versionCode="1">'
        '<application android:hasCode="false"/></manifest>\n', encoding="utf-8")

    out = tmp / "out"
    out.mkdir()
    r = subprocess.run(["aapt2", "compile", "-o", str(out), str(res_dir / "strings.xml")],
                       capture_output=True, text=True)
    check("compile", r.returncode == 0, r.stderr[:400])
    if r.returncode != 0:
        return
    flats = sorted(out.glob("*.flat"))
    apk = tmp / "t.apk"
    r2 = subprocess.run(["aapt2", "link", "-o", str(apk), "-I", ANDROID_JAR,
                         "--manifest", str(tmp / "AndroidManifest.xml"),
                         "--no-resource-removal"] + [str(f) for f in flats],
                        capture_output=True, text=True)
    check("link", r2.returncode == 0, r2.stderr[:300])
    if r2.returncode == 0:
        dump = subprocess.run(["aapt2", "dump", "resources", str(apk)],
                              capture_output=True, text=True).stdout
        for k, v in cases.items():
            i = dump.find("string/s_" + k + "\n")
            if i < 0:
                check(f"dump s_{k}", False, "ресурс не найден в dump")
                continue
            tail = dump[i:]
            m = re.search(r"\([^)]*\) \"", tail)
            if not m:
                check(f"dump s_{k}", False, "нет значения")
                continue
            rest = tail[m.end():]  # сразу после открывающей кавычки dump
            # читаем до строки, оканчивающейся на закрывающую кавычку
            buf = ""
            for _ in range(8):
                line, sep2, rest = rest.partition("\n")
                buf = buf + ("\n" if sep2 or buf else "") + line
                if buf.rstrip().endswith('"') or not sep2:
                    break
            got = buf
            if got.endswith('"'):
                got = got[:-1]
            got = re.sub(r"(?m)^\s+", "", got)  # отступ продолжения многострочного значения
            expect = v.replace("\\n", "\n").replace("\\t", "\t")
            # aapt2-dump рендер-нормализация (значение в APK байтовое, проверяем
            # две задокументированные особенности Вывода dump, не данных):
            #   (a) атрибуты styled-тегов печатаются без кавычек: href="url" -> href=url
            #   (b) значение, начинающееся/заканчивающееся литеральной ", сливается
            #       с биндной кавычкой dump и теряет внешнюю кавычку.
            got = re.sub(r'="([^"]*)"', r"=\1", got)
            expect = re.sub(r'="([^"]*)"', r"=\1", expect)
            if expect.startswith('"') and expect.endswith('"') and len(expect) >= 2:
                expect = expect[1:-1]
            check(f"roundtrip s_{k}", got == expect, f"want={expect!r}\n     got={got!r}")
    shutil.rmtree(tmp)


def test_parse_repair() -> None:
    print("\n== parse_repair (lenient) ==")
    from improve_translations import _parse_repair_response
    good = '[{"n":1,"window_ru":"окно один"},{"n":2,"window_ru":"окно два"}]'
    check("clean JSON", _parse_repair_response(good) ==
          [{"n": 1, "window_ru": "окно один"}, {"n": 2, "window_ru": "окно два"}])
    # модель вставляет живой \n в окно (битый escape для json.loads)
    raw_nl = '[{"n":1,"window_ru":"л1\\nл2"},{"n":2,"window_ru":"x"}]'
    check("бытовой \\n выживает", _parse_repair_response(raw_nl)[0]["window_ru"] == "л1\nл2")
    # невалидный escape \\q -> сам символ (не падает)
    bad = '[{"n":1,"window_ru":"текст \\q конец"}]'
    check("невалидный escape не падает",
          _parse_repair_response(bad)[0]["window_ru"] == "текст q конец")
    # пустой/мусор -> None (запустит полный повтор)
    check("мусор -> None", _parse_repair_response("не json") is None)
    check("пустой массив -> falsy", not _parse_repair_response("[]"))


# ---------------------------------------------------------------------------
# 8. styled-механика (tokenize / cjk_frags / verify / reassemble)
# ---------------------------------------------------------------------------
def test_styled() -> None:
    print("\n== styled-механика (--styled: tokenize/frags/verify/reassemble) ==")
    # tokenize: текст + реальные теги
    zh = '要为<b>所有</b>用户卸载此应用吗？'
    parts = _styled_tokenize(zh)
    check("tokenize mixed", [p for p in parts] ==
          [["T", "要为"], ["K", "<b>"], ["T", "所有"], ["K", "</b>"],
           ["T", "用户卸载此应用吗？"]], repr(parts))
    check("tokenize tags", _styled_toks(parts) == ["<b>", "</b>"])
    # tokenize: экранированные теги (по образцу из реальных 13 styled-строк).
    #   &lt;br/&gt;   -> '&lt;' + КРАТКИЙ текст 'br/' + '&gt;'  (из policy_*/clause_*)
    #   &lt;strong>  -> один токен;  &lt;/strong> -> один токен (из clause_title)
    #   &nbsp;       -> один токен-сущность
    check("tokenize &lt;br/&gt; (реальный WTN-паттерн)",
          _styled_toks(_styled_tokenize(' &lt;br/&gt; ')) == ["&lt;", "&gt;"],
          repr(_styled_tokenize(' &lt;br/&gt; ')))
    check("tokenize &lt;strong>..&lt;/strong> (clause_title)",
          _styled_toks(_styled_tokenize('<Data>&lt;strong>高德服务条款&lt;/strong>&lt;br/></Data>'))
          == ["<Data>", "&lt;strong>", "&lt;/strong>", "&lt;br/>", "</Data>"],
          repr(_styled_toks(_styled_tokenize(
              '<Data>&lt;strong>高德服务条款&lt;/strong>&lt;br/></Data>'))))
    # cjk_frags: голые тела сущностей pre/post — остаток double-encoded
    # &amp;emsp; (где &amp; — K-токен, а emsp; — голый текст-фрагмент).
    # Модель не видит pre/post (иначе добавит & → новая сущность → рассинхрон).
    # PRE: реальный паттерн из WT_FusionNavigation/policy_content*
    parts_p = _styled_tokenize('&amp;emsp;欢迎您使用我们的产品和服务！')
    fp = _styled_cjk_frags(parts_p)
    check("cjk_frags pre (реальный &amp;emsp;)", len(fp) == 1 and
          fp[0]["pre"] == "emsp;" and
          fp[0]["core"] == "欢迎您使用我们的产品和服务！" and fp[0]["post"] == "",
          repr(fp))
    # POST: CJK-текст дословно заканчивается голым 'nbsp;' (без '&' = текст)
    parts_q = _styled_tokenize('<b>你好nbsp;</b>')
    fq = _styled_cjk_frags(parts_q)
    check("cjk_frags post (голый nbsp; в конце)", len(fq) == 1 and
          fq[0]["post"] == "nbsp;" and fq[0]["core"] == "你好" and
          fq[0]["pre"] == "", repr(fq))
    # check (per-fragment verify): CJK-остаток, пустота, короткий
    check("verify cjk-left", _styled_check("你好世界", "Привет世界") is not None)
    check("verify empty", _styled_check("你好", "   ") is not None)
    check("verify none-ok", _styled_check("你好世界", "Привет, мир") is None)
    check("verify too-short", _styled_check("非常非常长的中文文本内容", "短") is not None)
    # reassemble: токены 1:1, перевод только текстовых фрагментов
    pr = [
        ["T", "要为"], ["K", "<b>"], ["T", "所有"], ["K", "</b>"],
        ["T", "用户卸载。"],
    ]
    fr = _styled_cjk_frags(pr)
    ru = _styled_reassemble(pr, {0: "для", 1: "всех", 2: "пользователей."}, fr)
    check("reassemble 1:1 toks", ru is not None, repr(ru))
    check("reassemble tokens kept", ru is not None and
          _styled_toks(_styled_tokenize(ru)) == ["<b>", "</b>"], repr(ru))
    # reassemble: модель ДВАЖДЫ вставила разметку (лишний токен в переводе)
    # -> каркас рассинхронизирован -> None
    ru_bad = _styled_reassemble(pr, {0: "<i>для</i>", 1: "всех", 2: "пользователей."}, fr)
    check("reassemble extra token in RU -> None", ru_bad is None)
    # РЕАЛЬНАЯ строка (WT_FusionNavigation/clause_title): идентичность перевода
    # (core->core) -> полный round-trip ровно к исходному zh + токены 1:1.
    real_zh = "<Data>&lt;strong>高德服务条款&lt;/strong>&lt;br/></Data>"
    rp = _styled_tokenize(real_zh)
    rf = _styled_cjk_frags(rp)
    rtrans = {f["tidx"]: (f["head"] + f["core"]) for f in rf}
    rru = _styled_reassemble(rp, rtrans, rf)
    check("real 13-string exact round-trip == zh", rru == real_zh, repr(rru))
    check("real 13-string tokens 1:1", rru == real_zh and
          _styled_toks(_styled_tokenize(rru)) == _styled_toks(rp))


# ---------------------------------------------------------------------------
# 9. aapt2-лимит (fmt_* и детект too-large в create_rro)
# ---------------------------------------------------------------------------
def test_aapt2_limit() -> None:
    print("\n== aapt2 32767-лимит (fmt_* -> None, DETECT) ==")
    # fmt_plain_checked: > AAPT2_MAX_BYTES -> None
    long_plain = "О" * (AAPT2_MAX_BYTES // 2 + 1)  # 1 байт -> 32701
    check("fmt_plain_checked >limit -> None",
          fmt_plain_checked("n", long_plain) is None)
    check("fmt_plain_checked ok -> str",
          isinstance(fmt_plain_checked("n", "коротко"), str))
    # fmt_styled_xml: > AAPT2_MAX_BYTES raw -> None (нет Data-фолбэка)
    long_styled = "Т" * (AAPT2_MAX_BYTES + 1)  # 2 байта/символ
    check("fmt_styled_xml >limit (без Data) -> None",
          fmt_styled_xml("n", long_styled) is None)
    # fmt_styled_xml: влезает -> вербати
    short_s = "<b>粗体</b> текст"
    v = fmt_styled_xml("n", short_s)
    check("fmt_styled_xml ok -> verbatim",
          v == f'    <string name="n" formatted="false">{short_s}</string>', repr(v))
    # fmt_styled_xml Data-фолбэк: ВЕСЬ ru = <Data>…</Data>, длина raw > лимит,
    # но inner (без '<') влезает -> храним inner как plain string.
    inner = "x" * (AAPT2_MAX_BYTES - 8)  # 32692 <= лимит, без '<'
    outer_big = "<Data>" + inner + "</Data>"  # 32692+13 = 32705 > 32700
    ds = fmt_styled_xml("n", outer_big)
    check("fmt_styled_xml Data-фолбэк -> inner",
          ds == f'    <string name="n" formatted="false">{inner}</string>',
          repr(ds)[:120])
    # а если inner тоже не влезает — None
    inner2 = "x" * (AAPT2_MAX_BYTES + 1)
    check("fmt_styled_xml Data-фолбэк не спасает -> None",
          fmt_styled_xml("n", "<Data>" + inner2 + "</Data>") is None)
    # эффективный размер: plain esc'ится, styled нет
    import validate as V
    rp = V.esc("a<>&'\"")
    check("validate.esc", rp == "a&lt;&gt;&amp;&apos;&quot;", repr(rp))
    b_plain = V.effective_value_bytes({"styled": False}, "x&y")
    b_styled = V.effective_value_bytes({"styled": True}, "x&y")
    check("bytes plain=esc(>raw), styled=raw",
          b_plain > b_styled, f"plain={b_plain} styled={b_styled}")

    # DETECT too-large по выводу aapt2 (rc=0, warning в stdout)
    import create_rro_min as CRM
    import create_rro_static as CRS
    import subprocess as sp

    class _R:
        def __init__(s, o, e, rc=0):
            s.stdout, s.stderr, s.returncode = o, e, rc
    warn = ("error: string too large to encode using UTF-8 "
            "written instead as 'STRING_TOO_LARGE'.\n")
    for mod in (CRM, CRS):
        check(f"{mod.__name__} detect warn in stdout",
              mod._aapt2_too_large(_R(warn, ""), "link", "A") is True)
        check(f"{mod.__name__} detect rc!=0 too",
              mod._aapt2_too_large(_R(warn, ""), "link", "A") is True)
        check(f"{mod.__name__} clean output -> False",
              mod._aapt2_too_large(_R("", "plain info"), "link", "A") is False)
    # (rc=0 обязателен для детекта: aapt2 не роняет процесс)
    # parse_aapt2_dump: STRING_TOO_LARGE как значение строки
    dump = ('package: com.android.vendor.translate.rro.x\n'
            'resource 0x7f010000 string/big\n      (ru) "STRING_TOO_LARGE"\n'
            'resource 0x7f010001 string/ok\n      (ru) "к"\n')
    parsed = V.parse_aapt2_dump(dump)
    check("parse STRING_TOO_LARGE value",
          V.STRING_TOO_LARGE in parsed.get("string/big", {}).get("values", []),
          repr(parsed))
    # check_apk: STRING_TOO_LARGE в values -> ошибка (rc=1)
    # Собираем реальный APK со строкой >32767 и прогоняем validate.check_apk
    tmp = Path(tempfile.mkdtemp(prefix="selftest_tolr_"))
    resd = tmp / "res" / "values-ru"
    resd.mkdir(parents=True)
    (resd / "strings.xml").write_text(
        '<?xml version="1.0" encoding="utf-8"?>\n<resources>\n'
        f'    <string name="big">{"Т"*20000}</string>\n'
        '    <string name="ok">к</string>\n</resources>\n', encoding="utf-8")
    (tmp / "AndroidManifest.xml").write_text(
        '<?xml version="1.0" encoding="utf-8"?>\n'
        '<manifest xmlns:android="http://schemas.android.com/apk/res/android" '
        'package="com.android.vendor.translate.rro.x" android:versionCode="1">'
        '<overlay android:targetPackage="com.android.vendor.translate.rro" '
        'android:isStatic="false"/>'
        '<application android:hasCode="false"/></manifest>\n', encoding="utf-8")
    out = tmp / "out"; out.mkdir()
    r = subprocess.run(["aapt2", "compile", "-o", str(out), str(resd / "strings.xml")],
                       capture_output=True, text=True)
    flats = sorted(out.glob("*.flat"))
    apk = tmp / "t.apk"
    subprocess.run(["aapt2", "link", "-o", str(apk), "-I", ANDROID_JAR,
                    "--manifest", str(tmp / "AndroidManifest.xml"),
                    "--auto-add-overlay", "--no-resource-removal"] + [str(f) for f in flats],
                   capture_output=True, text=True)
    if apk.exists():
        before = len(V.errors)
        V.check_apk(apk, "x", {}, "apks_rro_min", "com.android.vendor.translate.rro.")
        new_errs = V.errors[before:]
        check("check_apk STRING_TOO_LARGE -> ERROR",
              any("STRING_TOO_LARGE" in msg for _, msg in new_errs),
              repr(new_errs))
        # очистить для чистоты остальных тестов
        del V.errors[before:]
    else:
        check("check_apk STRING_TOO_LARGE -> ERROR", False, "аapt2 не собрал тестовый APK")
    shutil.rmtree(tmp)


def test_fit() -> None:
    print("\n== fit (укорочение перешироких RU) ==")
    # strwidth: ширина
    check("width cjk=2", display_width("景点") == 4)
    check("width ru=1", display_width("Дост") == 4)
    check("width mixed", display_width("CA 证书") == 7)   # C,A,' ' =3 + 证,书=4
    # hard_cap: min(FIT_HARD_MAX=28, 2.0*zh_w + 8)
    check("cap 用户协议(w8)", hard_cap("用户协议") == min(28, 24))  # 4 CJK = w8
    check("cap 电耗(w4)", hard_cap("电耗") == min(28, 16))          # 2 CJK = w4
    # is_ru_oversized: порог
    check("oversized yes", is_ru_oversized("用户协议", "Пользовательское соглашение"))
    check("oversized no (short ru)", not is_ru_oversized("用户协议", "Соглашение"))
    check("oversized no (всего 1)", not is_ru_oversized("景", "Достопримечательность"))
    # is_target fit: корректная переширокая строка — кандидат
    e = {"name": "n", "zh": "用户协议", "ru": "Пользовательское соглашение", "type": "string"}
    check("is_target fit", is_target(e, "fit", "A", "n"))
    check("is_target fit (короткий ru)", not is_target(
        {"name": "n", "zh": "用户协议", "ru": "Соглашение", "type": "string"}, "fit", "A", "n"))
    # guard fit: короче по ширине + в cap -> принят
    v, r = guard("A", "n", "用户协议", "Соглашение",
                 fit=True, ru_current="Пользовательское соглашение")
    check("fit: укорочен -> принят", v == "Соглашение", str(r))
    # guard fit: не короче текущего по ширине -> отклонён
    v, r = guard("A", "n", "用户协议",
                 "Пользовательское соглашение сервиса",
                 fit=True, ru_current="Пользовательское соглашение")
    check("fit: не короче -> отклонён", v is None and "не короче" in (r or ""), str(r))
    # guard fit: короче по ширине, но длиннее cap (в символах) -> отклонён
    v, r = guard("A", "n", "用户协议", "Пользовательское соглашение",
                 fit=True, ru_current="Пользовательское соглашение о правилах сервиса")
    check("fit: > cap -> отклонён", v is None and "cap" in (r or ""), str(r))
    # guard fit: плейсхолдеры по-прежнему обязательны
    v, r = guard("A", "p", "行程%s", "Дальше",
                 fit=True, ru_current="Маршрут %s (очень длинное текущее) значение")
    check("fit: потерян %s -> отклонён", v is None and "формatters" in (r or ""), str(r))
    # guard fit: сформматтером и короче -> принят
    v, r = guard("A", "q", "行程%s", "Дальше %s",
                 fit=True, ru_current="Маршрут %s (очень длинное текущее) значение")
    check("fit: %s сохранён, короче -> принят", v == "Дальше %s", str(r))
    # guard fit: CJK -> отклонён
    v, r = guard("A", "c", "用户协议", "Соглашение 用",
                 fit=True, ru_current="Пользовательское соглашение о сервисе")
    check("fit: CJK -> отклонён", v is None and "CJK" in (r or ""), str(r))
    # guard БЕЗ fit: длинный вариант принимается (фильтр — только для --fit)
    v, r = guard("A", "x", "用户协议", "Пользовательское соглашение о сервисе")
    check("non-fit: широкий принят", v is not None, str(r))


def main() -> int:
    test_specs()
    test_guard()
    test_fit()
    test_wake()
    test_parse_repair()
    test_styled()
    test_aapt2_limit()
    test_aapt2_roundtrip()
    print("\n" + "=" * 60)
    fails = [r for r in results if not r[1]]
    print(f"Итог: {len(results) - len(fails)}/{len(results)} OK")
    for name, _, info in fails:
        print(f"  FAIL: {name} — {info}")
    print("=" * 60)
    return 1 if fails else 0


if __name__ == "__main__":
    sys.exit(main())
