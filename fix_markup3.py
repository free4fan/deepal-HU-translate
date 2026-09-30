#!/usr/bin/env python3
"""Добирает те из 13 строк с разметкой, которые ещё NOT styled (fallback run).

Пер-фрагмент (без JSON-групп, которые модель «забывает» при одной битости),
каждый фрагмент — до MAX_RETRY plain-вызовов, пока не пройдёт verify.
Успешные фрагменты кэшируются в /tmp/opencode/frag_cache.json, повторный
запуск только недостающие. Разметка сохраняется побайтово (tokenize/reassemble).
"""
import json, os, re, time, urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parent
TRANS = ROOT / "translations"
DECOMPILED = ROOT / "decompiled"
CACHE = Path("/tmp/opencode/frag_cache.json")

API_URL = os.environ.get("API_URL", "http://10.0.0.128:11434/v1/chat/completions")
API_KEY = os.environ.get("API_KEY", "ollama")
API_MODEL = os.environ.get("API_MODEL", "qwen3.8:27b")
MAX_RETRY = 20
SLEEP = 1.0
# qwen3.8:27b — reasoning-модель: расходует много токенов на reasoning.
# Недостаточный max_tokens -> обрезанный/пустой content. Держим запас:
# reasoning ~1-2k + перевод (до ~400 симв CJK -> до ~700 рус).
def _max_tokens(core):
    return max(5000, 3000 + len(core) * 4)

CJK = re.compile(r"[\u4e00-\u9fff\u3400-\u4dbf\uf900-\ufaff]")
TOKEN = re.compile(
    r"<[^<>]*>"
    r"|&lt;/?[A-Za-z][^<>]*?>"
    r"|&[A-Za-z]+;|&#x?[0-9a-fA-F]+;")
DEGENERATE = {"", "none", "null", "n/a", "undefined", "перевод отсутствует", "нет", "—", "-", "."}

LOG = Path("/tmp/opencode/fix_markup3.log")


def log(*a):
    s = " ".join(str(x) for x in a)
    print(s, flush=True)
    with LOG.open("a", encoding="utf-8") as f:
        f.write(s + "\n")


def raw_inner(app, rel, name):
    txt = (DECOMPILED / app / "res" / rel).read_text(encoding="utf-8")
    m = re.search(rf'<string name="{re.escape(name)}"[^>]*>(.*?)</string>', txt, re.S)
    return m.group(1) if m else None


def tokenize(zh):
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
    """frag: tidx, head, pre, core, post, tail.

    pre/post = «голое тело сущности» вида emsp;/nbsp; (без &). В исходнике это
    остаток double-encoded &amp;emsp; где &amp; — токентокен, emsp; — текст.
    Модель не должна их видеть — иначе добавит & и создаст новый тег <entity>.
    Сохраняем pre/post побайтово вокруг перевода core."""
    frags, tidx = [], 0
    for kind, val in parts:
        if kind != "T":
            continue
        if CJK.search(val):
            m = re.match(r"^(\s*)(.*?)(\s*)$", val, re.S)
            head, body, tail = m.group(1), m.group(2), m.group(3)
            pm = re.match(r"^([a-zA-Z]+;)( ?)", body)
            pre = pm.group(1) if pm else ""
            body2 = body[len(pre):]
            pm2 = re.search(r"( ?)([a-zA-Z]+;)$", body2)
            post = pm2.group(2) if pm2 else ""
            core = body2[:len(body2) - len(post)]
            frags.append({"tidx": tidx, "head": head, "pre": pre,
                          "core": core, "post": post, "tail": tail,
                          "nl": "\n" in core, "body": body})
        tidx += 1
    return frags


def has_token(s):
    return bool(TOKEN.search(s or ""))


def check(zh, ru):
    ru = (ru or "").strip().strip('`').strip()
    if not ru or ru.lower() in DEGENERATE:
        return "empty"
    if CJK.search(ru):
        return "cjk"
    if "%" in ru and "%" not in zh:
        return "pct"
    if len(zh.strip()) >= 4 and len(ru.strip()) < 0.20 * len(zh.strip()):
        return "short"
    return None


SYS = ("Ты — переводчик zh→ru. Ответь ОДНИМ строковым значением — русский перевод "
       "введенного фрагмента. Без пояснений, без кавычек-обёрток, без JSON. "
       "Форматтеры %s/%d/%1$s не трогай. 高德/Amap = Amap, 深蓝汽车/Deepal = Deepal. "
       "Дата по-русски: 2022年06月10日 -> 10.06.2022. Адреса/имена транслитерируй. "
       "Используй запятую, точку, тире как в исходнике; не сокращай смысл.")


def call(core):
    payload = {"model": API_MODEL, "stream": False, "temperature": 0.4,
               "max_tokens": _max_tokens(core),
               "messages": [{"role": "system", "content": SYS},
                            {"role": "user", "content": core}]}
    req = urllib.request.Request(
        API_URL, data=json.dumps(payload).encode(),
        headers={"Content-Type": "application/json",
                 "Authorization": f"Bearer {API_KEY}"})
    with urllib.request.urlopen(req, timeout=400) as r:
        c = json.loads(r.read())["choices"][0]["message"].get("content") or ""
    c = c.strip()
    c = re.sub(r"^```[a-zA-Z]*\s*", "", c)
    c = re.sub(r"\s*```$", "", c)
    return c.strip().strip('"').strip("'").strip()


