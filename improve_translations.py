#!/usr/bin/env python3
"""improve_translations.py — улучшение/перевод строк в translations/*.json.

Копия логики api_translate_ambiguity.py с изменённой обработкой входа/выхода:

  ВХОД  — строки из translations/<app>.json (не ambiguity_db.json);
  ВЫХОД — значение пишется в поле `ru` ТОЙ ЖЕ ЗАПИСИ (нет new_ru, нет
          отдельной базы); файл сохраняется после каждого батча;
          прогресс — translations/improve_progress.json (resume/fresh).

Системный промпт — promt.md (жёсткие правила по HTML/экранированию/плейсхолдерам).

Валидация ответа до записи (guard; при отклонении `ru` НЕ затирается):
  - пустой ответ отклоняется;
  - формatters zh != формatters ru (количество/номера) → отклоняется;
  - wake-word ***...*** из zh обязан остаться в ru дословно;
  - CJK в ru отклоняется, кроме CJK внутри ***...*** и whitelist-записей.

Нормализация перед записью (наследование конвенции _sanitize_new_ru):
  - двойное HTML-экранирование сводится и расшифровывается до ОДИНОЧНОГО
    уровня: &amp; -> &, &quot; -> ", &gt; -> >, &lt; -> <, &apos; -> '
    (JSON хранит «сырой» текст; generate_overlays._esc эксит его ровно
    один раз перед aapt2 — иначе получится &amp;lt; и аapt2 FAIL);
  - ' / &apos; / &#x27; -> ‘ (U+2019): aapt2 отклоняет одиночный '
    (unescaped apostrophe) даже в форме &apos; — проверено;
  - живые \n / \r / \t -> литеральные \\n / \\t (конвенция ресурсов).

Аргументы:
  --all        (по умолчанию) все CJK-строки — улучшить всё
  --defective  только проблемные: пустой ru / CJK в ru / битые плейсхолдеры
  --empty      только пустые ru
  --fit        только «раздутые коротки»: короткие UI-строки (см. strwidth),
               у которых RU заметно шире ZH (soft-порог 2.5x/24w). Задача —
               укоротить RU без потери смысла/плейсхолдеров. ДВА лимита
               последовательно: (1) мягкий — принимаем ответ только если он
               КОРОЧЕ текущего по display_width (и в пределах hard_cap);
               (2) жёсткий — повтор с явным cap'ом в символов
               (strwidth.hard_cap: min(28, 2.0·zh_w+8)), принимаем только
               <= cap. Не укоротилось — ru НЕ меняется, строка в
               logs/fit_stuck.txt. Прогресс отдельный:
               improve_fit_progress.json (смешиваться с improve_progress
               НЕ должен).
  --long       только длинные строки (len(zh) >= API_LONG_THRESHOLD); НЕ
               фильтруются progress'ом — повторный прогон именно тех, что
               падали в батче (короткие batch-mates не затронуты повтором)
  --app NAME   одно приложение
  --limit N    максимум строк на запуск (smoke-тест)
  --resume     продолжить с improve_progress.json
  --fresh      начать прогресс заново
  --dry-run    показать, что бы отправилось, не отправлять

Длинные строки в main-цикле всегда отправляются ПО ОДНОЙ (batch size = 1):
один очень длинный zh в пакете из 8 переполняет/обрезает ответ модели и
роняет весь батч. Порог — API_LONG_THRESHOLD (по умолчанию 200).

Переменные окружения (как в run_pipeline.sh):
  API_URL, API_KEY, API_MODEL, API_BATCH_SIZE, API_MAX_TOKENS, API_TIMEOUT,
  API_LONG_THRESHOLD (длина zh, с которой строка отправляется по одной),
  API_DEBUG (1 — лог в logs/improve_translations.log), API_DRY_RUN (1)

Примеры:
  API_URL=http://10.0.0.128:11434/v1/chat/completions API_KEY=ollama \
  API_MODEL=qwen3.8:27b python3 improve_translations.py --app AdayoAlarm --limit 24
  ... python3 improve_translations.py --defective        # точечно чинить дефекты
  ... API_DEBUG=1 nohup python3 improve_translations.py --all --resume &   # фоновый
"""
from __future__ import annotations

import argparse
import json
import logging
import math
import os
import re
import sys
import time
import traceback
import urllib.request
from pathlib import Path
from typing import Any

from strwidth import display_width, hard_cap, is_ru_oversized

BASE_DIR = Path(__file__).resolve().parent
TRANS_DIR = BASE_DIR / "translations"
PROMPT_MD = BASE_DIR / "promt.md"
PROGRESS_PATH = TRANS_DIR / "improve_progress.json"
FIT_PROGRESS_PATH = TRANS_DIR / "improve_fit_progress.json"
FIT_STUCK_PATH = BASE_DIR / "logs" / "fit_stuck.txt"
LOG_PATH = BASE_DIR / "logs" / "improve_translations.log"

API_URL = os.environ.get("API_URL", "http://10.0.0.128:11434/v1/chat/completions")
API_KEY = os.environ.get("API_KEY", "ollama")
API_MODEL = os.environ.get("API_MODEL", "qwen3.8:27b")
API_MAX_TOKENS = int(os.environ.get("API_MAX_TOKENS", "60000"))
API_TIMEOUT = float(os.environ.get("API_TIMEOUT", "600"))
BATCH_SIZE = int(os.environ.get("API_BATCH_SIZE", "8"))

# --- режим --styled (rich-text с разметкой: <b>/<a>/<annotation>/<Data>…) ---
# Перевод ТОЛЬКО текстовых фрагментов; разметочные токены сохраняются
# побайтово (verify-сверка). Эталон механики — fix_markup3.py (21.09).
STYLED_CACHE_PATH = TRANS_DIR / "improve_styled_cache.json"
STYLED_STUCK_PATH = BASE_DIR / "logs" / "styled_stuck.txt"
STYLED_MAX_RETRY = int(os.environ.get("STYLED_MAX_RETRY", "20"))
# reasoning-модель (qwen3.8:27b) сжигает лимит на reasoning; малый
# max_tokens -> пустой content. Держим щедрую оценку.
def _styled_max_tokens(core: str) -> int:
    return max(5000, 3000 + len(core) * 4)
STYLED_TOKEN = re.compile(
    r"<[^<>]*>"                       # реальные теги: <b>, </b>, <a ..>, <Data>, …
    r"|&lt;/?[A-Za-z][^<>]*?>"        # экранированные теги: &lt;br/>, &lt;/strong>
    r"|&[A-Za-z]+;|&#x?[0-9a-fA-F]+;")  # сущности: &emsp; &nbsp; &amp;
STYLED_BARE_ENTITY = re.compile(r"[A-Za-z]+;")

# Порог длины zh: строки длиннее отправляются ПО ОДНОЙ (batch size = 1).
# Одна очень длинная строка в пакете из 8 переполняет/обрезает ответ модели
# (объём выхода) и роняет весь батч — падают и соседние короткие.
LONG_THRESHOLD = int(os.environ.get("API_LONG_THRESHOLD", "200"))
DEBUG_ONLY = os.environ.get("API_DEBUG", "").lower() in ("1", "yes", "true")
DRY_RUN_ONLY = os.environ.get("API_DRY_RUN", "").lower() in ("1", "yes", "true")

