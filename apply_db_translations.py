#!/usr/bin/env python3
"""
Применить переводы из SQLite-базы к JSON-файлам.

Скрипт ищет в базе переводы для строк из JSON и вставляет их в поле ru.

Использование:
  python3 apply_db_translations.py              - для всех приложений
  python3 apply_db_translations.py --app WT_BTPhone - для конкретного
  python3 apply_db_translations.py --all --dry-run - только показать что будет

JSON-формат:
  [{"name": "...", "zh": "中文", "ru": ""}]
"""
import sys, os, json, re
from pathlib import Path
from sqlite3 import connect

SCRIPT_DIR = Path(__file__).resolve().parent
TRANS_DIR = SCRIPT_DIR / "translations"
DB_PATH = SCRIPT_DIR / "database" / "translations.sqlite3"

# Форматтеры (как в validate.py / improve_translations.py): паритет между zh и ru.
FORMATTER_RE = re.compile(r"%(\d+\$)?(\.?\d*)([dfs])|%%")
CJK_WHITELIST = {("WT_VehicleCenter", "Exterior_voice_interaction_title")}


def _specs(s: str) -> list[str]:
    return [m.group(0) for m in FORMATTER_RE.finditer(s or "")]


def _has_cjk(t) -> bool:
    return isinstance(t, str) and any(0x4E00 <= ord(c) <= 0x9FFF for c in t)


def _save_json_atomic(json_path: Path, data) -> None:
    """Атомарная запись (tmp + os.replace) — как в improve_translations.save_app."""
    tmp = json_path.with_suffix(".json.tmp")
    with open(tmp, "w", encoding="utf-8") as f:
        json.dump(data, f, ensure_ascii=False, indent=2)
        f.write("\n")
    os.replace(tmp, json_path)


def connect_db():
    if not DB_PATH.exists():
        print(f"❌ База не найдена: {DB_PATH}")
        print("   Запустите: python3 create_db.py")
        return None
    DB_PATH.parent.mkdir(parents=True, exist_ok=True)
    return connect(DB_PATH)


def _candidate_ok(zh: str, ru: str, app_name: str, name: str) -> bool:
    """Guard: кандидат пригоден только если ru непустой, без CJK (кроме whitelist),
    и форматтеры совпадают с zh. Иначе запись в ru ломает строку на ГУ."""
    ru = (ru or "").strip()
    if not ru:
        return False
    if _has_cjk(ru) and (app_name, name) not in CJK_WHITELIST:
        return False
    if _specs(zh or "") != _specs(ru):
        return False
    return True


def _resolve_translation(conn, app_name: str, name: str, zh: str):
    """Найти кандидат перевода. Возврат: (ru|None, source, note)
      source: 'exact' | 'app-zh' | 'cross-app' | None
      note:   'ambiguous' | 'guard:...' | None

    Стратегия (от строгого к широкому):
      1) (app_name, name) — точный ключ;
      2) (app_name, zh)   — другое name в том же приложении;
      3) (zh)             — кросс-апп, только если всех кандидатов
         (с учётом guard) ровно ОДИН уникальный перевод; иначе 'ambiguous'.
    """
    row = conn.execute(
        "SELECT ru FROM translations WHERE app_name=? AND name=? AND ru != '' LIMIT 1",
        (app_name, name),
    ).fetchone()
    if row and (row[0] or "").strip():
        return (row[0] or "").strip(), "exact", None

    row = conn.execute(
        "SELECT ru FROM translations WHERE app_name=? AND zh=? AND ru != '' "
        "AND name != ? LIMIT 1",
        (app_name, zh, name),
    ).fetchone()
    if row and (row[0] or "").strip():
        return (row[0] or "").strip(), "app-zh", None

    rows = conn.execute(
        "SELECT ru FROM translations WHERE zh=?", (zh,)
    ).fetchall()
    uniq = {
        (r[0] or "").strip() for r in rows
        if (r[0] or "").strip()
        and _candidate_ok(zh, (r[0] or "").strip(), app_name, name)
    }
    if len(uniq) == 1:
        return uniq.pop(), "cross-app", None
    if len(uniq) > 1:
        return None, None, "ambiguous"
    # есть строки, но все провалили guard
    if rows:
        return None, None, "guard:rejected"
    return None, None, None


