#!/usr/bin/env python3
"""13 строк с вложенной разметкой — перевод ПО ФРАГМЕНТАМ (побайтовая разметка).

extract_cjk.py regex ([^<]*) рвал на первом '<' -> строки с <b>/<a>/<annotation>
и <Data>...&lt;br/&gt;...</Data> не попадали в JSON -> китайский на ГУ.

Метод: строку режем на разметочные токены (теги / экранированные теги / сущности)
и текстовые фрагменты. Разметку сохраняем побайтово; переводим ТОЛЬКО текстовые
фрагменты, содержащие CJK, партиями (numbered JSON). Пересобираем исходную
строку из переводов + исходных токенов -> 1:1 разметка гарантированно.

Запись в translations/<app>.json: {name, zh, ru, type:"string", styled:true}.
styled:true -> generate_overlays*.py пишут ru в values-ru БЕЗ _esc().
"""
import json, os, re, sys, time, urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parent
TRANS = ROOT / "translations"
DECOMPILED = ROOT / "decompiled"

API_URL = os.environ.get("API_URL", "http://10.0.0.128:11434/v1/chat/completions")
API_KEY = os.environ.get("API_KEY", "ollama")
API_MODEL = os.environ.get("API_MODEL", "qwen3.8:27b")
API_MAX_TOKENS = int(os.environ.get("API_MAX_TOKENS", "10000"))
API_TIMEOUT = int(os.environ.get("API_TIMEOUT", "300"))

CHAR_BUDGET = 1500     # CJK-символов суммарно в партии
MAX_FRAGS = 30

TARGETS = [
    ("CarService", "values-zh-rCN/strings.xml", "imsi_protection_warning"),
    ("Fota", "values/strings.xml", "upgrade_service_agreement_content"),
    ("ManagedProvisioning", "values-zh-rCN/strings.xml", "read_more_delete_profile"),
    ("PackageInstaller", "values-zh-rCN/strings.xml", "uninstall_application_text_all_users"),
    ("WT_FusionNavigation", "values/strings.xml", "clause_content"),
    ("WT_FusionNavigation", "values/strings.xml", "clause_content1"),
    ("WT_FusionNavigation", "values/strings.xml", "clause_time"),
    ("WT_FusionNavigation", "values/strings.xml", "clause_title"),
    ("WT_FusionNavigation", "values/strings.xml", "policy_content"),
    ("WT_FusionNavigation", "values/strings.xml", "policy_content1"),
    ("WT_FusionNavigation", "values/strings.xml", "policy_content2"),
    ("WT_FusionNavigation", "values/strings.xml", "policy_time"),
    ("WT_FusionNavigation", "values/strings.xml", "policy_title"),
]

CJK = re.compile(r"[\u4e00-\u9fff\u3400-\u4dbf\uf900-\ufaff]")
TOKEN = re.compile(
    r"<[^<>]*>"
    r"|&lt;/?[A-Za-z][^<>]*?>"
    r"|&[A-Za-z]+;|&#x?[0-9a-fA-F]+;")
DEGENERATE = {"", "none", "null", "n/a", "undefined", "перевод отсутствует",
              "нет", "—", "-", "."}

LOG = Path("/tmp/opencode/fix_markup2.log")


def log(*a):
    s = " ".join(str(x) for x in a)
    print(s, flush=True)
    with LOG.open("a", encoding="utf-8") as f:
        f.write(s + "\n")


def raw_inner(app, rel, name):
    txt = (DECOMPILED / app / "res" / rel).read_text(encoding="utf-8")
    m = re.search(rf'<string name="{re.escape(name)}"[^>]*>(.*?)</string>', txt, re.S)
    if not m:
        raise SystemExit(f"не найден raw: {app}/{rel} #{name}")
    return m.group(1)


def tokenize(zh):
    """[(kind, value)] kind T=text / K=markup token."""
    parts, pos = [], 0
    for m in TOKEN.finditer(zh):
        if m.start() > pos:
            parts.append(["T", zh[pos:m.start()]])
        parts.append(["K", m.group(0)])
        pos = m.end()
    if pos < len(zh):
        parts.append(["T", zh[pos:]])
    return parts


def toks_of(parts):
    return [v for k, v in parts if k == "K"]


