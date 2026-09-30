#!/usr/bin/env python3
"""Аудит «RU-перевод влезает в выделенную под него рамку», все приложения.

Сканирует decompiled/<app>/res/{layout*,menu}/*.xml: TextView|Button|EditText
с android:text="@string/NAME" (RU берём из translations/<app>.json).

Модель (условные px, density=1, dp==sp):
  - размер шрифта: attr android:textSize (dp/sp/@dimen) -> @dimen ->
    цепочка style (только stили из собственных values APK) -> default 14sp;
    bold = textStyle="bold" (attr или style); letterSpacing учитывается;
  - RU-ширина слова/строки: PIL + Roboto ГУ (regular/bold), линейное
    масштабирование (замер при 100px);
  - высота строки: (ascent+descent) Roboto при размере шрифта +
    lineSpacingExtra (style), если читается;
  - рамка (бюджет) текстового виджета:
      avail_w = собственная фикс. ширина (dp/@dimen) минус padding/margin;
                иначе ближайший предок с фикс. шириной (верхняя граница;
                0dp-constraint считаем «неизвестно» и идём выше);
                если предок = h-LinearLayout/TableRow — вычитаем фикс.
                не-текстовых соседей (иконки и пр.);
      avail_h = собственная фикс. высота иначе ближайший предок с фикс.
                высотой (аналогично).
      Если и ширины, и высоты нигде нет — рамка растёт вместе с текстом ->
      проверка не нужна (это зона width-ratio-аудита 1c в validate.py).
  - перенос: жадный word-wrap RU в avail_w (PIL). Высота строк zh и ru
    сравниваются с avail_h:
      CLIP  — maxLines=1/singleLine/ellipsize и RU не влезает в 1 линию;
      FAIL  — required_lines(RU) > lines_fit(avail_h) и рамки реальны
              (обрезка верха/снизу или справа — то, что и «…ольш»);
      WARN  — влезает, но RU в N>1 строк при zh в 1 (растяжение рамки,
              может сдвинуть layout в плотных диалогах) ИЛИ >92% по
              1-строчному расчёту.

Ограничения (осознанные, см. отчёт): multi-line строки (\\n) пропуск;
%s-аргументы не подставляются (база — нижняя оценка); стили внешних
библиотек (wtcl.lib.*) не резолвятся -> 14sp default (может занижать);
exact constraint-цепочки в ConstraintLayout не решаем -> avail по предку
(верхняя граница, т.е. меньше ложных FAIL, но возможны пропуски, если
виджет стоит в узкой левой части).
"""
from __future__ import annotations

import json
import re
import sys
from pathlib import Path

from PIL import ImageFont

ROOT = Path(__file__).resolve().parent
DECOMPILED = ROOT / "decompiled"
TRANS_DIR = ROOT / "translations"
LOGS = ROOT / "logs"

FONT_REG = ("/home/user/projects/deepal-firmware/extracted/mnt_sys/"
            "system/fonts/Roboto-Regular.ttf")
FONT_BOLD = ("/home/user/projects/deepal-firmware/extracted/mnt_sys/"
             "system/fonts/Roboto-Bold.ttf")
MS = 100  # measurement font size

_font_cache: dict[bool, ImageFont.FreeTypeFont] = {}


def font(bold: bool) -> ImageFont.FreeTypeFont:
    if bold not in _font_cache:
        _font_cache[bold] = ImageFont.truetype(
            FONT_BOLD if bold else FONT_REG, MS)
    return _font_cache[bold]


def wpx(s: str, bold: bool, ls: float = 0.0) -> float:
    """Ширина в условных px при условном font-size = MS(=100)."""
    if not s:
        return 0.0
    bb = font(bold).getbbox(s)
    w = float(bb[2] - bb[0])
    if ls:
        w += len(s) * ls * MS
    return w


_ESC_MAP = {"n": "\n", "t": "\t", "r": "\r", "\\": "\\"}


def unesc(s: str) -> str:
    """Android-ресурсные экскейпы (\\n, \\t, \\' ...) -> raw."""
    return re.sub(r"\\(n|t|r|\\)", lambda m: _ESC_MAP[m.group(1)], s)


def w_at(s: str, bold: bool, size: float, ls: float = 0.0) -> float:
    return wpx(s, bold, ls) * (size / MS) if size > 0 else 0.0


