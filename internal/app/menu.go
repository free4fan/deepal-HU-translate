package app

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"deepal-hu-translate/internal/groups"
)

// Menu — интерактивный цикл (батник :MENU_SCREEN/:GROUP_MENU).
// Показывается ВСЕГДА, даже без adb (быстрая проверка в баннере).
type Menu struct {
	a      *App
	out    io.Writer
	sc     *bufio.Scanner
	scheme string
	// Clear — очистка экрана (cls). nil = не очищать (тесты, pipe).
	Clear func(io.Writer)
}

// NewMenu — менеджер dynamic или static. in == nil -> os.Stdin.
func NewMenu(a *App, scheme groups.Scheme, out io.Writer, in io.Reader) *Menu {
	m := &Menu{a: a, out: out, scheme: string(scheme)}
	if in == nil {
		in = os.Stdin
	}
	m.sc = bufio.NewScanner(in)
	return m
}

// Run — цикл до выбора «Выход».
func (m *Menu) Run() {
	for {
		choice := m.actionMenu()
		var mode Mode
		switch choice {
		case "0":
			return
		case "R":
			fmt.Fprintln(m.out)
			m.a.Adb.Check(m.a.Ctx)
			fmt.Fprintln(m.out)
			continue
		case "1", "2", "3", "4", "5", "6", "7":
			mode = m.modeFor(choice)
		default:
			fmt.Fprintln(m.out, "Неверный выбор.")
			time.Sleep(time.Second)
			continue
		}
		// status/diag/report не требуют цели
		if mode == ModeStatus || mode == ModeDiag || mode == ModeReport {
			if _, err := m.a.Run(mode, ""); err != nil {
				fmt.Fprintln(m.out)
				fmt.Fprintln(m.out, err)
				continue // без ADB/с ошибкой — обратно в меню (без Y/N)
			}
			if !m.askAgain() {
				return
			}
			continue
		}
		preset := m.groupMenu(string(mode))
		// "0" / "" — «Назад к действию»: никакого прогона и никакого
		// ADB-check (аналог bat if "%PRESET%"=="0" goto MENU).
		if preset == "" || preset == "0" {
			continue
		}
		if _, err := m.a.Run(mode, preset); err != nil {
			fmt.Fprintln(m.out)
			fmt.Fprintln(m.out, err)
			continue // без ADB/пустая цель — обратно в меню (без Y/N)
		}
		if !m.askAgain() {
			return
		}
	}
}

// askAgain — «Вернуться в меню? [Y/N]».
func (m *Menu) askAgain() bool {
	fmt.Fprintln(m.out)
	m.print("Вернуться в меню? [Y/N]: ")
	return m.readLine() == "Y"
}

func (m *Menu) readLine() string {
	if !m.sc.Scan() {
		return "0" // EOF -> выход
	}
	return strings.ToUpper(strings.TrimSpace(m.sc.Text()))
}

// modeFor — номер пункта меню -> Mode (нумерация динамической схемы: 1-7).
// Для static: 1-5 (нет enable/disable — раздельные install/uninstall).
func (m *Menu) modeFor(num string) Mode {
	dyn := map[string]Mode{
		"1": ModeInstall, "2": ModeEnable, "3": ModeDisable,
		"4": ModeUninstall, "5": ModeStatus, "6": ModeDiag, "7": ModeReport,
	}
	stat := map[string]Mode{
		"1": ModeInstall, "2": ModeUninstall, "3": ModeStatus, "4": ModeDiag, "5": ModeReport,
	}
	if m.scheme == "static" {
		if md, ok := stat[num]; ok {
			return md
		}
		return ModeStatus
	}
	if md, ok := dyn[num]; ok {
		return md
	}
	return ModeStatus
}

func (m *Menu) print(s string) { fmt.Fprint(m.out, s) }

// adbBanner — быстрая проверка (одна `adb devices`), ничего не блокирует.
func (m *Menu) adbBanner() string {
	if m.a.Adb.CheckFast(m.a.Ctx) {
		return " [OK] ADB: устройство подключено"
	}
	return " [!!] ADB: устройство не найдено. Подключите USB и разрешите отладку (кнопка «АВТОРИЗОВАТЬ» на HU). Повторная проверка - [R]"
}