def cjk_frags(parts):
    """list of dict: tidx (position among text parts), core, ws, frag."""
    frags = []
    tidx = 0
    for kind, val in parts:
        if kind == "K":
            continue
        if CJK.search(val):
            m = re.match(r"^(\s*)(.*?)(\s*)$", val, re.S)
            fragments = [x for x in m.group(2).split("\n") if x.strip()]
            head, tail = m.group(1), m.group(3)
            frags.append({"tidx": tidx, "raw": val, "head": head, "tail": tail,
                          "core": m.group(2), "n": len(fragments)})
        tidx += 1
    return frags


def check_frag(zh_core, ru):
    ru = (ru or "").strip("\u2028").strip()
    if not ru or ru.lower() in DEGENERATE:
        return "пусто"
    if CJK.search(ru):
        return f"CJK осталось: {ru[:50]!r}"
    if "%" in ru and "%" not in zh_core:
        return "появился %"
    # ratio: ru может быть длиннее (русский длиннее). нижний порог 0.25
    zl = len(zh_core.strip())
    if zl >= 4 and len(ru.strip()) < 0.25 * zl:
        return f"слишком коротко: {len(ru)}<{0.25*zl:.0f} zh={zl}"
    return None


def call_llm(system, user, max_tokens=API_MAX_TOKENS):
    payload = {"model": API_MODEL, "stream": False, "temperature": 0.2,
               "max_tokens": max_tokens,
               "messages": [{"role": "system", "content": system},
                            {"role": "user", "content": user}]}
    req = urllib.request.Request(
        API_URL, data=json.dumps(payload).encode(),
        headers={"Content-Type": "application/json",
                 "Authorization": f"Bearer {API_KEY}"})
    with urllib.request.urlopen(req, timeout=API_TIMEOUT) as r:
        return json.loads(r.read())["choices"][0]["message"].get("content") or ""


def parse_json_lenient(content):
    c = (content or "").strip()
    c = re.sub(r"^```[a-zA-Z]*\s*", "", c)
    c = re.sub(r"\s*```$", "", c)
    for cand in (c,):
        try:
            return json.loads(cand)
        except Exception:
            pass
    m = re.search(r"\{.*\}", c, re.S)
    if m:
        try:
            return json.loads(m.group(0))
        except Exception:
            pass
    return None


FRAG_SYSTEM = (
    "Ты — переводчик zh→ru (Android/автосистема). Ответ — ТОЛЬКО валидный JSON-объект: "
    "ключ = индекс фрагмента (входной объект), значение = его русский перевод. "
    "Сохраняй ВСЕ ключи 1:1, без пояснений, без markdown. "
    "Форматтеры %s/%d/%1$s не трогай. Дата 2022年06月10日 -> 10.06.2022. "
    "Термины: OTA, IMEI, MAC, OBD, SIM, 高德=Amap (сохрани название Amap), Deepal/深蓝汽车=Deepal. "
    "Кавычки кит."
)


def translate_group(group):
    """group: list of frag dicts. Returns dict tidx->ru. None on fail."""
    payload = {str(f["tidx"]): f["core"] for f in group}
    for attempt in range(3):
        try:
            content = call_llm(FRAG_SYSTEM,
                               "Переведи каждый фрагмент, сохранив индексы:\n"
                               + json.dumps(payload, ensure_ascii=False))
        except Exception as e:
            log(f"        попытка {attempt+1}: API {type(e).__name__} {e}")
            time.sleep(2); continue
        data = parse_json_lenient(content)
        if not isinstance(data, dict):
            log(f"        попытка {attempt+1}: не-JSON head={content[:80]!r}")
            time.sleep(1); continue
        missing = [str(f["tidx"]) for f in group if str(f["tidx"]) not in data]
        if missing:
            log(f"        попытка {attempt+1}: нет ключей {missing}")
            time.sleep(1); continue
        bad = []
        for f in group:
            err = check_frag(f["core"], data[str(f["tidx"])])
            if err:
                bad.append((f["tidx"], err))
        if not bad:
            return {str(f["tidx"]): data[str(f["tidx"])].strip("\u2028").strip()
                    for f in group}
        log(f"        попытка {attempt+1}: битых {len(bad)}: {bad[:2]}")
        time.sleep(1)
    # fallback: per-fragment
    log("        ПОФРАГМЕНТНО")
    out = {}
    for f in group:
        got = None
        for a in range(3):
            try:
                v = call_llm(
                    FRAG_SYSTEM + "\nОдин фрагмент — ответь ОДНИМ значением (без JSON/ключей).",
                    f["core"], max_tokens=max(400, len(f["core"]) + 500))
            except Exception:
                time.sleep(1); continue
            v = v.strip().strip('`').strip('"').strip()
            if check_frag(f["core"], v) is None:
                got = v; break
        if got is None:
            log(f"          tidx={f['tidx']} FAIL: {f['core'][:60]!r}")
            return None
        out[str(f["tidx"])] = got
    return out