LINE_RATIO = None  # заполняется после first font access


def line_ratio() -> float:
    global LINE_RATIO
    if LINE_RATIO is None:
        f = font(False)
        asc, desc = f.getmetrics()
        LINE_RATIO = (asc + desc) / MS  # ~1.17 у Roboto
    return LINE_RATIO


# ---------------------------------------------------------------------------
# XML
# ---------------------------------------------------------------------------

TAG_RE = re.compile(r"<(/?)([A-Za-z_][\w.]*)\b([^>]*?)(/?)>", re.S)
ATTR_RE = re.compile(r'([A-Za-z_:][\w:.]*)="([^"]*)"')
SREF = re.compile(r"^@string/([A-Za-z0-9_.]+)$")
DREF = re.compile(r"^@dimen/([A-Za-z0-9_.]+)$")


def to_px(v: str | None) -> float | None:
    if not v:
        return None
    m = re.fullmatch(r"([-+]?\d+(?:\.\d+)?)(sp|dp|px|dip)?", v.strip())
    return float(m.group(1)) if m else None


class AppRes:
    def __init__(self, app: str):
        self.app = app
        self.dimens: dict[str, float | None] = {}
        self.dref: dict[str, str] = {}
        self.styles: dict[str, dict[str, str]] = {}
        self.spar: dict[str, str] = {}
        self.zhs: dict[str, str] = {}
        self.rus: dict[str, str] = {}
        self._load()

    def _load(self):
        res = DECOMPILED / self.app / "res"
        if not res.is_dir():
            return
        for xml in sorted(res.glob("values/*.xml")):
            self._parse(xml)
        jf = TRANS_DIR / f"{self.app}.json"
        if jf.exists():
            try:
                for e in json.loads(jf.read_text(encoding="utf-8")):
                    if e.get("type") == "string" and e.get("name"):
                        self.zhs[e["name"]] = e.get("zh") or ""
                        self.rus[e["name"]] = e.get("ru") or ""
            except Exception:
                pass

    def _parse(self, xml: Path):
        try:
            src = xml.read_text(encoding="utf-8", errors="replace")
        except OSError:
            return
        for m in re.finditer(r'<dimen\b[^>]*?\bname="([^"]+)"[^>]*>([^<]*)</dimen>', src):
            n, v = m.group(1), m.group(2).strip()
            d = DREF.fullmatch(v)
            if d:
                self.dref[n] = d.group(1)
            else:
                self.dimens[n] = to_px(v)
        for m in re.finditer(r'<string\b[^>]*?\bname="([^"]+)"[^>]*>\s*([^<]*?)\s*</string>', src):
            self.zhs.setdefault(m.group(1), m.group(2))
        for m in re.finditer(r'<style\b([^>]*)>(.*?)</style>', src, re.S):
            at = dict(ATTR_RE.findall(m.group(1)))
            n = at.get("name")
            if not n:
                continue
            self.spar[n] = at.get("parent", "")
            self.styles[n] = dict(ATTR_RE.findall(m.group(2)))

    def dv(self, name: str, depth: int = 0) -> float | None:
        if depth > 8:
            return None
        v = self.dimens.get(name)
        if v is not None:
            return v
        r = self.dref.get(name)
        return self.dv(r, depth + 1) if r else None

    def sa(self, st: str, attr: str, depth: int = 0):
        if depth > 8 or not st:
            return None
        it = self.styles.get(st)
        if it and attr in it:
            return it[attr]
        return self.sa(self.spar.get(st, ""), attr, depth + 1)


def tag_iter(lines):
    """Yield ('open', i, tag, attrs, selfclose) / ('close', i, tag)."""
    for i, line in enumerate(lines):
        for m in TAG_RE.finditer(line):
            is_close, tag, a_s, sc = m.groups()
            if tag in ("requestFocus", "include"):
                continue
            if is_close:
                yield "close", i, tag, None, False
            else:
                yield "open", i, tag, dict(ATTR_RE.findall(a_s)), sc == "/"


LEAF = {"TextView", "Button", "EditText", "ImageView", "ImageButton", "Space",
        "View", "ProgressBar", "CheckBox", "RadioButton", "Switch", "SeekBar",
        "ViewStub", "androidx.constraintlayout.widget.Group",
        "androidx.constraintlayout.widget.Guideline",
        "androidx.constraintlayout.widget.Group"}