def apply_translations(conn, json_path, dry_run=False):
    """Применить переводы из базы к одному JSON-файлу.

    Матчинг (см. _resolve_translation): точный (app,name) -> (app,zh) ->
    кросс-апп по zh, но только ОДНОЗНАЧНО (уникальный кандидат).
    Заполняются только ПУСТЫЕ ru; guard отклоняет CJK/битые форматтеры.
    """
    app_name = json_path.stem

    with open(json_path, encoding="utf-8") as f:
        data = json.load(f)

    if not isinstance(data, list):
        return 0, 0, "bad_json"

    applied = 0
    skipped = 0
    ambiguous = 0
    errors = 0

    for i, entry in enumerate(data):
        name = entry.get("name", "")
        zh = (entry.get("zh") or "").strip()
        ru = (entry.get("ru") or "").strip()

        # Пропускаем строки без zh/name или уже с переводом
        if not zh or not name or ru:
            continue

        translation, source, note = _resolve_translation(conn, app_name, name, zh)

        if translation:
            if dry_run:
                print(f"  📝 {name:<30} {zh[:24]:<24} → {translation}  [{source}]")
            else:
                entry["ru"] = translation
                applied += 1
        else:
            if note == "ambiguous":
                ambiguous += 1
                if dry_run:
                    print(f"  ⚠️ {name:<30} → (несколько разных переводов по zh, не применили)")
            elif note == "guard:rejected":
                skipped += 1
                if dry_run:
                    print(f"  🚫 {name:<30} → (кандидаты отклонены guard'ом)")
            else:
                skipped += 1
                if dry_run:
                    print(f"  ⏳ {name:<30} → (нет перевода)")

    # Сохраняем JSON атомарно
    if applied > 0 and not dry_run:
        _save_json_atomic(json_path, data)
        print(f"  ✅ Применено {applied} переводов")

    return applied, skipped + ambiguous, errors


def main():
    import argparse

    parser = argparse.ArgumentParser(
        description="Применить переводы из SQLite-базы к JSON-файлам.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""Примеры:
  python3 apply_db_translations.py --all              - для всех приложений
  python3 apply_db_translations.py --all --dry-run    - только показать
  python3 apply_db_translations.py --app WT_BTPhone   - для одного
""",
    )
    parser.add_argument("--all", action="store_true", help="Все приложения")
    parser.add_argument("--app", type=str, help="Конкретное приложение (без .json)")
    parser.add_argument("--dry-run", action="store_true", help="Только показать")
    parser.add_argument("--stats", action="store_true", help="Статистика базы")
    args = parser.parse_args()

    if not sys.argv[1:]:
        print("❌ Укажите: --all, --app NAME, --dry-run, или --stats")
        sys.exit(1)

    if args.stats:
        conn = connect_db()
        if not conn:
            return
        cur = conn.cursor()
        total = cur.execute("SELECT COUNT(*) FROM translations").fetchone()[0]
        with_ru = cur.execute(
            "SELECT COUNT(*) FROM translations WHERE ru != ''"
        ).fetchone()[0]
        unique_zh = cur.execute(
            "SELECT COUNT(DISTINCT zh) FROM translations WHERE ru != ''"
        ).fetchone()[0]
        print(f"Всего записей:    {total}")
        pct = f"{with_ru/total*100:.1f}%" if total else "N/A"
        print(f"С переводом:     {with_ru} ({pct})")
        print(f"Уникальных zh:   {unique_zh}")
        conn.close()
        return

    conn = connect_db()
    if not conn:
        return

    if args.app:
        apps = [args.app]
    elif args.all:
        apps = [
            f.stem for f in sorted(TRANS_DIR.glob("*.json"))
            if not f.name.endswith("_progress.json")
            and f.name not in ("index.json", "extraction.json", "ambiguity_db.json", "exclude.json")
        ]
    else:
        print("❌ Укажите --all или --app NAME")
        conn.close()
        sys.exit(1)

    total_applied = 0
    total_skipped = 0

    print(f"📦 Приложений: {len(apps)}")
    print(f"   Режим: {'dry-run' if args.dry_run else 'apply'}")
    print(f"{'='*60}")

    for app_name in apps:
        json_path = TRANS_DIR / f"{app_name}.json"
        if not json_path.exists():
            continue

        applied, skipped, error = apply_translations(conn, json_path, args.dry_run)
        total_applied += applied
        total_skipped += skipped

        if not args.dry_run and applied > 0:
            print(f"  [{app_name}] → {applied}/{applied + skipped} применено")

    print(f"\n{'='*60}")
    print(f"ИТОГО: применено {total_applied}, пропущено {total_skipped}")

    conn.close()


if __name__ == "__main__":
    main()