def translate_string(zh):
    parts = tokenize(zh)
    frags = cjk_frags(parts)
    trans = {}
    # group by budget (count core CJK chars, split newlines into separate)
    groups, cur, cur_len = [], [], 0
    for f in frags:
        clen = sum(len(x) for x in f["core"].split("\n") if x.strip())
        if cur and (cur_len + clen > CHAR_BUDGET or len(cur) >= MAX_FRAGS):
            groups.append(cur); cur, cur_len = [], 0
        cur.append(f); cur_len += clen
    if cur:
        groups.append(cur)
    for g in groups:
        res = translate_group(g)
        if res is None:
            return None
        trans.update(res)
    # reassemble each text part tidx
    rebuilt = []
    tidx = 0
    for kind, val in parts:
        if kind == "K":
            rebuilt.append(val)
        else:
            rawru = trans.get(str(tidx))
            if rawru is None:
                rebuilt.append(val)
            else:
                m = re.match(r"^(\s*)\n?(.*?)(\s*)$", val, re.S)
                head = m.group(1) if m else ""
                tail = m.group(3) if m else ""
                # если исходник один текст (нет \n) -> просто подставляем
                if "\n" not in val:
                    rebuilt.append(head + rawru + tail)
                else:
                    # множественные строки: head + перевод первой строки,
                    # остальные строки сохраняем из исходника (разметка та же,
                    # переводы приходят как единый текст с \n)
                    lines = rawru.split("\n")
                    lines = [l for l in lines if l.strip()]  # убираем пустые
                    # сохраняем исходные \n
                    rebuilt.append(head + "\n".join(lines) + tail)
            tidx += 1
    ru = "".join(rebuilt)
    # integrity: markup tokens 1:1 and CJK-free
    p2 = tokenize(ru)
    if toks_of(p2) != toks_of(parts):
        raise RuntimeError("markup skeleton mismatch")
    return ru


def main():
    if LOG.exists():
        LOG.unlink()
    log(f"=== fix_markup2  model={API_MODEL} url={API_URL} ===")
    dry = "--dry-run" in sys.argv
    results = {}
    for app, rel, name in TARGETS:
        fp = TRANS / f"{app}.json"
        data = json.loads(fp.read_text(encoding="utf-8"))
        have = next((e for e in data if e.get("name") == name), None)
        if have and have.get("styled") and (have.get("ru") or "").strip():
            log(f"-- {app}/{name}: уже styled, skip")
            results[name] = ("skip", 0)
            continue
        zh = raw_inner(app, rel, name)
        log(f"== {app}/{name}  zh={len(zh)} tokens={len(toks_of(tokenize(zh)))}")
        try:
            ru = translate_string(zh)
        except Exception as e:
            log(f"   !!! {app}/{name}: EXCEPTION {type(e).__name__} {e}")
            results[name] = ("fail", 0); continue
        if ru is None:
            log(f"   !!! {app}/{name}: FAILED")
            results[name] = ("fail", 0); continue
        if CJK.search(ru):
            i = CJK.search(ru).start()
            log(f"   !!! {app}/{name}: CJK left at {i}: ...{ru[max(0,i-30):i+30]!r}...")
            results[name] = ("fail", 0); continue
        if dry:
            log(f"   [dry-run] ru={len(ru)}")
            results[name] = ("dry", len(ru)); continue
        if have is None:
            data.append({"name": name, "zh": zh, "ru": ru,
                         "type": "string", "styled": True})
        else:
            for e in data:
                if e.get("name") == name:
                    e.update({"zh": zh, "ru": ru, "type": "string", "styled": True})
                    break
        tmp = fp.with_suffix(".json.tmp")
        with open(tmp, "w", encoding="utf-8") as f:
            json.dump(data, f, ensure_ascii=False, indent=2); f.write("\n")
        os.replace(tmp, fp)
        log(f"   OK ru={len(ru)} -> {fp.name}")
        results[name] = ("ok", len(ru))
    ok = sum(1 for v in results.values() if v[0] in ("ok", "skip"))
    log(f"\n=== done: {ok}/{len(TARGETS)} ===")
    for n, (st, ln) in results.items():
        log(f"   {st:4} ({ln:>6})  {n}")
    if ok < len(TARGETS) and not dry:
        log("!!! не все переведены — проверить ручным re-run")


if __name__ == "__main__":
    main()