class W:
    __slots__ = ("tag", "a", "line", "ch", "p")

    def __init__(self, tag, a, line):
        self.tag, self.a, self.line, self.ch, self.p = tag, a, line, [], None

    @property
    def wid(self):
        v = self.a.get("android:id")
        return v.split("/")[-1] if v else None


def trees(lines):
    roots, stack = [], []
    for kind, i, tag, a, sc in tag_iter(lines):
        if kind == "close":
            if stack and stack[-1].tag == tag:
                stack.pop()
            continue
        w = W(tag, a, i)
        if stack:
            w.p = stack[-1]
            stack[-1].ch.append(w)
        else:
            roots.append(w)
        if not sc and tag not in LEAF:
            stack.append(w)
    return roots


# ---------------------------------------------------------------------------
# размеры
# ---------------------------------------------------------------------------

def fixed(v: str | None, res: AppRes):
    """Фикс. размер dp/@dimen -> px, иначе None. 0dp/match/wrap -> None."""
    if not v:
        return None
    if v.startswith("@dimen/"):
        d = DREF.fullmatch(v)
        return res.dv(d.group(1)) if d else None
    m = re.fullmatch(r"([-+]?\d+(?:\.\d+)?)(dp|dip|sp)", v.strip())
    return float(m.group(1)) if m else None


def padh(w: W, res: AppRes) -> float:
    s = 0.0
    for k in ("android:paddingStart", "android:paddingEnd"):
        v = w.a.get(k)
        if v:
            s += (res.dv(DREF.fullmatch(v).group(1)) if v.startswith("@dimen/")
                  and DREF.fullmatch(v) else (to_px(v) or 0)) or 0
    v = w.a.get("android:paddingHorizontal")
    if v:
        s += 2 * ((res.dv(DREF.fullmatch(v).group(1)) if v.startswith("@dimen/") and DREF.fullmatch(v) else (to_px(v) or 0)) or 0)
    return s


def padv(w: W, res: AppRes) -> float:
    s = 0.0
    for k in ("android:paddingTop", "android:paddingBottom"):
        v = w.a.get(k)
        if v:
            s += (res.dv(DREF.fullmatch(v).group(1)) if v.startswith("@dimen/") and DREF.fullmatch(v) else (to_px(v) or 0)) or 0
    v = w.a.get("android:paddingVertical")
    if v:
        s += 2 * ((res.dv(DREF.fullmatch(v).group(1)) if v.startswith("@dimen/") and DREF.fullmatch(v) else (to_px(v) or 0)) or 0)
    return s


def margh(w: W, res: AppRes) -> float:
    s = 0.0
    for k in ("android:layout_marginStart", "android:layout_marginEnd"):
        v = w.a.get(k)
        if v:
            s += (res.dv(DREF.fullmatch(v).group(1)) if v.startswith("@dimen/") and DREF.fullmatch(v) else (to_px(v) or 0)) or 0
    v = w.a.get("android:layout_marginHorizontal")
    if v:
        s += 2 * ((res.dv(DREF.fullmatch(v).group(1)) if v.startswith("@dimen/") and DREF.fullmatch(v) else (to_px(v) or 0)) or 0)
    return s


TV_TAGS = ("TextView", "Button", "EditText")
HLL = ("LinearLayout", "RadioGroup", "TableRow")


def metrics(w: W, res: AppRes):
    """(size, prov, bold, ls, line_spacing_extra)."""
    bold = w.a.get("android:textStyle") == "bold"
    ls = 0.0
    lse = 0.0
    v = w.a.get("android:textSize")
    if v:
        p = res.dv(DREF.fullmatch(v).group(1)) if v.startswith("@dimen/") else to_px(v)
        if p is not None:
            m = w.a.get("android:letterSpacing")
            if m:
                ls = float(m)
            return p, "attr" + (f" @{v.split('/')[1]}" if v.startswith("@dimen/") else ""), bold, ls, lse
    st = w.a.get("style")
    if st:
        sn = st.split("/")[-1]
        if res.sa(sn, "android:textStyle") == "bold":
            bold = True
        sv = res.sa(sn, "android:textSize")
        if sv:
            p = res.dv(DREF.fullmatch(sv).group(1)) if sv.startswith("@dimen/") else to_px(sv)
            if p is not None:
                ls = float(res.sa(sn, "android:letterSpacing") or 0)
                lv = res.sa(sn, "android:lineSpacingExtra")
                if lv:
                    lse = (res.dv(DREF.fullmatch(lv).group(1)) if lv.startswith("@dimen/") and DREF.fullmatch(lv) else (to_px(lv) or 0)) or 0
                return p, f"style {sn}", bold, ls, lse
        sls = res.sa(sn, "android:letterSpacing")
        if sls:
            ls = float(sls)
    return 14.0, "default", bold, ls, lse