def translate_one(name, core):
    for i in range(MAX_RETRY):
        try:
            v = call(core)
        except Exception as e:
            time.sleep(SLEEP + i); continue
        err = check(core, v)
        if err is None:
            return v
        if i % 5 == 4:
            log(f"      {name} try{i+1} err={err} tail={v[:50]!r}")
        time.sleep(SLEEP + 0.3 * i)
    return None


def main():
    if LOG.exists():
        LOG.unlink()
    log(f"=== fix_markup3 model={API_MODEL} ===")
    cache = {}
    if CACHE.exists():
        cache = json.loads(CACHE.read_text(encoding="utf-8"))
    log(f"cache loaded: {len(cache)} fragments")

    pending = []  # (app, rel, name)
    for app, rel, name in [
        ("WT_FusionNavigation", "values/strings.xml", "clause_content1"),
        ("WT_FusionNavigation", "values/strings.xml", "policy_content1"),
    ]:
        fp = TRANS / f"{app}.json"
        data = json.loads(fp.read_text(encoding="utf-8"))
        e = next((x for x in data if x.get("name") == name), None)
        if e and e.get("styled") and (e.get("ru") or "").strip():
            log(f"-- {app}/{name}: already styled, skip")
            continue
        pending.append((app, rel, name, fp))
    if not pending:
        log("no pending"); return

    # gather frags for all pending, translate missing
    for app, rel, name, fp in pending:
        zh = raw_inner(app, rel, name)
        parts = tokenize(zh)
        frags = cjk_frags(parts)
        log(f"== {app}/{name} frags={len(frags)}")
        trans = {}
        failed_tidx = set()
        for f in frags:
            key = f"{app}/{name}#{f['tidx']}"
            v = cache.get(key)
            # cache может содержать устаревшие значения (добавляют &emsp; или
            # переводили по «body», а не по «core»). Проверяем по core.
            if v is not None:
                if has_token(v) or check(f["core"], v) is not None:
                    log(f"     {name}#{f['tidx']}: cached value bad (token/cjk/len), re-fetch")
                    v = None
            if v is None:
                v = translate_one(key, f["core"])
                if v is None:
                    log(f"   !!! {name}: tidx={f['tidx']} FAILED after {MAX_RETRY}: core={f['core'][:60]!r}")
                    failed_tidx.add(f["tidx"])
                    break
                cache[key] = v
            trans[f["tidx"]] = v
            if len([f for f in frags if f['tidx'] in trans]) % 25 == 0:
                CACHE.write_text(json.dumps(cache, ensure_ascii=False, indent=1), encoding="utf-8")
                log(f"     {name}: {len(trans)}/{len(frags)} done (failed={failed_tidx})")
        if failed_tidx:
            continue
        # all done — reassemble. Каждый CJK-фрагмент: head+pre+translation+post+tail
        # (всё сохранено побайтово, кроме core -> перевод; core может быть multiline ->
        # перевод одной строкой, пустых строк не добавляем).
        frag_at = {f["tidx"]: f for f in frags}
        rebuilt = []
        tidx = 0
        for kind, val in parts:
            if kind == "K":
                rebuilt.append(val)
                continue
            fi = frag_at.get(tidx)
            if fi is None:
                rebuilt.append(val)  # пустой/не-CJK текст
            else:
                lines = [l for l in trans[tidx].split("\n") if l.strip()]
                core_ru = "\n".join(lines)
                rebuilt.append(f'{fi["head"]}{fi["pre"]}{core_ru}{fi["post"]}{fi["tail"]}')
            tidx += 1
        ru = "".join(rebuilt)
        p2 = tokenize(ru)
        if toks_of(p2) != toks_of(parts):
            log(f"   !!! {name}: SKELETON MISMATCH ({len(toks_of(p2))} vs {len(toks_of(parts))})")
            continue
        if CJK.search(ru):
            i = CJK.search(ru).start()
            log(f"   !!! {name}: CJK LEFT at {i}: ...{ru[max(0, i-30):i+30]!r}...")
            continue
        data = json.loads(fp.read_text(encoding="utf-8"))
        # Обновить, если запись уже есть, ИНАЧЕ дописать (те из 13 строк, что
        # не попали в JSON из-за бага extract_cjk.py — их записи нет).
        found = False
        for e in data:
            if e.get("name") == name:
                e.update({"zh": zh, "ru": ru, "type": "string", "styled": True})
                found = True
                break
        if not found:
            data.append({"name": name, "zh": zh, "ru": ru,
                         "type": "string", "styled": True})
        tmp = fp.with_suffix(".json.tmp")
        with open(tmp, "w", encoding="utf-8") as f2:
            json.dump(data, f2, ensure_ascii=False, indent=2); f2.write("\n")
        os.replace(tmp, fp)
        log(f"   OK {name} ru={len(ru)} -> {fp.name}")
    CACHE.write_text(json.dumps(cache, ensure_ascii=False, indent=1), encoding="utf-8")
    log("=== done ===")


if __name__ == "__main__":
    main()