# Дегенеративные «ответы»-плейсхолдеры (англ./программные), которые модель
# возвращает, когда сдалась (guard обязан их отклонить, иначе "None" уйдёт в
# ru как есть). ВАЖНО: сюда НЕ входят "нет"/"неизвестно" — это ЛЕГИТИМНЫЕ
# русские переводы коротких CJK-слов 无/没有/未知 (не должны отклоняться).
# Для длинных строк «слишком короткий ответ» ловится отдельной проверкой
# LONG_RATIO_MIN ниже (ru="Нет" на 9000-символьном документе поймат там).
DEGENERATE_ANSWERS = {"none", "null", "undefined", "n/a", "nil"}
# Для длинных строк guard требует: len(ru) >= LONG_RATIO_MIN * len(zh).
# Длинный документ не может уложиться в крошечный перевод; легитимный ru
# почти всегда >= 50% длины zh (ru не короче CJK для прозы), 0.25 — запас.
LONG_RATIO_MIN = 0.25

# Записи, где CJK в ru допустим (voice wake-words):
CJK_WHITELIST: set[tuple[str, str]] = {
    ("WT_VehicleCenter", "Exterior_voice_interaction_title"),
}

# Голосовой wake-word в данных: «***你好» (открытые звёзды, без закрывающих) —
# цепляем CJK-последовательность после 2+ звёзд. Закрытая форма ***...*** тоже
# ловится: lookahead \*{1,4} останавливает группировку перед закрытием.
WAKE_WORD_RE = re.compile(
    r"\*{2,3}([\u4e00-\u9fffA-Za-z0-9]{1,24}?)\s*(?=\*{1,4}|[^*\u4e00-\u9fffA-Za-z0-9]|$)")
# формatters: %s, %d, %.1f, %1$s, %2$d, %1$2d, %% — ВСЕ конвенции Android
FORMATTER_RE = re.compile(r"%(\d+\$)?(\.?\d*)([dfs])|%%")


# ---------------------------------------------------------------------------
# утилиты
# ---------------------------------------------------------------------------

def _has_cjk(text: Any) -> bool:
    if not isinstance(text, str):
        return False
    return any(0x4E00 <= ord(c) <= 0x9FFF for c in text)


def specs(s: str) -> list[str]:
    """Список Android-форматтеров строки ПО ЗАДАННЫМ ПОЛОЖЕНИЯМ (упорядоченно).

    Сравнение zh и ru — точным по порядку: модель не должна менять число/тип
    форматтера И не должна менять их относительный порядок. (Сортировка здесь
    не годится — она гасит смену %1$s <-> %2$s.)"""
    return [m.group(0) for m in FORMATTER_RE.finditer(s or "")]


def _logger() -> logging.Logger:
    logger = logging.getLogger("improve")
    logger.setLevel(logging.DEBUG)
    logger.handlers.clear()
    fmt = logging.Formatter("%(asctime)s [%(levelname)s] %(message)s",
                            datefmt="%Y-%m-%d %H:%M:%S")
    if DEBUG_ONLY:
        LOG_PATH.parent.mkdir(parents=True, exist_ok=True)
        fh = logging.FileHandler(LOG_PATH, encoding="utf-8")
        fh.setFormatter(fmt)
        logger.addHandler(fh)
    sh = logging.StreamHandler(sys.stderr)
    sh.setLevel(logging.INFO if not DEBUG_ONLY else logging.WARNING)
    sh.setFormatter(fmt)
    logger.addHandler(sh)
    return logger


LOGGER = _logger()


# ---------------------------------------------------------------------------
# данные
# ---------------------------------------------------------------------------

def list_apps() -> list[str]:
    apps = []
    for fp in sorted(TRANS_DIR.glob("*.json")):
        if fp.name.endswith("_progress.json"):
            continue
        if fp.name in ("index.json", "extraction.json", "ambiguity_db.json", "exclude.json"):
            continue
        apps.append(fp.stem)
    return apps


def load_app(app: str) -> list[dict]:
    data = json.loads((TRANS_DIR / f"{app}.json").read_text(encoding="utf-8"))
    if not isinstance(data, list):
        raise ValueError(f"{app}.json: не список")
    return data


def save_app(app: str, data: list[dict]) -> None:
    fp = TRANS_DIR / f"{app}.json"
    tmp = fp.with_suffix(".json.tmp")
    with open(tmp, "w", encoding="utf-8") as f:
        json.dump(data, f, ensure_ascii=False, indent=2)
        f.write("\n")
    os.replace(tmp, fp)  # атомарно: не гоним файл на середине записи


def _ru_defective(app: str, name: str, zh: str, ru: str) -> bool:
    """Текущее значение ru считается дефектным (нужен повторный прогон)."""
    if not ru:
        return True
    if _has_cjk(ru) and (app, name) not in CJK_WHITELIST:
        return True
    if specs(zh) != specs(ru):
        return True
    if len(zh) >= LONG_THRESHOLD:
        if ru.strip().lower() in DEGENERATE_ANSWERS:
            return True
        if len(ru) < LONG_RATIO_MIN * len(zh):
            return True
    return False


def is_target(entry: dict, mode: str, app: str, name: str) -> bool:
    if entry.get("styled"):
        return False  # rich-text с разметкой: переводится вручную, модель её сломает
    zh = (entry.get("zh") or "").strip()
    ru = (entry.get("ru") or "").strip()
    if not zh or not _has_cjk(zh):
        return False  # пустые array-элементы и латиница — не модель
    if mode == "empty":
        return not ru
    if mode == "long":
        return len(zh) >= LONG_THRESHOLD and _ru_defective(app, name, zh, ru)
    if mode == "defective":
        return _ru_defective(app, name, zh, ru)
    if mode == "fit":
        # короткая UI-строка, у которой RU заметно шире ZH (soft-порог).
        # Корректные (не-дефектные) переводы тоже кандидаты: ширина —
        # отдельный дефект, не связанный с плейсхолдерами/CJK.
        return is_ru_oversized(zh, ru)
    # "all": всё, где CJK в zh (и ru не равен zh — тогда ничего не изменится)
    return ru != zh


# ---------------------------------------------------------------------------
# нормализация + guard
# ---------------------------------------------------------------------------

def sanitize_ru(text: str) -> str:
    """Нормализовать ответ модели до записи в `ru` (см. docstring модуля)."""
    st = text.replace("&amp;", "&")
    st = st.replace("&quot;", '"').replace("&gt;", ">").replace("&lt;", "<")
    st = st.replace("&apos;", "'")
    st = st.replace("'", "\u2019").replace("&#x27;", "\u2019").replace("&apos;", "\u2019")
    st = st.replace("\r\n", "\\n").replace("\n", "\\n").replace("\r", "\\n")
    st = st.replace("\t", "\\t")
    return st