def wrap_lines(text: str, max_w: float, bold: bool, size: float,
               ls: float) -> int:
    """Жадный word-wrap: сколько строк нужно, чтобы уложить text в max_w."""
    if max_w <= 0:
        return 1
    total = w_at(text, bold, size, ls)
    if total <= max_w:
        return 1
    words = text.split(" ")
    lines, cur = 1, 0.0
    spw = w_at(" ", bold, size, ls)
    for wd in words:
        wdw = w_at(wd, bold, size, ls)
        need = wdw if cur == 0 else cur + spw + wdw
        if need <= max_w or cur == 0:
            cur = need
        else:
            lines += 1
            cur = wdw
    return lines


def _weight_budget(w: W, p: W, axis: str, res: AppRes) -> float | None:
    """Виджет 0dp + layout_weight внутри LinearLayout: доля родителя среди
    weight-соперников (не-weight фикс. соседи вычитаются из родителя)."""
    my_w = float(w.a.get("android:layout_weight") or 0)
    if my_w <= 0:
        return None
    size_attr = ("android:layout_height" if axis == "v"
                 else "android:layout_width")
    pw = fixed(p.a.get(size_attr), res)
    if not pw:
        return None
    total_w, fixed_use = 0.0, 0.0
    for c in p.ch:
        cw = float(c.a.get("android:layout_weight") or 0)
        if cw > 0:
            total_w += cw
        else:
            csize = fixed(c.a.get(size_attr), res)
            if csize:
                fixed_use += csize
    if total_w <= 0:
        return None
    pad = padv(p, res) if axis == "v" else padh(p, res)
    return max((pw - pad - fixed_use) * my_w / total_w, 0.0)


def budget(w: W, res: AppRes):
    """(avail_w, avail_h, kind_w, kind_h). None = «рамка растёт» → не
    проверяем по соответсв. оси."""
    raw_w = w.a.get("android:layout_width")
    raw_h = w.a.get("android:layout_height")
    fw = fixed(raw_w, res)
    fh = fixed(raw_h, res)
    a_w = max(fw - padh(w, res) - margh(w, res), 0.0) if fw else None
    k_w = "own" if a_w is not None else ""
    a_h = max(fh - padv(w, res), 0.0) if fh else None
    k_h = "own" if a_h is not None else ""
    w_weight = float(w.a.get("android:layout_weight") or 0)
    zero_w = (fw is not None and fw == 0)
    zero_h = (fh is not None and fh == 0)
    p = w.p
    while p is not None:
        pw = fixed(p.a.get("android:layout_width"), res)
        ph = fixed(p.a.get("android:layout_height"), res)
        if a_w is None:
            if zero_w and w_weight > 0 and p.tag in HLL \
                    and p.a.get("android:orientation") == "horizontal":
                bw = _weight_budget(w, p, "h", res)
                if bw is not None:
                    a_w = max(bw - padh(w, res) - margh(w, res), 0.0)
                    k_w = "weight"
            elif pw and p.tag in HLL \
                    and p.a.get("android:orientation") == "horizontal" \
                    and p is w.p:
                used = 0.0
                for c in p.ch:
                    if c is w or c.tag in TV_TAGS \
                            or float(c.a.get("android:layout_weight") or 0) > 0:
                        continue
                    cw = fixed(c.a.get("android:layout_width"), res)
                    if cw:
                        used += cw + margh(c, res)
                a_w = max(pw - padh(p, res) - used, 0.0)
                k_w = f"parent {a_w:.0f} [hLL]"
        if a_h is None:
            if zero_h and w_weight > 0 and p.tag in HLL \
                    and p.a.get("android:orientation") == "vertical":
                bh = _weight_budget(w, p, "v", res)
                if bh is not None:
                    a_h = max(bh - padv(w, res), 0.0)
                    k_h = "weight"
        if a_w is not None and a_h is not None:
            break
        p = p.p
    return a_w, a_h, k_w, k_h


