// Package groups — справочник целей (группы 1-4, TOP8, EXCLUDE9),
// пресеты и пути. ЕДИНСТВЕННЫЙ источник списков — файл targets.txt
// (см. targets.go): bat-менеджеры читают его напрямую, Go — через
// Load() (env DEEPL_TARGETS / ./targets.txt в CWD). Списков В КОДЕ И В
// БИНАРНИКЕ НЕТ (ни go:embed, ни встроенной копии) — для другой прошивки
// правится только targets.txt.
//
// ВАЖНОЕ РАЗЛИЧИЕ: GROUP1 статической схемы = GROUP1 динамической +
// 9 AOSP-target (NetworkStack/Providers/…), т.к. в /vendor/overlay они
// ставятся, тогда как через adb install — нет (см. manage.bat REM-блок).
package groups

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Scheme — тип RRO-схемы.
type Scheme string

const (
	SchemeDynamic Scheme = "dynamic" // com.android.vendor.translate.rro.*
	SchemeStatic  Scheme = "static"  // com.deepal.translate.rro.*
)

// Пресеты (меню и CLI).
const (
	Preset0    = "0" // меню/назад
	Preset1    = "1"
	Preset2    = "2"
	Preset3    = "3"
	Preset4    = "4"
	Preset8    = "8"
	PresetA    = "A" // все группы
	PresetC    = "C" // 2+3
	PresetAuto = "auto"
)

// Config — полная конфигурация схемы (все, что переопределялось в bat).
type Config struct {
	Scheme      Scheme
	Prefix      string
	ApkDir      string
	Filter      string
	OverlayBase string
	LogsDir     string

	Groups  [4][]string
	Top8    []string
	Exclude []string
}

// Dynamic — конф и manage.bat; списки целей — из targets.txt
// (Load: env DEEPL_TARGETS / ./targets.txt, см. targets.go).
func Dynamic() *Config {
	c, _, err := Load(SchemeDynamic, "")
	if err != nil {
		panic(fmt.Sprintf("groups: нет читаемого targets.txt: %v", err))
	}
	return c
}

// Static — конф и manage_static.bat (GROUP1 = GROUP1 + EXCLUDE9).
func Static() *Config {
	c, _, err := Load(SchemeStatic, "")
	if err != nil {
		panic(fmt.Sprintf("groups: нет читаемого targets.txt: %v", err))
	}
	return c
}

// Load — Config схемы из targets.txt. Встроенного списка НЕТ (бинарник
// APK-списков не несёт): файл ищется и читается извне, при отсутствии /
// битом — ошибка. Приоритет:
//  1. path (явный аргумент, тесты);
//  2. env DEEPL_TARGETS — путь (например, общий файл на уровне выше
//     папок dynamic_hide/static_hide — как в bat);
//  3. ./targets.txt в CWD, затем восхождение по папкам-родителям вверх
//     (пока не найдётся или не упрёмся в корень ФС) — как у bat (%~dp0 и
//     %~dp0..\), поэтому deepl запускается и из подпапки, и из глубины
//     репозитория.
//
// Возвращает также путь, из которого список реально взят (для вывода).
func Load(scheme Scheme, path string) (*Config, string, error) {
	if path != "" {
		return loadTargets(scheme, path)
	}
	if p := os.Getenv("DEEPL_TARGETS"); p != "" {
		return loadTargets(scheme, p)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, "", err
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "targets.txt")); err == nil {
			return loadTargets(scheme, filepath.Join(dir, "targets.txt"))
		}
		parent := filepath.Dir(dir)
		if parent == dir { // корень ФС — выше некуда
			break
		}
		dir = parent
	}
	return nil, "", fmt.Errorf(
		"не найден targets.txt (CWD=%s и все родительские папки) — положите targets.txt рядом / на уровень выше (общий для dynamic_hide и static_hide) или задайте DEEPL_TARGETS=/путь; эталон — корневой targets.txt в git-репозитории", cwd)
}

// loadTargets — ParseFile + Build; битый/отсутствующий файл = ошибка.
func loadTargets(scheme Scheme, path string) (*Config, string, error) {
	t, err := ParseFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("%s: %w", path, err)
	}
	return Build(scheme, t), path, nil
}