def guard(app: str, name: str, zh: str, new: str,
          fit: bool = False, ru_current: str = "") -> tuple[str | None, str | None]:
    """(принятое_значение, причина_отклонения).

    fit=True (режим --fit): дополнительно принимает ответ только если он
    короче текущего ru по display_width И len <= hard_cap(zh). ru_current —
    текущее значение, с которым идёт сравнение короче/не короче."""
    new = (new or "").strip()
    if not new:
        return None, "empty"
    # дегенеративные «ответы» модели (None/null/…) — не перевод
    if new.lower() in DEGENERATE_ANSWERS:
        return None, f"дегенеративный ответ {new!r}"
    new = sanitize_ru(new)
    if specs(zh) != specs(new):
        return None, f"формatters zh={specs(zh)} ru={specs(new)}"
    for m in WAKE_WORD_RE.finditer(zh or ""):
        if m.group(1) not in new:
            return None, f"wake-word {m.group(1)!r} потеряно"
    if _has_cjk(new) and (app, name) not in CJK_WHITELIST:
        stripped = WAKE_WORD_RE.sub("", new)
        if _has_cjk(stripped):
            return None, f"CJK осталось в ru: {new[:60]!r}"
    # длинный документ не может уложиться в крошечный ответ — отклоняем,
    # иначе записывается обрезанный/сброшенный перевод как «ок»
    if len(zh) >= LONG_THRESHOLD and len(new) < LONG_RATIO_MIN * len(zh):
        return None, (f"слишком короткий для length: len(ru)={len(new)} < "
                      f"{LONG_RATIO_MIN:g}·len(zh)={len(zh)}")
    # режим --fit: жёсткий верхний лимит ширины коротких UI-строк. Принятый
    # ответ ОБЯЗАН быть короче текущего ru ПО ШИРИНЕ (см. guard_fit) И
    # укладываться в hard_cap (в символах) — иначе смысл в UI «переполнит»
    # фиксированный виджет и перенесётся на 2 строки.
    if fit:
        cur_w = display_width(ru_current or "")
        new_w = display_width(new)
        if new_w >= cur_w and cur_w > 0:
            return None, (f"fit: не короче текущего: w={new_w} >= "
                          f"w(текущий)={cur_w}")
        cap = hard_cap(zh)
        if len(new) > cap:
            return None, (f"fit: не влезает в cap: len={len(new)} > "
                          f"cap={cap} (zh_w={display_width(zh)})")
    return new, None


# ---------------------------------------------------------------------------
# API (OpenAI-совместимый; парсинг — как в api_translate_ambiguity.py)
# ---------------------------------------------------------------------------

def system_prompt() -> str:
    if not PROMPT_MD.exists():
        sys.exit(f"Не найден системный промпт: {PROMPT_MD}")
    return PROMPT_MD.read_text(encoding="utf-8")


def build_payload(batch: list[dict], hint: str = "") -> dict:
    rows = [
        {"id": it["id"], "zh": it["zh"], "ru": it["ru"], "type": it.get("type", "string")}
        for it in batch
    ]
    user = (
        "Улучши/переведи строки по системным инструкциям.\n"
        "Вход — JSON-массив объектов. Ответ: ТОЛЬКО JSON-массив вида "
        '[{"id": "<id>", "ru": "<улучшенный перевод>"}, ...] — все id, '
        "порядок как во входе, без markdown.\n\n"
        + json.dumps(rows, ensure_ascii=False)
    )
    if hint:
        user += "\n\nВАЖНО, учти при ответе на каждый id: " + hint
    return {
        "model": API_MODEL,
        "max_tokens": API_MAX_TOKENS,
        "temperature": 0.5,
        "messages": [
            {"role": "system", "content": system_prompt()},
            {"role": "user", "content": user},
        ],
    }


def build_fit_payload(batch: list[dict], hard_mode: bool = False,
                      hint: str = "") -> dict:
    """Пайлоад режима --fit: по id — текущий (переширокий) ru + лимит.

    hard_mode=False (мягкий проход): просим «сделать короче при сохранении
    смысла», без жёсткого числа. hard_mode=True (эскалация): явный cap в
    СИМВОЛАХ на каждый id — модель обязана уложиться (guard принимает
    ответ только <= cap).
    """
    rows = []
    for it in batch:
        row = {
            "id": it["id"], "zh": it["zh"], "ru": it["ru"],
            "type": it.get("type", "string"),
        }
        if hard_mode:
            row["max_chars"] = hard_cap(it["zh"])
        rows.append(row)
    if hard_mode:
        user = (
            "Задача — УКОРОТИТЬ существующие русские переводы (они переносят "
            "виджет на 2 строки вместо одной, так как заметно длиннее "
            "китайского оригинала).\n"
            "Для каждого id дано: zh (оригинал), ru (текущий переширокий "
            "перевод), max_chars (жёсткий предел ДЛИНЫ в символах).\n"
            "Ответ: ТОЛЬКО JSON-массив [{\"id\": ..., \"ru\": ...}] на все id.\n"
            "ТРЕБОВАНИЯ к каждому ru:\n"
            "1. длина (в СИМВОЛАХ) <= max_chars этого id — НЕ ПРЕВЫШАТЬ "
            "ни на один символ (каждый знак, пробел и плейсхолдер "
            "засчитывается);\n"
            "2. смысл сохранён, термин кратчайший естественный (UI-лейбл); "
            "без пояснений в скобках, без «функция/режим/система», если "
            "понятно из контекста;\n"
            "3. плейсхолдеры %s/%d/%.1f/%1$s/%%%% и wake-words ***...*** — "
            "1:1 как в zh (число, порядок, номера);\n"
            "4. без CJK (кроме wake-word), без markdown.\n\n"
            + json.dumps(rows, ensure_ascii=False)
        )
    else:
        user = (
            "Задача — УКОРОТИТЬ существующие русские переводы (они заметно "
            "длиннее китайского оригинала и переносят виджет на 2 строки).\n"
            "Для каждого id дано: zh (оригинал), ru (текущий перевод).\n"
            "Ответ: ТОЛЬКО JSON-массив [{\"id\": ..., \"ru\": ...}] на все id.\n"
            "ТРЕБОВАНИЯ: каждое новое ru — КОРОЧЕ текущего ru при сохранённом "
            "смысле; кратчайшая естественная UI-форма (термин вместо "
            "описания, без скобок/уточнений); плейсхолдеры и wake-words 1:1; "
            "без CJK; без markdown.\n\n"
            + json.dumps(rows, ensure_ascii=False)
        )
    if hint:
        user += "\n\nВАЖНО, учти при ответе на каждый id: " + hint
    return {
        "model": API_MODEL,
        "max_tokens": API_MAX_TOKENS,
        "temperature": 0.5,
        "messages": [
            {"role": "system", "content": system_prompt()},
            {"role": "user", "content": user},
        ],
    }


def _call_api(payload: dict) -> str:
    body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
    req = urllib.request.Request(
        API_URL,
        data=body,
        headers={
            "Authorization": f"Bearer {API_KEY}",
            "Content-Type": "application/json",
            "User-Agent": "deepal-translate/improve",
        },
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=API_TIMEOUT) as resp:
        return resp.read().decode("utf-8")


def _inner_json_normalize(content: str) -> str:
    """Починить нестандартные escape локальных моделей (как в api_translate)."""
    content = re.sub(r"\\x([0-9A-Fa-f]{2})", r"\\u00\1", content)
    content = content.replace(r"\\\"", r"\"")
    content = content.replace('\\"', '"')
    return content


def parse_response(response_text: str) -> list[dict[str, Any]] | None:
    """[{id, ru}] из ответа: OpenAI-формат / чистый JSON / markdown-fence."""
    cleaned = (response_text or "").strip()
    if cleaned.startswith("```"):
        cleaned = cleaned.strip("`")
        for head in ("json", "py", "python"):
            if cleaned.startswith(head):
                cleaned = cleaned[len(head):].lstrip("\n\r")
                break
        if cleaned.startswith("```"):
            cleaned = cleaned.strip("`\n\r")
    if cleaned.startswith("{"):
        try:
            payload = json.loads(cleaned)
            cleaned = payload["choices"][0]["message"]["content"]
        except (KeyError, json.JSONDecodeError, IndexError, TypeError):
            return None
    if not cleaned.startswith("["):
        return None
    try:
        data = json.loads(cleaned)
    except json.JSONDecodeError:
        try:
            data = json.loads(_inner_json_normalize(cleaned))
        except json.JSONDecodeError:
            return None
    return data if isinstance(data, list) else None