def has_scroll_ancestor(w: W) -> bool:
    p = w.p
    while p is not None:
        if p.tag in ("ScrollView", "HorizontalScrollView", "NestedScrollView",
                     "androidx.recyclerview.widget.RecyclerView",
                     "android.support.v7.widget.RecyclerView"):
            return True
        if p.a.get("android:scrollbars") in ("vertical", "both"):
            return True
        p = p.p
    return False


def analyze_layout(layout: Path, res: AppRes):
    lines = layout.read_text(encoding="utf-8", errors="replace").splitlines()
    out = []
    for r in trees(lines):
        st = [r]
        idx = 0
        while idx < len(st):
            w = st[idx]
            idx += 1
            if w.tag in TV_TAGS:
                t = w.a.get("android:text", "")
                m = SREF.fullmatch(t)
                ru = res.rus.get(m.group(1)) if m else None
                if ru and m:
                    name = m.group(1)
                    size, prov, bold, ls, lse = metrics(w, res)
                    lh = size * line_ratio() + lse
                    a_w, a_h, k_w, k_h = budget(w, res)
                    one = w.a.get("android:maxLines")
                    ml = (int(one) if one is not None and one.isdigit()
                          else (1 if w.a.get("android:singleLine")
                                == "true" else None))
                    ell = w.a.get("android:ellipsize")
                    if ell == "none":
                        ell = None  # «none» = перенос, без обреза
                    # Аксиоматичные/технические рамки — не проверяем
                    if (a_w is not None and (a_w < 2 or (
                            a_h is not None and a_h < 2))):
                        st.extend(w.ch)
                        continue
                    # «Вертикальный» режим: ширина виджета растёт
                    # (wrap/match), но есть фикс. высота (sвоя — центр.
                    # виджет; предка — вертикальный стек/карточка).
                    vert_own_h = a_w is None and a_h is not None \
                        and k_h == "own"
                    vert = a_w is None
                    if a_w is None:
                        # ширина = ближайшая фикс. ширина предка, иначе экран
                        pw_find = None
                        p = w.p
                        while p is not None and pw_find is None:
                            pwv = fixed(p.a.get("android:layout_width"), res)
                            if pwv:
                                pw_find = max(pwv - padh(p, res), 0.0)
                            p = p.p
                        if pw_find is not None:
                            a_w = pw_find
                            k_w = "parentW"
                        else:
                            a_w = 1280.0
                            k_w = "screen-1280"
                    ru_r = unesc(ru)
                    zh_r = unesc(res.zhs.get(name, "") or "")
                    multi = "\n" in ru_r or "\n" in zh_r
                    ru_segs = [s.strip() for s in ru_r.split("\n")]
                    zh_segs = [s.strip() for s in zh_r.split("\n")]
                    ru_lines = sum(max(wrap_lines(s, a_w, bold, size, ls),
                                       1) for s in ru_segs if s)
                    zh_lines = sum(max(wrap_lines(s, a_w, bold, size, ls),
                                       1) for s in zh_segs if s) or 1
                    if ml == 1 or ell:
                        ru_lines = 1
                    fit1 = max((w_at(s, bold, size, ls) for s in ru_segs
                                if s), default=0.0)
                    zh_disp = (ru_r if (multi and not zh_r) else zh_r)
                    zh = zh_disp.replace("\n", " ")
                    h_fit = (a_h / lh) if (a_h is not None and lh > 0) \
                        else None
                    h_overflow = a_h is None and (
                        (ml is None and not ell and fit1 > 1.02 * a_w
                         and a_w < 1280 and k_w != "screen-1280"))
                    own_w = k_w == "own"
                    max_word = max((w_at(x, bold, size, ls)
                                    for x in ru.split(" ") if x),
                                   default=0.0)
                    one_ovf = ml is None and not ell and fit1 > 1.02 * a_w
                    v = None
                    if ml is not None or ell:
                        if fit1 > a_w or max_word > a_w or (
                                h_fit is not None and h_fit < 1.0
                                and a_w < 1280):
                            v, why = "CLIP", \
                                f"1-линия шире рамки (maxLines={ml}/ell={ell or '-'})"
                    scroll = has_scroll_ancestor(w)
                    real_box = a_w < 1280 and (
                        own_w or k_w.startswith("parent"))
                    if v is None and ml is None and not ell:
                        if real_box and max_word > a_w:
                            too = next(x for x in ru.split(" ")
                                       if w_at(x, bold, size, ls) > a_w)
                            note = (" в контейнере предка — класс "
                                    "симптома «…ольш»") \
                                if not own_w else ""
                            v, why = ("FAIL",
                                      "слово " + repr(too)
                                      + f" ({max_word:.0f}px) шире рамки "
                                      f"{a_w:.0f}px ({k_w}) — "
                                      f"вылезание/обрезка{note}")
                        elif (k_h == "own" and h_fit is not None
                              and a_h < 1280 and ru_lines > h_fit + 1e-9
                              and not (zh_lines > h_fit + 1e-9)):
                            v, why = ("FAIL",
                                      f"RU {ru_lines} стр. > своя фиксная "
                                      f"высота {a_h:.0f}px (влезает "
                                      f"{h_fit:.1f} стр.; ZH {zh_lines} влезал)")
                        elif (k_h == "own" and h_fit is not None
                              and ru_lines > h_fit + 1e-9
                              and zh_lines > h_fit + 1e-9):
                            v, why = ("WARN",
                                      f"RU {ru_lines} стр. > высота, но ZH "
                                      f"({zh_lines} стр.) тоже не влезал — "
                                      "рамка/перенос уже были")
                        elif (k_h == "parent" and h_fit is not None
                              and a_h < 2.5 * lh and not scroll
                              and ru_lines > h_fit + 1e-9
                              and zh_lines <= h_fit + 1e-9):
                            v, why = ("FAIL",
                                      f"RU {ru_lines} стр. > зона {a_h:.0f}px "
                                      f"(под 1 строку; ZH 1 влезал) — "
                                      "сдвиг/обрезка layout")
                        elif h_overflow and a_w < 1280 and not own_w:
                            v, why = ("WARN",
                                      f"1-линия {fit1:.0f}px > ширина {k_w} "
                                      f"{a_w:.0f}px (верхняя оценка)")
                    if v is None:
                        if (k_h in ("own", "parent") and h_fit is not None
                                and not scroll and ru_lines > zh_lines
                                and ru_lines <= h_fit + 1e-9):
                            v, why = ("WARN",
                                      f"RU {ru_lines} стр. при ZH {zh_lines} — "
                                      "текст/карточка растёт, но влезает")
                        elif fit1 > 0.92 * a_w and ru_lines <= 1 \
                                and a_w < 1280 and not multi:
                            v, why = "WARN", "впритык (>92% ширины)"
                    if v is None:
                        v, why = "ok", ""
                    out.append({
                        "v": v, "why": why, "vert": vert,
                        "app": res.app,
                        "layout": f"{layout.parent.name}/{layout.name}",
                        "line": w.line + 1, "id": w.wid or "",
                        "name": name, "zh": zh, "ru": ru,
                        "size": size, "prov": prov, "bold": bold,
                        "w_px": round(fit1, 1),
                        "aw": round(a_w, 1), "kw": k_w,
                        "ah": round(a_h, 1) if a_h is not None else None,
                        "kh": k_h, "lh": round(lh, 1),
                        "ml": ml, "ell": ell,
                        "vis": w.a.get("android:visibility"),
                        "ru_lines": ru_lines, "zh_lines": zh_lines,
                        "h_fit": None if h_fit is None else round(h_fit, 2),
                    })
            st.extend(w.ch)
    return out


