"""Общая метрика «ширины строки» для аудита и режима --fit.

Проблема: русский UI-текст часто заметно ШИРЕ китайского оригинала (CJK-знак
≈ 2 знака ширины, но рус. слово на то же значение длиннее на 50–300%). RRO
покрывает только `string` — layout виджетов (maxLines/ellipsize/ширина) не
меняется, поэтому переведённая строка, вдвое-втрое длиннее оригинала, может
перенестись на 2 строки вместо одной. Модуль даёт ЕДИНУЮ метрику + пороги для:
  - аудита в validate.py (какие короткие строки «раздулись»);
  - режима improve_translations.py --fit (переупаковка перешироких RU).

Модель ширины (информационная, для сравнения zh vs ru ВЗАИМНО):
  CJK / fullwidth (ПВ)              = 2 юнита
  латиница, цифры, ASCII-знаки,     = 1 юнит
  кириллица, прочие                 = 1 юнит
Смысл в отНОСИТЕЛЬНОМ размере (ru/zh), а не в абсолютных px; простата важна —
одна и та же функция в guard и в аудите.

Пороги (переопределяются env, как API_LONG_THRESHOLD; значения — результат
предварительного разбора 17 827 строк 18.09):
  FIT_ZH_MAX_WIDTH = 14   ширина zh короткого лейбла; строки шире — «текст»,
                          там перенос норма (кнопка/лейбл не раздувается)
  FIT_MAX_NEWLINES = 1    у multi-line строки свой layout, не трогаем
  FIT_SOFT_RATIO = 2.5    мягкий (основной) триаж: ru_w > 2.5·zh_w И
  FIT_SOFT_MIN_RU  = 24   ru_w >= 24 -> кандидат на --fit / WARN аудита.
                          По данным: 1067 строк / 71 приложение.
  FIT_HARD_RATIO = 2.0, FIT_HARD_PAD = 8, FIT_HARD_MAX = 28
                          жёсткий cap: max_chars = min(28, 2.0·zh_w + 8) —
                          эскалация для строк, которые мягкие лимиты не
                          укоротили достаточно (модель просим уложиться в
                          N символов; guard принимает только <= N).

  Пример zh «景点» (w=8): cap = min(28, 20) = 20 символов
  («Достопримечательность» = 22 — не влезает -> "не влезает");
  zh «预警» (w=4): cap = min(28, 16) = 16.
"""
from __future__ import annotations

import os

# ---------------------------------------------------------------------------
# ширина
# ---------------------------------------------------------------------------

def display_width(s: str | None) -> int:
    """Оценка видимой ширины строки: CJK/fullwidth = 2, остальное = 1."""
    w = 0
    for ch in s or "":
        o = ord(ch)
        if (0x4E00 <= o <= 0x9FFF or 0x3400 <= o <= 0x4DBF
                or 0x3000 <= o <= 0x303F or 0xFF00 <= o <= 0xFFEF):
            w += 2
        else:
            w += 1
    return w


# ---------------------------------------------------------------------------
# пороги
# ---------------------------------------------------------------------------

FIT_ZH_MAX_WIDTH = int(os.environ.get("FIT_ZH_MAX_WIDTH", "14"))
FIT_MAX_NEWLINES = int(os.environ.get("FIT_MAX_NEWLINES", "1"))
FIT_SOFT_RATIO = float(os.environ.get("FIT_SOFT_RATIO", "2.5"))
FIT_SOFT_MIN_RU = int(os.environ.get("FIT_SOFT_MIN_RU", "24"))
FIT_HARD_RATIO = float(os.environ.get("FIT_HARD_RATIO", "2.0"))
FIT_HARD_PAD = int(os.environ.get("FIT_HARD_PAD", "8"))
FIT_HARD_MAX = int(os.environ.get("FIT_HARD_MAX", "28"))


def is_short_ui(zh: str | None) -> bool:
    """Короткая однострочная UI-строка (кнопка/лейбл/элемент списка).

    Только такие строки реально «раздуваются» в фиксированном виджете:
    в длинном прозаическом тексте перенос на N строк — норма."""
    zh = zh or ""
    return zh.count("\n") <= FIT_MAX_NEWLINES and display_width(zh) <= FIT_ZH_MAX_WIDTH


def is_ru_oversized(zh: str | None, ru: str | None) -> bool:
    """Триаж --fit / аудита: ru заметно шире, чем мог бы быть zh."""
    zh = zh or ""
    ru = ru or ""
    if not zh or not ru:
        return False
    if not is_short_ui(zh):
        return False
    zw, rw = display_width(zh), display_width(ru)
    if zw <= 0:
        return False
    return rw > FIT_SOFT_RATIO * zw and rw >= FIT_SOFT_MIN_RU


def hard_cap(zh: str | None) -> int:
    """Жёсткий предельный размер (в СИМВОЛАХ) короткого перевода."""
    zw = display_width(zh or "")
    return max(6, min(FIT_HARD_MAX, int(FIT_HARD_RATIO * zw) + FIT_HARD_PAD))