# ---------------------------------------------------------------------------
# прогресс
# ---------------------------------------------------------------------------

def load_progress(path: Path = PROGRESS_PATH) -> set[str]:
    if path.exists():
        try:
            return set(json.loads(path.read_text(encoding="utf-8")).get("done", []))
        except (OSError, json.JSONDecodeError):
            LOGGER.warning("progress бит — начинаю заново (%s)", path.name)
    return set()


def save_progress(done: set[str], path: Path = PROGRESS_PATH) -> None:
    tmp = path.with_suffix(".json.tmp")
    with open(tmp, "w", encoding="utf-8") as f:
        json.dump({"done": sorted(done), "model": API_MODEL,
                   "url": API_URL, "ts": int(time.time())}, f, ensure_ascii=False)
    os.replace(tmp, path)


# ---------------------------------------------------------------------------
# режим --styled: rich-text с разметкой (пер-фрагмент, разметка 1:1)
# ---------------------------------------------------------------------------

def _styled_tokenize(zh: str) -> list[list[str]]:
    """[(kind, val)] kind T=text | K=markup-token (побайто из zh)."""
    parts: list[list[str]] = []
    pos = 0
    for m in STYLED_TOKEN.finditer(zh):
        if m.start() > pos:
            parts.append(["T", zh[pos:m.start()]])
        parts.append(["K", m.group(0)])
        pos = m.end()
    if pos < len(zh):
        parts.append(["T", zh[pos:]])
    return parts


def _styled_toks(parts: list[list[str]]) -> list[str]:
    return [v for k, v in parts if k == "K"]


def _styled_cjk_frags(parts: list[list[str]]) -> list[dict]:
    """CJK-фрагменты: {tidx, head, pre, core, post, tail}.

    pre/post — «голое тело сущности» вида emsp; (остаток double-encoded
    &amp;emsp;: &amp; — token, emsp; — текст). Модель НЕ видит pre/post
    (иначе добавит & → новый сущностный token → рассинхрон каркаса);
    они подставляются побайтово вокруг перевода core."""
    frags: list[dict] = []
    tidx = 0
    for kind, val in parts:
        if kind != "T":
            continue
        if _has_cjk(val):
            m = re.match(r"^(\s*)(.*?)(\s*)$", val, re.S)
            head, body, tail = m.group(1), m.group(2), m.group(3)
            pm = re.match(r"^([A-Za-z]+;)( ?)", body)
            pre = pm.group(1) if pm else ""
            body2 = body[len(pre):]
            pm2 = re.search(r"( ?)([A-Za-z]+;)$", body2)
            post = pm2.group(2) if pm2 else ""
            core = body2[:len(body2) - len(post)]
            frags.append({"tidx": tidx, "head": head, "pre": pre,
                          "core": core, "post": post, "tail": tail})
        tidx += 1
    return frags


def _styled_check(core: str, ru: str) -> str | None:
    """Итоговая проверка фрагмента. None — ок, иначе причина."""
    ru = (ru or "").strip().strip("`").strip()
    if not ru or ru.lower() in DEGENERATE_ANSWERS or ru.lower() in {"нет", "—", "-", "."}:
        return "empty"
    if _has_cjk(ru):
        return f"cjk-left: {ru[:50]!r}"
    if "%" in ru and "%" not in core:
        return "formatter-appears"
    if len(core.strip()) >= 4 and len(ru.strip()) < 0.20 * len(core.strip()):
        return f"too-short: {len(ru)}<{0.2 * len(core.strip()):.0f}"
    return None


def _styled_has_token(s: str) -> bool:
    return bool(STYLED_TOKEN.search(s or ""))


STYLED_SYS = (
    "Ты — переводчик zh→ru для Android/автосистемы. Ответь ОДНИМ строковым значением — "
    "тотже руссcкий перевод введенного фрагмента. Без пояснений, без кавычек-обёрток, без JSON. "
    "Форматтеры %s/%d/%1$s не изменяй. Дата по-русски: 2022年06月10日 -> 10.06.2022. "
    "Адреса/имена транслитерируй. 高德/Amap = Amap, 深蓝汽车/Deepal = Deepal. "
    "Используй запятую, точку, тире как в исходнике; не сокращай смысл."
)


def _styled_call(core: str, max_tokens: int) -> str:
    payload = {
        "model": API_MODEL, "stream": False, "temperature": 0.4,
        "max_tokens": max_tokens,
        "messages": [{"role": "system", "content": STYLED_SYS},
                     {"role": "user", "content": core}],
    }
    raw = _call_api(payload)
    content = _extract_content(raw) or ""
    c = content.strip()
    c = re.sub(r"^```[a-zA-Z]*\s*", "", c)
    c = re.sub(r"\s*```$", "", c)
    return c.strip().strip('"').strip("'").strip()


def _styled_translate_one(app: str, name: str, core: str) -> str | None:
    """Пер-фрагмент с ретраями. None — не получилось после STYLED_MAX_RETRY."""
    for i in range(STYLED_MAX_RETRY):
        try:
            v = _styled_call(core, _styled_max_tokens(core))
        except Exception as e:
            LOGGER.warning("styled %s/%s try=%d API %s %s", app, name, i + 1,
                           type(e).__name__, str(e)[:120])
            time.sleep(1.0 + i * 0.1)
            continue
        err = _styled_check(core, v)
        if err is None:
            return v
        if i % 5 == 4:
            LOGGER.warning("styled %s/%s try=%d %s tail=%r",
                           app, name, i + 1, err, v[:50])
        time.sleep(1.0 + 0.3 * i)
    return None


def _styled_reassemble(parts: list[list[str]], trans: dict[int, str],
                       frags: list[dict]) -> str | None:
    """Пересборка: токены 1:1, core -> перевод (head/pre/post/tail побайто)."""
    frag_at = {f["tidx"]: f for f in frags}
    out: list[str] = []
    tidx = 0
    for kind, val in parts:
        if kind == "K":
            out.append(val)
            continue
        fi = frag_at.get(tidx)
        if fi is None:
            out.append(val)
        else:
            lines = [l.strip() for l in (trans[tidx] or "").split("\n") if l.strip()]
            core_ru = "\n".join(lines)
            out.append(f'{fi["head"]}{fi["pre"]}{core_ru}{fi["post"]}{fi["tail"]}')
        tidx += 1
    ru = "".join(out)
    if _styled_toks(_styled_tokenize(ru)) != _styled_toks(parts):
        return None
    return ru


def _load_styled_cache() -> dict:
    if STYLED_CACHE_PATH.exists():
        try:
            d = json.loads(STYLED_CACHE_PATH.read_text(encoding="utf-8"))
            if isinstance(d, dict):
                return d
        except (OSError, json.JSONDecodeError):
            LOGGER.warning("styled cache бит (%s) — начинаю заново", STYLED_CACHE_PATH.name)
    return {}


def _save_styled_cache(cache: dict) -> None:
    (BASE_DIR / "logs").mkdir(parents=True, exist_ok=True)
    tmp = STYLED_CACHE_PATH.with_suffix(".json.tmp")
    with open(tmp, "w", encoding="utf-8") as f:
        json.dump(cache, f, ensure_ascii=False, indent=1)
    os.replace(tmp, STYLED_CACHE_PATH)