def main():
    only = sys.argv[1] if len(sys.argv) > 1 else None
    results = []
    apps = sorted(p.name for p in DECOMPILED.iterdir() if p.is_dir())
    if only:
        apps = [a for a in apps if a == only]
    for app in apps:
        res = AppRes(app)
        if not res.rus:
            continue
        for layout in sorted(list((DECOMPILED / app / "res").glob("layout*/*.xml"))
                             + list((DECOMPILED / app / "res").glob("menu/*.xml"))):
            try:
                results.extend(analyze_layout(layout, res))
            except Exception as e:  # noqa: BLE001
                print(f"WARN {app}: {layout}: {e}", file=sys.stderr)

    fails = [h for h in results if h["v"] == "FAIL"]
    clips = [h for h in results if h["v"] == "CLIP"]
    warns = [h for h in results if h["v"] == "WARN"]

    LOGS.mkdir(exist_ok=True)
    (LOGS / "fitboxes_report.json").write_text(
        json.dumps(results, ensure_ascii=False, indent=1), encoding="utf-8")

    def fmt(h, extra=""):
        return (f"- **{h['app']}** `{h['name']}` — «{h['ru']}» [{h['zh']}] "
                f"@{h['size']}{'.bold' if h['bold'] else ''} ({h['prov']}): "
                f"1-линия {h['w_px']:.0f}px, строк RU={h['ru_lines']} "
                f"(ZH {h['zh_lines']}), рамка W={h['aw']:.0f} ({h['kw']}) "
                + (f"H={h['ah']:.0f} ({h['kh']}, строка≈{h['lh']:.0f}px, "
                   f"влезает {h['h_fit']:.1f})" if h["ah"] is not None else "H=∞")
                + extra + f" — {h['layout']}:{h['line']} id={h['id']}")

    L = []
    L.append("# Аудит: RU-перевод в фиксированных рамках layout'ов (все приложения)")
    L.append("")
    L.append(f"Проверено: {len(results)} строк в фикс. рамках -> "
             f"**FAIL: {len(fails)}**, WARN: {len(warns)}, "
             f"CLIP (1-линейная обрезка по дизайну): {len(clips)}")
    L.append("")
    L.append("FAIL = RU переносится на больше строк, чем влезает в высоту "
             "рамки (вертикальная/горизонтальная обрезка — симптом «…ольш»), "
             "ИЛИ 1-линейный виджет шире рамки без ellipsize.")
    L.append("WARN = влезает, но строка RU занимает больше строк, чем ZH-"
             "оригинал (рамка растёт/layout сдвигается), либо RU впритык "
             "(>92% ширины).")
    L.append("CLIP = сужение по дизайну (maxLines=1/ellipsize): строка "
             "сужается с «…» — сверяем, осознанно ли.")
    L.append("Метод: Roboto ГУ (regular/bold), условный px (dp==sp), "
             "size из attr/@dimen/style собственн. values (внешниие lib "
             "стили -> 14sp default, может занижать); перенос — жадный "
             "word-wrap; высота строки = (asc+desc)+lineSpacingExtra. "
             "Рамка = собственнаи dp-размер либо ближайший фикс. предок "
             "(верхняя граница; 0dp-constraint неизвестно). Не проверяем: "
             "строки с \\n, %s-аргументы (нижняя оценка), custom-виджета "
             "(наследники TextView/Button/EditText).")
    L.append("")
    L.append(f"## FAIL ({len(fails)}) — вероятна обрезка на ГУ")
    L.append("")
    for h in sorted(fails, key=lambda x: (x["app"], -x["ru_lines"])):
        L.append(fmt(h))
    L.append("")
    L.append(f"## WARN ({len(warns)}) — влезает, но растянулось/впритык")
    L.append("")
    for h in sorted(warns, key=lambda x: (x["app"], -x["ru_lines"])):
        L.append(fmt(h, " [впритык]" if h["w_px"] > 0.92 * h["aw"] and h["ru_lines"] == 1 else ""))
    L.append("")
    L.append(f"## CLIP ({len(clips)}) — maxLines=1/ellipsize, RU шире")
    L.append("")
    for h in sorted(clips, key=lambda x: (x["app"], -x["w_px"])):
        L.append(fmt(h, f" [ml={h['ml']} ell={h['ell'] or '-'}]"))
    (LOGS / "fitboxes_report.txt").write_text("\n".join(L) + "\n",
                                              encoding="utf-8")
    print(f"checked={len(results)} FAIL={len(fails)} WARN={len(warns)} "
          f"CLIP={len(clips)}")
    print("report:", LOGS / "fitboxes_report.txt")


if __name__ == "__main__":
    main()
