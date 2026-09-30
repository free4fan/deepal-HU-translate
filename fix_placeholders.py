#!/usr/bin/env python3
"""Одноразовая правка данных: плейсхолдеры + CJK-in-ru в translations/*.json.

Правит ТОЛЬКО поле ru по точным парам (app, name). WT_VehicleCenter/
Exterior_voice_interaction_title НЕ трогается: там CJK «***你好» — это
голосовой wake-word, а не текст для перевода.
"""
import json
from pathlib import Path

TRANS = Path(__file__).resolve().parent / "translations"

# (app, name) -> новое значение ru
FIXES = {
    # `` `s `` вместо `` "%s ``; литеральные кавычки, как в zh, сохранены
    ("WT_AppStore", "srl_component_falsify"):
        '"%s фантомная область\nпредставляет высоту перетаскивания во время выполнения【%.1fdp】\nне отображая при этом ничего"',
    ("WT_ElectronicDirections", "srl_component_falsify"):
        '"%s фантомная область\nпредставляет высоту перетаскивания во время выполнения【%.1fdp】\nне отображая при этом ничего"',
    ("WT_MultiMediaCenter", "srl_component_falsify"):
        '"%s фантомная область\nпредставляет высоту перетаскивания во время выполнения【%.1fdp】\nне отображая при этом ничего"',
    # %s проглочен («Минут» = явно не тот смысл; 分 = балл/очко)
    ("WT_AutoMaintenance", "order_shop_score_text"): "%s баллов",
    ("WT_FusionNavigation", "road_book_main_list_item_detail_avgScore"): "%s баллов",
    # %S -> %s, добавлена единица «с» (секунды)
    ("WT_FusionNavigation", "continue_journey_countdown"): "Продолжить маршрут %s с",
    ("WT_FusionNavigation", "team_countdown_i_see"): "Понятно %s с",
    # 第%s个 = «%s-й» (порядковый); было «%-й»
    ("WT_FusionNavigation", "open_num_position"): "%s-й",
    # 3 %s: направление, характер затора, время
    ("WT_TSpeech", "agent_route_cond_info"):
        "На пути к %s образовалась %s пробка, поездка займёт %s.",
    # 3 %s: значение+единица, время; CJK «，预计需要» переведён
    ("WT_TSpeech", "agent_ahead_cond_info"):
        "Впереди %s%s, время в пути %s.",
    # 5 именованных %1..5$s; CJK «,预计» переведён
    ("WT_TSpeech", "m2011"):
        "Выезжаем сейчас, до %1$s, весь путь %2$s, в пути %3$s, прибудем в %4$s, дорожная обстановка %5$s",
}


def main():
    by_app = {}
    for (app, name), new_ru in FIXES.items():
        by_app.setdefault(app, {})[name] = new_ru

    for app, names in by_app.items():
        fp = TRANS / f"{app}.json"
        data = json.loads(fp.read_text(encoding="utf-8"))
        idx = {e.get("name"): e for e in data if isinstance(e, dict)}
        for name, new_ru in names.items():
            e = idx.get(name)
            if e is None:
                print(f"  !! {app}/{name}:Entry not found")
                continue
            old = e.get("ru")
            e["ru"] = new_ru
            print(f"{app}/{name}:")
            print(f"  old: {old!r}")
            print(f"  new: {new_ru!r}")
        fp.write_text(
            json.dumps(data, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
        )


if __name__ == "__main__":
    main()