def _styled_translate_entry(app: str, name: str, zh: str,
                            cache: dict) -> str | None:
    """Целая styled-строка по фрагментам (с кэшем per-фрагмент)."""
    parts = _styled_tokenize(zh)
    frags = _styled_cjk_frags(parts)
    trans: dict[int, str] = {}
    for f in frags:
        key = f"{app}/{name}#{f['tidx']}"
        v = cache.get(key)
        if v is not None:
            if _styled_has_token(v) or _styled_check(f["core"], v) is not None:
                LOGGER.warning("styled %s: кэш #tidx=%d битый — перевызов", key, f["tidx"])
                v = None
        if v is None:
            v = _styled_translate_one(app, name, f["core"])
            if v is None:
                LOGGER.warning("styled %s: tidx=%d FAILED (ядро=%r)",
                               key, f["tidx"], f["core"][:60])
                return None
            cache[key] = v
            _save_styled_cache(cache)
        trans[f["tidx"]] = v
    return _styled_reassemble(parts, trans, frags)


def _styled_defective(app: str, name: str, zh: str, ru: str) -> bool:
    """Текущее ru styled-строки дефектное (нужен перевод)."""
    if not ru:
        return True
    if ru.lower() in DEGENERATE_ANSWERS:
        return True
    if _has_cjk(ru) and (app, name) not in CJK_WHITELIST:
        return True
    if specs(zh) != specs(ru):
        return True
    return False


def run_styled(only_app: str | None, limit: int | None, dry_run: bool) -> None:
    """Перевод styled-строк с пустым/дефектным ru (режим --styled)."""
    cache = _load_styled_cache()
    done_n = 0
    stuck_n = 0
    skipped = 0
    total = 0
    for app in list_apps():
        if only_app and app != only_app:
            continue
        data = load_app(app)
        for e in data:
            if not e.get("styled"):
                continue
            name = e.get("name", "")
            zh = (e.get("zh") or "").strip()
            ru = (e.get("ru") or "").strip()
            if not zh or not _has_cjk(zh):
                continue
            total += 1
            if not _styled_defective(app, name, zh, ru):
                skipped += 1
                continue
            if limit is not None and done_n + stuck_n >= limit:
                break
            result = _styled_translate_entry(app, name, e.get("zh") or "", cache)
            if result is None:
                stuck_n += 1
                (BASE_DIR / "logs").mkdir(parents=True, exist_ok=True)
                with open(STYLED_STUCK_PATH, "a", encoding="utf-8") as f:
                    f.write(f"{app}/{name}\n")
                continue
            if _has_cjk(result) and (app, name) not in CJK_WHITELIST:
                stuck_n += 1
                LOGGER.warning("styled %s/%s: CJK в результате — не записываю", app, name)
                continue
            if dry_run:
                continue
            e["ru"] = result
            save_app(app, data)
            done_n += 1
            LOGGER.info("styled %s/%s: записано (ru %d симв.)", app, name, len(result))
    print(f"\nИТОГО styled: переведено {done_n}, ок (skip) {skipped}, "
          f"не переведено {stuck_n} (всего {total}). Не переведённые: {STYLED_STUCK_PATH}")


# ---------------------------------------------------------------------------
# batчи + починка CJK-остатков
# ---------------------------------------------------------------------------

CJK_RUN_RE = re.compile(r"[\u4e00-\u9fff]+")
# Сколько CJK-фрагментов можно ещё чинить точечно; больше — ответ модели
# развалился, ремонт не поможет, пусть уходит в полный повтор.
REPAIR_MAX_RUNS = 50
REPAIR_CONTEXT = 50  # символов рус. контекста по бокам фрагмента


def _extract_content(raw: str) -> str | None:
    """content из Ollama-конверта (или маркдаун-обёртка)."""
    cleaned = (raw or "").strip()
    if cleaned.startswith("{"):
        try:
            payload = json.loads(cleaned)
            cleaned = payload["choices"][0]["message"]["content"]
        except (KeyError, json.JSONDecodeError, IndexError, TypeError):
            return None
    if cleaned.startswith("```"):
        cleaned = cleaned.strip("`")
        for head in ("json", "py", "python"):
            if cleaned.startswith(head):
                cleaned = cleaned[len(head):].lstrip("\n\r")
                break
        if cleaned.startswith("```"):
            cleaned = cleaned.strip("`\n\r")
    return cleaned


def _parse_repair_response(raw: str) -> list[dict] | None:
    """Lenient-разбор [{n, window_ru}]. Если json.loads падает (модель вставляет
    живые \\n / битые escape в окно) — скан с учтом backslash-экранирования:
    невалидный escape читается как сам символ, строка берётся целиком."""
    cleaned = _extract_content(raw)
    if not cleaned:
        return None
    try:
        data = json.loads(cleaned)
        if isinstance(data, list):
            return data
    except json.JSONDecodeError:
        pass
    out: list[dict] = []
    for mobj in re.finditer(r'\{\s*"n"\s*:\s*(\d+)\s*,\s*"window_ru"\s*:\s*"', cleaned):
        n = int(mobj.group(1))
        pos = mobj.end()
        buf: list[str] = []
        ok = False
        map_esc = {'"': '"', '\\': '\\', '/': '/', 'b': '\b', 'f': '\f',
                   'n': '\n', 'r': '\r', 't': '\t'}
        while pos < len(cleaned):
            ch = cleaned[pos]
            if ch == "\\":
                nxt = cleaned[pos + 1] if pos + 1 < len(cleaned) else ""
                if nxt in map_esc:
                    buf.append(map_esc[nxt]); pos += 2; continue
                if nxt == "u" and pos + 5 < len(cleaned) and \
                        re.fullmatch(r"[0-9A-Fa-f]{4}", cleaned[pos + 2:pos + 6]):
                    buf.append(chr(int(cleaned[pos + 2:pos + 6], 16))); pos += 6; continue
                buf.append(nxt or ""); pos += 2; continue  # невалидный escape: сам символ
            if ch == '"':
                pos += 1; ok = True
                break
            buf.append(ch); pos += 1
        if not ok:
            return None
        out.append({"n": n, "window_ru": "".join(buf)})
    return out or None