func (m *Menu) actionMenu() string {
	cfg := m.a.Cfg
	m.clear()
	fmt.Fprintln(m.out, strings.Repeat("=", 60))
	title := "Deepal HU Translate - RRO Manager (динамические)"
	if m.scheme == "static" {
		title = "Deepal HU Translate - RRO Manager (статические)"
	}
	fmt.Fprintln(m.out, "  "+title)
	fmt.Fprintln(m.out, strings.Repeat("=", 60))
	fmt.Fprintln(m.out)
	fmt.Fprintf(m.out, "  ADB:%s\n", m.adbBanner())
	fmt.Fprintln(m.out)

	if m.scheme == "static" {
		fmt.Fprintln(m.out, "  Действие:")
		fmt.Fprintf(m.out, "    [1] Установить   (adb push в %s + reboot)\n", cfg.OverlayBase)
		fmt.Fprintf(m.out, "    [2] Удалить      (adb rm -rf из %s + reboot)\n", cfg.OverlayBase)
		fmt.Fprintln(m.out, "    [3] Статус       (overlay list: все / целевые)")
		fmt.Fprintln(m.out, "    [4] Диагностика  (locale, целевые оверлеи, dump, dumpsys)")
		fmt.Fprintln(m.out, "    [5] Собрать логи  (report: zip для анализа)")
		fmt.Fprintln(m.out, "    [R] Проверить ADB заново  (полная проверка + рестарт демона)")
		fmt.Fprintln(m.out, "    [0] Выход")
	} else {
		fmt.Fprintln(m.out, "  Действие:")
		fmt.Fprintln(m.out, "    [1] Установить + включить   (install + enable)")
		fmt.Fprintln(m.out, "    [2] Включить                (enable)")
		fmt.Fprintln(m.out, "    [3] Отключить               (disable)")
		fmt.Fprintln(m.out, "    [4] Удалить                 (disable + adb uninstall)")
		fmt.Fprintln(m.out, "    [5] Статус                  (overlay list: все / целевые)")
		fmt.Fprintln(m.out, "    [6] Диагностика             (locale, целевые оверлеи, dump, dumpsys)")
		fmt.Fprintln(m.out, "    [7] Собрать логи            (report: zip для анализа)")
		fmt.Fprintln(m.out, "    [R] Проверить ADB заново    (полная проверка + рестарт демона)")
		fmt.Fprintln(m.out, "    [0] Выход")
	}
	fmt.Fprintln(m.out)
	m.print("Выбор: ")
	choice := m.readLine()
	if choice == "" {
		return m.actionMenu()
	}
	return choice
}

// groupMenu — меню выбора пресета; "0"/"" = назад к действию.
// В интерактивном меню принимаются ТОЛЬКО пункты меню (свободное имя пакета
// — только в CLI-режиме: deepl dyn enable Camera). Несуществующий выбор
// (напр. "10") — не цель: сообщение и повтор меню, БЕЗ ADB-check.
func (m *Menu) groupMenu(mode string) string {
	cfg := m.a.Cfg
	valid := map[string]bool{
		"0": true, "1": true, "2": true, "3": true, "4": true,
		"8": true, "A": true, "C": true, "AUTO": true,
	}
	for {
		m.clear()
		fmt.Fprintln(m.out, strings.Repeat("=", 60))
		fmt.Fprintf(m.out, "  Режим: %s\n", mode)
		fmt.Fprintln(m.out, strings.Repeat("=", 60))
		fmt.Fprintln(m.out, "  Цель:")
		fmt.Fprintln(m.out, "    [1]  CRITICAL       (системные сервисы)")
		fmt.Fprintln(m.out, "    [2]  CORE UI        (основной интерфейс)")
		fmt.Fprintln(m.out, "    [3]  IMPORTANT APPS")
		fmt.Fprintln(m.out, "    [4]  SECONDARY      (второстепенные)")
		fmt.Fprintln(m.out, "    [8]  TOP-8          (критичные 8)")
		fmt.Fprintln(m.out, "    [A]  Все группы")
		fmt.Fprintln(m.out, "    [C]  CORE + IMPORTANT (2+3, без системных)")
		fmt.Fprintf(m.out, "    [auto] Все *_RRO.apk из %s\n", cfg.ApkDir)
		fmt.Fprintln(m.out, "    [L]  Пакеты         (список, выбор по номеру/номерам)")
		fmt.Fprintln(m.out, "    [0]  Назад к действию")
		fmt.Fprintln(m.out)
		m.print("Выбор: ")
		raw := strings.TrimSpace(m.readLine())
		if raw == "" {
			return "0"
		}
		up := strings.ToUpper(raw)
		if up == "L" {
			return m.packageList(mode)
		}
		if valid[up] {
			return up
		}
		fmt.Fprintln(m.out, "Неверный выбор.")
		time.Sleep(time.Second)
	}
}