// GroupLabelHuman — человекочитаемое имя группы (подпись в меню).
func GroupLabelHuman(i int) string {
	switch i {
	case 0:
		return "CRITICAL (системные сервисы)"
	case 1:
		return "CORE UI (основной интерфейс)"
	case 2:
		return "IMPORTANT APPS"
	case 3:
		return "SECONDARY (второстепенные)"
	default:
		return ""
	}
}

// Package — полный package-name и display-name цели.
// (Убивает 26-строчный :Lower из батников.)
func (c *Config) Package(name string) string {
	return c.Prefix + strings.ToLower(name)
}

// ApkPath — путь к APK на хосте (src для install).
func (c *Config) ApkPath(name string) string {
	return filepath.Join(c.ApkDir, name+"_RRO.apk")
}

// HasExcludes — есть ли список исключений (только dynamic).
func (c *Config) HasExcludes() bool { return len(c.Exclude) > 0 }

// excluded — в Exclude ли имя (без учёта регистра).
func (c *Config) excluded(name string) bool {
	for _, e := range c.Exclude {
		if strings.EqualFold(e, name) {
			return true
		}
	}
	return false
}

// AllCandidates — полный список для выбора (меню [L]): все группы
// (в порядке групп) плюс находящиеся в ApkDir, но не вошедшие в группы.
func (c *Config) AllCandidates() []string {
	seen := map[string]bool{}
	var out []string
	add := func(n string) {
		if n == "" || seen[strings.ToLower(n)] {
			return
		}
		seen[strings.ToLower(n)] = true
		out = append(out, n)
	}
	for _, g := range c.Groups {
		for _, t := range g {
			add(t)
		}
	}
	for _, t := range c.AutoTargets() {
		add(t)
	}
	return out
}

// All — все целевые (1+2+3+4), без дублей, в порядке групп.
func (c *Config) All() []string {
	seen := map[string]bool{}
	var out []string
	for _, g := range c.Groups {
		for _, t := range g {
			if !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	return out
}

// AutoTargets — все *_RRO.apk из ApkDir (имена без суффикса).
// Для dynamic исключаются EXCLUDE9.
func (c *Config) AutoTargets() []string {
	entries, err := os.ReadDir(c.ApkDir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, "_RRO.apk") {
			continue
		}
		base := strings.TrimSuffix(n, "_RRO.apk")
		if c.HasExcludes() && c.excluded(base) {
			continue
		}
		names = append(names, base)
	}
	sort.Strings(names)
	return names
}

// ResolvePreset — возвращает список целей и пресет.
// preset "0"/"" -> ok=false (меню/назад).
// "auto" -> AutoTargets.
func (c *Config) ResolvePreset(preset string) ([]string, string, bool) {
	switch strings.ToUpper(preset) {
	case Preset1:
		return clone(c.Groups[0]), preset, true
	case Preset2:
		return clone(c.Groups[1]), preset, true
	case Preset3:
		return clone(c.Groups[2]), preset, true
	case Preset4:
		return clone(c.Groups[3]), preset, true
	case Preset8:
		return clone(c.Top8), preset, true
	case PresetA:
		return c.All(), preset, true
	case PresetC:
		var out []string
		out = append(out, c.Groups[1]...)
		out = append(out, c.Groups[2]...)
		return dedupe(out), preset, true
	case PresetAuto:
		return c.AutoTargets(), preset, true
	case Preset0, "":
		return nil, preset, false
	}
	// Прочее — имя(на) пакетов (регистр не важен, для Package() — ToLower):
	// "Camera" | "WT_Launcher AdayoDvr" ...
	names := strings.Fields(preset)
	if len(names) == 0 {
		return nil, preset, false
	}
	return c.normalize(names), preset, true
}

// normalize — приводит введённые имена к каноническому регистру
// (по списку: All() + AutoTargets); неизвестные — как есть.
func (c *Config) normalize(names []string) []string {
	known := map[string]string{}
	for _, n := range c.All() {
		known[strings.ToLower(n)] = n
	}
	for _, n := range c.AutoTargets() {
		if _, ok := known[strings.ToLower(n)]; !ok {
			known[strings.ToLower(n)] = n
		}
	}
	out := make([]string, 0, len(names))
	seen := map[string]bool{}
	for _, n := range names {
		if canon, ok := known[strings.ToLower(n)]; ok {
			n = canon
		}
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}

func clone(in []string) []string {
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range in {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