def _repair_cjk(text: str) -> str | None:
    """Починить CJK-фрагменты в ИТОГОВОМ переводе без регенерации всего текста.

    Модель на длинных документах оставляет 2–5 китайских слов/фрагментов среди
    десятков тысяч нормальных русских символов. Полный повтор (5+ минут)
    роняет уже готовое и оставляет другие фрагменты. Здесь: для каждого
    фрагмента маленькая отправка — целый рус. «окно» вокруг фрагмента, модель
    возвращает окно ПОЛНОСТЬЮ исправленным (фрагмент + границы слов/пробелы
    уходят за неё), подстановка окна в исходный текст. 3 попытки на bad-JSON.
    Возврат — починенный текст (если CJK не осталось) или None (не сработало)."""
    runs = list(CJK_RUN_RE.finditer(text))
    if not runs:
        return text
    if len(runs) > REPAIR_MAX_RUNS:
        return None
    windows = []
    for i, mm in enumerate(runs):
        s, e = mm.span()
        ws, we = max(0, s - REPAIR_CONTEXT), min(len(text), e + REPAIR_CONTEXT)
        windows.append((ws, we, text[ws:we]))
    frags = [{"n": i + 1, "window_ru": win} for i, (ws, we, win) in enumerate(windows)]
    user = (
        "В готовом русском переводе остались китайские фрагменты. Для каждого n "
        "ниже `window_ru` — готовый русский текст, внутри которого ЕСТЬ китайский "
        "фрагмент. Верни ЭТОТ ОКНО, переведя фрагмент на русский с учётом соседей "
        "(смысл, род/число/падеж, пробелы/знаки препинания на границах). Всё "
        "остальное в окне НЕ меняй. Ответ: ТОЛЬКО JSON-массив [{\"n\": 1, "
        "\"window_ru\": \"<исправленное окно, целиком>\"}] — один объект на "
        "каждый n, без markdown.\n"
        + json.dumps(frags, ensure_ascii=False)
    )
    parsed = None
    for attempt in range(1, 4):
        raw = None
        try:
            raw = _call_api({
                "model": API_MODEL, "max_tokens": 8000, "temperature": 0.5,
                "messages": [{"role": "system", "content": system_prompt()},
                             {"role": "user", "content": user}],
            })
        except Exception as e:  # noqa: BLE001
            LOGGER.warning("repair API error: %s: %s", type(e).__name__, e)
        if raw is not None:
            parsed = _parse_repair_response(raw)
            if parsed:
                break
            LOGGER.warning("repair: bad JSON (попытка %d); head=%r",
                           attempt, raw[:120])
        time.sleep(1)
    if not parsed:
        return None
    fixed = text
    for i, res in enumerate(parsed):
        if not isinstance(res, dict) or res.get("n") != i + 1:
            return None  # порядок/набор окон нарушен — не рисковать сменой мест
        win_fixed = str(res.get("window_ru", "") or "").strip()
        if _has_cjk(win_fixed) or win_fixed == windows[i][2]:
            return None  # фрагмент не переведён — полный повтор будет в шаге 5
        fixed = fixed.replace(windows[i][2], win_fixed, 1)
    if CJK_RUN_RE.search(fixed):
        return None
    return fixed


def send_batch(chunk: list[dict]) -> list[tuple[dict, str | None, str]]:
    """Отправить батч. Возврат: [(item, value|None, 'ok'|'guard'|'fail')].

    До 3 попыток на строки, которые модель НЕ вернула. Отобранные guard'ом
    (пусто/CJK/formatters) получают ЕЩЁ ОДИН разовый повтор с точным
    указанием требуемого списка форматтеров — это основное промт-уточнение
    (модель любит переставлять %1$s/%2$s или терять %%). Строки с CJK-остатками
    в длинном переводе сначала чинятся ТОЧЕЧНО (`_repair_cjk`: только оставшиеся
    фрагменты с контекстом, без регенерации всего текста).
    """
    by_id = {it["id"]: it for it in chunk}
    accepted: dict[str, str] = {}
    guard_rejects: dict[str, str] = {}
    outstanding = list(chunk)
    last_responses: dict[str, str] = {}  # последний ответ модели на rid (сырой)

    def _try(items: list[dict], hint: str = "") -> None:
        nonlocal outstanding
        if not items:
            return
        try:
            raw = _call_api(build_payload(items, hint=hint))
        except Exception as e:  # noqa: BLE001
            LOGGER.warning("API error: %s: %s", type(e).__name__, e)
            time.sleep(2)
            return
        parsed = parse_response(raw)
        if parsed is None:
            LOGGER.warning("bad JSON; head=%r", raw[:200])
            return
        for res in parsed:
            if not isinstance(res, dict):
                continue
            rid = str(res.get("id", "")).strip()
            it = by_id.get(rid)
            if it is None:
                continue
            raw_ru = res.get("ru") or ""
            if isinstance(raw_ru, str) and raw_ru:
                last_responses[rid] = raw_ru
            # res["ru"] может быть JSON null (модель сдалась) — .get с default не
            # сработает (ключ есть), str(None) дал бы строку "None"; берём or ""
            value, reason = guard(it["app"], it["name"], it["zh"],
                                  res.get("ru") or "")
            if value is not None:
                accepted[rid] = value
                outstanding = [x for x in outstanding if x["id"] != rid]
            else:
                # повтор-проверка: если всё ещё guard, остаётся в rejects
                if reason not in guard_rejects:
                    guard_rejects[rid] = reason

    # 1) основной проход
    for attempt in range(1, 4):
        if not outstanding:
            break
        before = len(outstanding)
        _try(list(outstanding))
        if len(outstanding) == before:
            LOGGER.info("попытка %d: прогресса нет (%d строк)", attempt, before)
            time.sleep(1)

    # 2) повтор с точным hint'ом на guard-отклонённые по форматтерам
    placeholder_rejects = [
        it for rid, why in guard_rejects.items()
        if (it := by_id.get(rid)) and "формatters" in why
    ]
    if placeholder_rejects:
        LOGGER.info("повтор с hint на %d строк (placeholder)", len(placeholder_rejects))
        for it in placeholder_rejects:
            need = ", ".join(specs(it["zh"])) or "(без форматтеров, ни одного %)"
            hint = (
                f'Для id="{it["id"]}" В ОРИГИНАЛЕ (zh) ровно такие форматтеры '
                f"в таком порядке: {need}. Переведи смысл; эти форматтеры "
                "обязаны СОХРАНИТЬСЯ 1:1 (тоже по порядку). Проверь до ответа "
                "побуквенно."
            )
            _try([it], hint=hint)
        # обновим reject-статус для тех, что прошли
        for rid in list(guard_rejects):
            if rid in accepted:
                guard_rejects.pop(rid, None)

    # 3) ТОЧЕЧНАЯ починка CJK-остатков: модель на длинных документах оставляет
    #    2–5 китайских слов среди десятков тысяч нормальных символов. Повтор
    #    всего текста (5+ мин) роняет готовое и оставляет другие фрагменты —
    #    вместо этого чиним только сами фрагменты с русским контекстом.
    for rid, why in list(guard_rejects.items()):
        if rid in accepted or "CJK" not in why:
            continue
        it = by_id[rid]
        raw_ru = last_responses.get(rid, "")
        if not raw_ru:
            continue
        n_runs = len(CJK_RUN_RE.findall(raw_ru))
        if not raw_ru or n_runs > REPAIR_MAX_RUNS:
            continue
        repaired = _repair_cjk(raw_ru)
        if repaired is None:
            continue
        value, reason = guard(it["app"], it["name"], it["zh"], repaired)
        if value is not None:
            accepted[rid] = value
            outstanding = [x for x in outstanding if x["id"] != rid]
            guard_rejects.pop(rid, None)
            LOGGER.info("CJK-ремонт id=%s: %d фрагм. -> принят (len=%d)",
                        rid, n_runs, len(value))
        else:
            LOGGER.warning("CJK-ремонт id=%s не помог: %s", rid, reason)

    # 4) повтор с hint'ом на то, что не починил ремонт (модель оставляет
    #    китайские хвосты) — явно требуем: переведи ВЕСЬ текст, CJK=0.
    cjk_rejects = [
        it for rid, why in guard_rejects.items()
        if (it := by_id.get(rid)) and "CJK" in why and rid not in accepted
    ]
    if cjk_rejects:
        LOGGER.info("повтор с hint на %d строк (CJK-остатки)", len(cjk_rejects))
        for it in cjk_rejects:
            hint = (
                f'Для id="{it["id"]}": в предыдущем переводе ОСТАЛИСЬ КИТАЙСКИЕ '
                "символы. Переведи каждое такое слово/иероглиф на русский. В "
                "ответе не должно остаться ни одного CJK-символа (кроме "
                "wake-word внутри ***...***). Переведи весь текст целиком, "
                "без сокращений и без пропусков."
            )
            _try([it], hint=hint)
        for rid in list(guard_rejects):
            if rid in accepted:
                guard_rejects.pop(rid, None)

    out: list[tuple[dict, str | None, str]] = []
    for it in chunk:
        rid = it["id"]
        if rid in accepted:
            out.append((it, accepted[rid], "ok"))
        elif rid in guard_rejects:
            out.append((it, None, guard_rejects[rid]))
        else:
            out.append((it, None, "fail"))
    return out