// packageList — пронумерованный список всех пакетов; выбор по номерам:
// "5" — один, "5,12 18" — несколько (разделители: запятая, пробел, точка с
// запятой). "0" — назад к выбору цели. Возвращает имя(на) через пробел или "0".
func (m *Menu) packageList(mode string) string {
	cfg := m.a.Cfg
	names := cfg.AllCandidates()
	if len(names) == 0 {
		fmt.Fprintf(m.out, "  В списке нет пакетов (пустые группы и нет *_RRO.apk в %s).\n", cfg.ApkDir)
		return "0"
	}
	numW := len(strconv.Itoa(len(names)))
	nameW := 0
	for _, n := range names {
		if len(n) > nameW {
			nameW = len(n)
		}
	}
	// ширина ячейки = 2 пробела + номер + 2 пробела + ширина имени (по maxlen)
	colW := 2 + numW + 2 + nameW
	cols := 1
	if colW*2 <= 78 {
		cols = 2
	}
	for {
		m.clear()
		fmt.Fprintln(m.out, strings.Repeat("=", 60))
		fmt.Fprintf(m.out, "  Режим: %s  |  Пакеты: %d\n", mode, len(names))
		fmt.Fprintln(m.out, strings.Repeat("=", 60))
		fmt.Fprintln(m.out)
		i := 0
		for i < len(names) {
			line := ""
			for c := 0; c < cols && i < len(names); c++ {
				cell := fmt.Sprintf("  %*d  %-*s", numW, i+1, nameW, names[i])
				if c < cols-1 {
					if dl := colW - len(cell); dl > 0 {
						cell += strings.Repeat(" ", dl)
					}
				}
				line += cell
				i++
			}
			fmt.Fprintln(m.out, strings.TrimRight(line, " "))
		}
		fmt.Fprintln(m.out)
		fmt.Fprintln(m.out, "  Номера через запятую/пробел: напр. 5 или 5,12 18;  0 - назад")
		m.print("Выбор: ")
		raw := strings.TrimSpace(m.readLine())
		if raw == "" {
			continue
		}
		if strings.ToUpper(raw) == "0" {
			return "0"
		}
		picked, ok := m.parsePicks(raw, names)
		if !ok {
			fmt.Fprintln(m.out, "  Неверный номер или пустой выбор.")
			continue
		}
		return strings.Join(picked, " ")
	}
}

// parsePicks — строка номеров "5", "5,12" → индексы+имена (порядок списка,
// без дублей). ok=false — не распознан ни один корректный номер (1..len).
func (m *Menu) parsePicks(raw string, names []string) ([]string, bool) {
	raw = strings.ReplaceAll(raw, ",", " ")
	raw = strings.ReplaceAll(raw, ";", " ")
	seen := map[int]bool{}
	var out []string
	for _, tok := range strings.Fields(raw) {
		idx, err := strconv.Atoi(tok)
		if err != nil || idx < 1 || idx > len(names) {
			continue
		}
		if seen[idx] {
			continue
		}
		seen[idx] = true
		out = append(out, names[idx-1])
	}
	return out, len(out) > 0
}

// clear — cls (как в bat). По умолчанию нативная очистка консоли
// (ClearScreen: Windows cmd /c cls, Unix clear, только TTY).
// Полем Clear подменяется в тестах.
func (m *Menu) clear() {
	if m.Clear != nil {
		m.Clear(m.out)
		return
	}
	ClearScreen(m.out)
}
