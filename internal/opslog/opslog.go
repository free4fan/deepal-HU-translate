package opslog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// OpsLog — ops_<mode>_<TS>.log: каждое изменяющее действие пишется
// в отдельный файл (аналог manage.bat: OPS_LOG). Сохраняются
// маркерные строки: шапка, "[ROOT]", "RESULT:", подвал со счётчиками —
// при live-анализе без "[ROOT]" = bat не умел переключать root (см. manage.bat).
type OpsLog struct {
	f    *os.File
	path string
}

// Open создаёт logs/<mode>_<YYYYMMDDHHMMSS>.log и пишет шапку.
func Open(logsDir, mode, preset, schema string, now time.Time) (*OpsLog, error) {
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		return nil, err
	}
	name := fmt.Sprintf("ops_%s_%s.log", mode, now.Format("20060102150405"))
	f, err := os.Create(filepath.Join(logsDir, name))
	if err != nil {
		return nil, err
	}
	header := fmt.Sprintf("==== %s | mode=%s preset=%s | schema=%s | начало ====",
		now.Format("2006-01-02 15:04:05"), mode, preset, schema)
	_, werr := f.WriteString(header + "\n")
	if werr != nil {
		f.Close()
		return nil, werr
	}
	return &OpsLog{f: f, path: f.Name()}, nil
}

// Path — путь к файлу (для вывода «Операции записаны:»).
// Не меняет состояния: работает и после Close.
func (o *OpsLog) Path() string {
	if o == nil {
		return ""
	}
	return o.path
}

// WriteLine — одна строка в лог (идемпотентно при nil-рецепторе).
func (o *OpsLog) WriteLine(s string) {
	if o == nil || o.f == nil {
		return
	}
	_, _ = o.f.WriteString(s + "\n")
}

// Root — маркерная строка [ROOT] (live-анализ ищет их).
func (o *OpsLog) Root(format string, args ...any) {
	o.WriteLine("  [ROOT] " + fmt.Sprintf(format, args...))
}

// Result — "RESULT: OK/ERROR/WARN" (чтение и батники, и новый код).
func (o *OpsLog) Result(format string, args ...any) {
	o.WriteLine("  " + fmt.Sprintf("RESULT: "+format, args...))
}

// Begin — строка «---- [MODE] name [pkg] ...» перед операцией.
func (o *OpsLog) Begin(format string, args ...any) {
	o.WriteLine("---- " + fmt.Sprintf(format, args...))
}

// Db — строка вывода adb: «  db: ...» (конфрмат bat).
func (o *OpsLog) Db(line string) {
	o.WriteLine("  db: " + line)
}

// Close — подвал со счётчиками: "==== ... | конец: всего=N ok=N err=N нет_apk=N ====".
func (o *OpsLog) Close(now time.Time, total, ok, fail, miss int) {
	if o == nil || o.f == nil {
		return
	}
	tail := fmt.Sprintf("==== %s | конец: всего=%d ok=%d err=%d нет_apk=%d ====",
		now.Format("2006-01-02 15:04:05"), total, ok, fail, miss)
	_, _ = o.f.WriteString(tail + "\n")
	_ = o.f.Close()
	o.f = nil
}

// LastOpsFiles — N самых свежих logs/ops_*.log (для отчёта: копируется в zip).
// Возвращает полные пути, от новых к старым.
func LastOpsFiles(logsDir string, n int) []string {
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		return nil
	}
	type ft struct {
		path string
		m    time.Time
	}
	var files []ft
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "ops_") || !strings.HasSuffix(name, ".log") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, ft{filepath.Join(logsDir, name), info.ModTime()})
	}
	// новые первыми
	for i := 0; i < len(files); i++ {
		for j := i + 1; j < len(files); j++ {
			if files[j].m.After(files[i].m) {
				files[i], files[j] = files[j], files[i]
			}
		}
	}
	out := make([]string, 0, n)
	for i, f := range files {
		if i >= n {
			break
		}
		out = append(out, f.path)
	}
	return out
}