def send_fit_batch(chunk: list[dict]) -> list[tuple[dict, str | None, str]]:
    """Один батч режима --fit. Возврат: [(item, value|None, статус)].

    Статусы:
      ok      — принят (укороченный) перевод;
      not-shorter — guard «не короче текущего» (строка остаётся, не сломана);
      no-fit  — жёсткий cap не достигнут ни мягким, ни жёстким проходом;
      fail    — модель не вернула строку (3 попытки).
    ДВА лимита последовательно на строку, которая не прошла мягкий проход:
      (1) мягкий : ответ принимаем, если display_width(new) < display_width
                   (текущий) И len(new) <= hard_cap — см. guard(fit=True);
      (2) жёсткий: повторный запрос С max_chars в пайлоаде (hard_mode=True);
                   guard требует len(new) <= max_chars.
    Текущий ru в translations/ НЕ трогается при любом не-ок исходе.
    """
    by_id = {it["id"]: it for it in chunk}
    accepted: dict[str, str] = {}
    reasons: dict[str, str] = {}

    def _run(items: list[dict], hard: bool) -> None:
        if not items:
            return
        try:
            raw = _call_api(build_fit_payload(items, hard_mode=hard))
        except Exception as e:  # noqa: BLE001
            LOGGER.warning("fit API error: %s: %s", type(e).__name__, e)
            time.sleep(2)
            return
        parsed = parse_response(raw)
        if parsed is None:
            LOGGER.warning("fit bad JSON; head=%r", raw[:200])
            return
        for res in parsed:
            if not isinstance(res, dict):
                continue
            rid = str(res.get("id", "")).strip()
            it = by_id.get(rid)
            if it is None or rid in accepted:
                continue
            value, reason = guard(it["app"], it["name"], it["zh"],
                                  res.get("ru") or "",
                                  fit=True, ru_current=it["ru"])
            if value is not None:
                accepted[rid] = value
            else:
                reasons[rid] = reason or reasons.get(rid, "fail")

    # 1) мягкий проход: до 3 попыток (модель может не вернуть часть id)
    outstanding = list(chunk)
    for attempt in range(1, 4):
        todo = [it for it in outstanding if it["id"] not in accepted]
        if not todo:
            break
        before = len(todo)
        _run(todo, hard=False)
        still = [it for it in todo if it["id"] not in accepted]
        if len(still) == before:
            LOGGER.info("fit мягкий: попытка %d прогресса нет (%d)", attempt, before)
            time.sleep(1)

    # 2) жёсткий проход: всё, что мягкий НЕ принял, и только если текущий ru
    #    длиннее hard_cap (в символах) — иначе короче-текущего ответа cap'ом
    #    покрывается автоматически и повторный запрос не нужен.
    esc = [it for it in chunk
           if it["id"] not in accepted and len(it["ru"]) > hard_cap(it["zh"])]
    LOGGER.info("fit жёсткий: %d строк с явным cap (max_chars в пайлоаде)", len(esc))
    for it in esc:
        _run([it], hard=True)

    out: list[tuple[dict, str | None, str]] = []
    for it in chunk:
        rid = it["id"]
        if rid in accepted:
            out.append((it, accepted[rid], "ok"))
        elif rid in reasons:
            why = reasons[rid]
            st = "not-shorter" if "не короче" in why else (
                "no-fit" if "cap" in why else "fail")
            out.append((it, None, st))
        else:
            out.append((it, None, "fail"))
    return out


# ---------------------------------------------------------------------------
# основной цикл
# ---------------------------------------------------------------------------

def collect_targets(mode: str, only_app: str | None) -> list[dict]:
    targets = []
    for app in list_apps():
        if only_app and app != only_app:
            continue
        try:
            data = load_app(app)
        except (OSError, ValueError, json.JSONDecodeError) as e:
            LOGGER.error("%s: %s", app, e)
            continue
        for i, entry in enumerate(data):
            name = entry.get("name", "")
            if name and is_target(entry, mode, app, name):
                targets.append({
                    "app": app,
                    "entry_index": i,
                    "name": name,
                    "type": entry.get("type", "string"),
                    "zh": entry.get("zh", ""),
                    "ru": entry.get("ru", ""),
                    "id": f"{app}::{name}",
                })
    return targets


def _make_chunks(items: list[dict]) -> list[list[dict]]:
    """Делит строки приложения на батчи: длинные (len(zh)>=LONG_THRESHOLD)
    идут ПО ОДНОЙ, короткие — группами по BATCH_SIZE. Порядок сохраняется."""
    chunks: list[list[dict]] = []
    cur: list[dict] = []
    for it in items:
        if len(it["zh"]) >= LONG_THRESHOLD:
            if cur:
                chunks.append(cur)
                cur = []
            chunks.append([it])  # длинная — по одной, не смешивать с короткими
        else:
            if len(cur) >= max(BATCH_SIZE, 1):
                chunks.append(cur)
                cur = []
            cur.append(it)
    if cur:
        chunks.append(cur)
    return chunks


