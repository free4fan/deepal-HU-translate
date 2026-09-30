#!/usr/bin/env python3
"""
Создание/обновление SQLite базы переводов из JSON-файлов.

База: database/translations.sqlite3
Таблица: translations(app_name, name, zh, ru)

Примеры:
  python3 create_db.py                  создать/обновить базу + статистика
  python3 create_db.py --stats          только статистика
  python3 create_db.py --find 蓝牙      поиск по совпадению в zh
  python3 create_db.py --find-ru парковка  поиск по совпадению в ru
"""
import sys, os, json, argparse
from pathlib import Path
from sqlite3 import connect

SCRIPT_DIR = Path(__file__).resolve().parent
TRANS_DIR = SCRIPT_DIR / "translations"
DB_PATH = SCRIPT_DIR / "database" / "translations.sqlite3"


def collect_entries():
    all_apps = sorted([
        f.stem for f in TRANS_DIR.glob("*.json")
        if not f.name.endswith("_progress.json")
    ])
    rows = []
    for app in all_apps:
        json_path = TRANS_DIR / f"{app}.json"
        try:
            data = json.loads(json_path.read_text(encoding="utf-8"))
        except Exception as e:
            print(f"  [WARN] {app}.json: {e}", file=sys.stderr)
            continue
        if not isinstance(data, list):
            continue
        for entry in data:
            if not isinstance(entry, dict):
                continue
            name = entry.get("name", "")
            zh = (entry.get("zh") or "").strip()
            ru = (entry.get("ru") or "").strip()
            if zh:
                rows.append((app, name, zh, ru))
    return rows


def build_db(rows):
    DB_PATH.parent.mkdir(parents=True, exist_ok=True)
    conn = connect(DB_PATH)
    # Один атомарный транзакционный блок: DROP+CREATE+INSERT.
    # UNIQUE(app_name,name) против дублей; INSERT OR REPLACE — последний выигрывает.
    try:
        conn.execute("BEGIN")
        conn.execute("DROP TABLE IF EXISTS translations")
        conn.execute("""
            CREATE TABLE translations (
                app_name TEXT NOT NULL,
                name     TEXT NOT NULL,
                zh       TEXT NOT NULL,
                ru       TEXT DEFAULT '',
                UNIQUE(app_name, name)
            )
        """)
        conn.executemany(
            "INSERT OR REPLACE INTO translations VALUES (?,?,?,?)", rows
        )
        conn.commit()
    except Exception as e:
        conn.rollback()
        conn.close()
        raise SystemExit(f"❌ Ошибка сборки базы: {e}")
    conn.close()
    print(f"✅ Записано {len(rows)} строк в {DB_PATH}")


def show_stats():
    if not DB_PATH.exists():
        print("❌ База не существует. Запустите: python3 create_db.py")
        return
    conn = connect(DB_PATH)
    cur = conn.cursor()

    print("\n" + "=" * 60)
    print("  Статистика базы переводов")
    print("=" * 60)

    total = cur.execute("SELECT COUNT(*) FROM translations").fetchone()[0]
    with_ru = cur.execute(
        "SELECT COUNT(*) FROM translations WHERE ru != ''"
    ).fetchone()[0]
    pct = f"{with_ru/total*100:.1f}%" if total else "N/A"

    print(f"\n  Всего записей:     {total}")
    print(f"  С русским:         {with_ru} ({pct})")
    print(f"  Без перевода:      {total - with_ru}")
    print(f"  Приложений:        {cur.execute('SELECT COUNT(DISTINCT app_name) FROM translations').fetchone()[0]}")
    print(f"  Уникальных zh:     {cur.execute('SELECT COUNT(DISTINCT zh) FROM translations').fetchone()[0]}")

    print(f"\n  Топ-20 приложений:")
    print(f"  {'Приложение':<35} {'Всего':>6} {'С ru':>6} {'%':>6}")
    print(f"  {'-'*35} {'-'*6} {'-'*6}")
    for (app, total_app, trans_app) in cur.execute("""
        SELECT app_name, COUNT(*) AS total,
               SUM(CASE WHEN ru != '' THEN 1 ELSE 0 END)
        FROM translations GROUP BY app_name
        ORDER BY total DESC
    """).fetchall():
        rate = trans_app / total_app * 100 if total_app else 0
        print(f"  {app:<35} {total_app:>6} {trans_app:>6} {rate:>5.0f}%")

    print(f"\n  Не переведённые (топ-15):")
    print(f"  {'Приложение':<35} {'Без ru':>8}")
    print(f"  {'-'*35} {'-'*8}")
    for (app, cnt) in cur.execute("""
        SELECT app_name, COUNT(*) AS cnt FROM translations
        WHERE ru = '' GROUP BY app_name ORDER BY cnt DESC LIMIT 15
    """).fetchall():
        print(f"  {app:<35} {cnt:>8}")

    conn.close()


def search_db(query, column="zh"):
    if not DB_PATH.exists():
        print("❌ База не существует. Запустите: python3 create_db.py")
        return
    if column == "zh":
        cond = "zh LIKE ?"
        params = (f"%{query}%",)
    elif column == "ru":
        cond = "ru LIKE ?"
        params = (f"%{query}%",)
    else:
        cond = "zh LIKE ? OR ru LIKE ?"
        params = (f"%{query}%", f"%{query}%")

    conn = connect(DB_PATH)
    cur = conn.cursor()
    rows = cur.execute(f"""
        SELECT app_name, name, zh, ru,
               CASE WHEN ru != '' THEN '✅' ELSE '⏳' END
        FROM translations WHERE {cond}
        ORDER BY app_name, name
    """, params).fetchall()

    hit_col = "Zh" if column == "zh" else ("Ru" if column == "ru" else "Zh/Ru")
    print(f"  {'Приложение':<30} {'Zh':<25} {'Status':<8} {'Ru'}")
    print(f"  {'-'*65}")
    if rows:
        for app, name, zh, ru, status in rows[:100]:
            zh_d = zh[:23]
            ru_d = ru[:30] if ru else ""
            print(f"  {app:<30} {zh_d:<25} {status:<8} {ru_d}")
        if len(rows) > 100:
            print(f"  ... ещё {len(rows) - 100}")
    else:
        print(f"  Найдено 0 по {hit_col}: {query}")

    conn.close()


def main():
    parser = argparse.ArgumentParser(
        description="Создание/обновление SQLite базы переводов.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""Примеры:
  python3 create_db.py              создать базу + показать статистику
  python3 create_db.py --stats      только статистика
  python3 create_db.py --find 蓝牙  поиск по CJK в zh
  python3 create_db.py --find-ru парковка  поиск по переводу в ru
""",
    )
    parser.add_argument("-s", "--stats", action="store_true",
                        help="Показать статистику существующей базы")
    parser.add_argument("-f", "--find", type=str, metavar="TEXT",
                        help="Поиск по CJK (поле zh)")
    parser.add_argument("-r", "--find-ru", type=str, metavar="TEXT",
                        help="Поиск по переводу (поле ru)")
    args = parser.parse_args()

    # default: build + stats
    if not sys.argv[1:]:
        rows = collect_entries()
        if not rows:
            print("❌ Нет строк для базы")
            return
        build_db(rows)
        show_stats()
        return

    if args.stats:
        show_stats()
        return

    if args.find:
        search_db(args.find, "zh")
        return

    if args.find_ru:
        search_db(args.find_ru, "ru")
        return

    parser.print_help()


if __name__ == "__main__":
    main()