def run(targets: list[dict], mode: str, resume: bool, dry_run: bool,
        limit: int | None) -> int:
    # Фильтр progress'ом — только для --all (resume). Для --defective /
    # --empty / --long НЕ фильтруем: они сами выбирают, ЧТО проблемное, и
    # обязаны уметь повторять то, что упало ранее (оно уже в done).
    # ВАЖНО: для не-all режимов ОСНОВНОЙ done НЕ стираем — берём его как базу
    # и только присоединяем, иначе save_progress затрёт накопленный прогресс
    # (done) целого --all-прогона. Для --all сохраняем исходную семантику.
    # --fit: собственный прогресс-файл (уменьшенные строки), всегда фильтрует
    # pending: повторный прогон без --fresh НЕ должен укорачивать уже
    # укороченное (риск переусечения смысла). "stuck"-строки (не короче /
    # не влезло / fail) в done НЕ попадают и повторятся в следующем прогоне.
    fit = mode == "fit"
    ppath = FIT_PROGRESS_PATH if fit else PROGRESS_PATH
    if mode == "all":
        done = load_progress(ppath) if resume else set()
        pending = [t for t in targets if t["id"] not in done]
    elif fit:
        done = load_progress(ppath)
        pending = [t for t in targets if t["id"] not in done]
    else:
        done = load_progress(ppath)     # merge-база: не losing основной прогресс
        pending = list(targets)
    if limit:
        pending = pending[:limit]

    n_long = sum(1 for t in pending if len(t["zh"]) >= LONG_THRESHOLD)
    n_fit = len(pending) if fit else 0
    print(f"Целей: {len(targets)}, к отправке: {len(pending)} (длинных: {n_long})"
          + (f", fit-кандидатов: {n_fit}" if fit else "")
          + f" | batch={BATCH_SIZE} (длинные — по 1) | model={API_MODEL} | url={API_URL}")

    by_app: dict[str, list[dict]] = {}
    for t in pending:
        by_app.setdefault(t["app"], []).append(t)
    app_chunks = {app: _make_chunks(items) for app, items in by_app.items()}
    total_chunks = sum(len(c) for c in app_chunks.values())

    if dry_run:
        print("\n=== DRY RUN: превью первых батчей (длинные помечены [LONG]) ===")
        shown = 0
        for app, chunks in app_chunks.items():
            for chunk in chunks:
                is_long = len(chunk) == 1 and len(chunk[0]["zh"]) >= LONG_THRESHOLD
                print(f"--- {app} ({len(chunk)} строк)"
                      + (" [LONG, 1 шт]" if is_long else "") + " ---")
                for it in chunk[:2]:
                    print(f"  {it['id']}  (len zh={len(it['zh'])})")
                    print(f"    zh: {it['zh'][:60]!r}")
                    if fit:
                        print(f"    ru: {it['ru'][:60]!r} (w={display_width(it['ru'])}, "
                              f"cap={hard_cap(it['zh'])})")
                shown += 1
                if shown >= 3:
                    break
            if shown >= 3:
                break
        print("\nDry-run: ничего не отправлено.")
        return 0

    if not pending:
        print("Отправлять нечего (всё в progress / нет дефектных).")
        return 0

    updated = guarded = failed = 0
    fit_shrunk = fit_stuck = 0
    stuck: list[tuple[str, str, str]] = []   # (app/name, zh, ru) для отчёта
    batch_no = 0
    for app, chunks in app_chunks.items():
        data = load_app(app)
        for bi, chunk in enumerate(chunks, 1):
            batch_no += 1
            is_long = len(chunk) == 1 and len(chunk[0]["zh"]) >= LONG_THRESHOLD
            t0 = time.monotonic()
            results = send_fit_batch(chunk) if fit else send_batch(chunk)
            dt = time.monotonic() - t0
            n_ok = sum(1 for r in results if r[1] is not None)
            n_guard = sum(1 for r in results if r[1] is None and r[2] != "fail")
            n_fail = sum(1 for r in results if r[1] is None and r[2] == "fail")
            LOGGER.info("[%d/%d] %s batch %d/%d%s: %.1fs ok=%d guard=%d fail=%d",
                        batch_no, total_chunks, app, bi, len(chunks),
                        " [LONG]" if is_long else "",
                        dt, n_ok, n_guard, n_fail)
            for it, value, reason in results:
                if value is not None:
                    data[it["entry_index"]]["ru"] = value
                    updated += 1
                    if fit:
                        fit_shrunk += 1
                else:
                    if reason == "fail":
                        failed += 1
                        if fit:
                            fit_stuck += 1
                            stuck.append((f"{app}/{it['name']}", it["zh"], it["ru"]))
                    else:
                        guarded += 1
                        if fit:
                            fit_stuck += 1
                            stuck.append((f"{app}/{it['name']}", it["zh"], it["ru"]))
                        else:
                            LOGGER.warning("GUARD %s: %s", it["id"], reason)
                # fit: в done попадают ТОЛЬКО укороченные (не укорачивать
                # повторно = не рисковать переусечением). Не-fit: исходная
                # семантика — done.add для КАЖДОЙ обработанной строки.
                if value is not None or not fit:
                    done.add(it["id"])
            save_app(app, data)
            save_progress(done, ppath)

    if fit:
        print(f"\nИТОГО fit: укорочено {fit_shrunk}, "
              f"не укорочено (текущее ru сохранено) {fit_stuck}")
        if stuck:
            FIT_STUCK_PATH.parent.mkdir(parents=True, exist_ok=True)
            with open(FIT_STUCK_PATH, "w", encoding="utf-8") as f:
                f.write(f"# fit stuck: {len(stuck)} строк, текущее ru НЕ менялось\n")
                f.write(f"# формат: app/name | zh (w=ширина) | ru (w=ширина, "
                        f"cap=max_символов)\n")
                for nm, zh, ru in stuck:
                    f.write(f"{nm} | zh[{display_width(zh)}]: {zh} | "
                            f"ru[{display_width(ru)},cap={hard_cap(zh)}]: {ru}\n")
            print(f"Список не укороченных: {FIT_STUCK_PATH}")
            print("(ручное ревью: часть слов просто не имеет короткого "
                  "русского эквивалента)")
    else:
        print(f"\nИТОГО: принято {updated}, guard {guarded}, не переведено {failed}")
        print(f"Прогресс: {ppath}")
    if updated:
        print("Дальше: python3 validate.py && python3 generate_overlays.py "
              "&& python3 create_rro_min.py (+ static)")
    return 0 if failed == 0 else 2


def main() -> int:
    ap = argparse.ArgumentParser(
        description="Улучшение/перевод строк в translations/*.json (запись в ru).",
        formatter_class=argparse.RawDescriptionHelpFormatter)
    grp = ap.add_mutually_exclusive_group()
    grp.add_argument("--all", dest="mode", action="store_const", const="all",
                     default="all", help="все CJK-строки (по умолчанию)")
    grp.add_argument("--defective", dest="mode", action="store_const",
                     const="defective",
                     help="только проблемные: пустой ru / CJK в ru / битые плейсхолдеры; "
                          "длинные + дегенеративные (None) / слишком короткие")
    grp.add_argument("--empty", dest="mode", action="store_const", const="empty",
                      help="только пустые ru")
    grp.add_argument("--long", dest="mode", action="store_const", const="long",
                      help="только длинные (len(zh) >= API_LONG_THRESHOLD) и дефектные; "
                           "каждая отправляется по одной")
    grp.add_argument("--fit", dest="mode", action="store_const", const="fit",
                      help="укоротить «раздутые» короткие UI-строки (RU шире ZH "
                           "сильнее порога strwidth); два лимита: мягкий "
                           "(только короче) + жёсткий (cap в символах, повтор); "
                           "прогресс — improve_fit_progress.json, stuck — "
                           "logs/fit_stuck.txt")
    grp.add_argument("--styled", dest="mode", action="store_const", const="styled",
                      help="rich-text с разметкой (<b>/<a>/<annotation>/<Data>…): "
                           "перевод по фрагментам, разметка 1:1; кэш — "
                           "improve_styled_cache.json, stuck — logs/styled_stuck.txt")
    ap.add_argument("--app", default=None, help="только одно приложение")
    ap.add_argument("--limit", type=int, default=None, help="максимум строк на запуск")
    ap.add_argument("--resume", action="store_true", help="продолжить с progress-файла")
    ap.add_argument("--fresh", action="store_true", help="игнорировать progress-файл")
    ap.add_argument("--dry-run", action="store_true", help="показать, не отправляя")
    args = ap.parse_args()

    ppath = FIT_PROGRESS_PATH if args.mode == "fit" else PROGRESS_PATH
    if args.fresh and ppath.exists():
        ppath.unlink()
    if DRY_RUN_ONLY:
        args.dry_run = True
    if not args.dry_run and not API_KEY:
        args.dry_run = True
        print("API_KEY пуст — перехожу в dry-run.")

    if args.mode == "styled":
        # отдельный путь (один вызов на фрагмент, свой кэш/verdict/стук)
        run_styled(only_app=args.app, limit=args.limit, dry_run=args.dry_run)
        return 0

    targets = collect_targets(args.mode, args.app)
    return run(targets, mode=args.mode, resume=args.resume, dry_run=args.dry_run,
               limit=args.limit)


if __name__ == "__main__":
    try:
        sys.exit(main())
    except KeyboardInterrupt:
        LOGGER.error("USER INTERRUPT")
        sys.exit(130)
    except Exception as e:  # noqa: BLE001
        LOGGER.error("FATAL: %s\n%s", e, traceback.format_exc())
        sys.exit(1)
