// Package shot — снятие живого logcat-снимака: стрим adb logcat,
// сохранение в файл + индекс по времени (для охотника) + служебный
// снапшот состояния (pidof, pm path, uid, buffers).
package shot

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"logshot/internal/adb"
	"logshot/internal/hunt"
)

// Logger — живой снимок: пишет строки logcat в файл, держит строки в
// кольце (память) и отдаёт их охотнику.
type Logger struct {
	a    *adb.Adb
	LogF string // путь к *.log
	// Bufrs — буферы logcat, снимаемые одновременно (main events system crash).
	Bufrs []string
	// RingN — размер кольцевого буфера строк в памяти (для hunt).
	RingN int

	mu      sync.Mutex
	ring    []string
	file    *os.File
	fw      *bufio.Writer
	lines   int
	cancel  func()
	closing sync.Once
}

// New.
func New(a *adb.Adb, logPath string, bufrs []string, ringN int) *Logger {
	if len(bufrs) == 0 {
		bufrs = []string{"main", "system", "crash"}
	}
	if ringN <= 0 {
		ringN = 50000
	}
	return &Logger{a: a, LogF: logPath, Bufrs: bufrs, RingN: ringN, ring: make([]string, 0, ringN)}
}

// StartFile — создаёт файл и запущает стрим (бегает асинхронно).
func (l *Logger) StartFile(ctx context.Context) error {
	if err := os.MkdirAll(filepath.Dir(l.LogF), 0o755); err != nil {
		return err
	}
	f, err := os.Create(l.LogF)
	if err != nil {
		return err
	}
	l.file = f
	l.fw = bufio.NewWriterSize(f, 256*1024)
	l.mu.Lock()
	l.lines = 0
	l.mu.Unlock()
	// чистим хвостовые буферы, чтобы снять свежий (best eff; если rc!=0
	// — идём дальше, снимок всё равно будет).
	for _, b := range l.Bufrs {
		l.a.Exec(ctx, "logcat", "-b", b, "-c")
	}
	runCtx, cancel := context.WithCancel(ctx)
	l.mu.Lock()
	l.cancel = cancel
	l.mu.Unlock()
	var wg sync.WaitGroup
	wg.Add(len(l.Bufrs))
	for _, b := range l.Bufrs {
		go func(buf string) {
			defer wg.Done()
			args := []string{"-v", "threadtime"}
			if buf != "main" {
				args = append(args, "-b", buf)
			}
			tag := ""
			if buf != "main" {
				tag = " [" + buf + "]"
			}
			streamArgs := append([]string{"logcat"}, args...)
			l.a.Stream(runCtx, func(line string) error {
				l.push(tag, line)
				return nil
			}, streamArgs...)
		}(b)
	}
	go func() {
		wg.Wait()
		l.closeFile()
	}()
	return nil
}

// closeFile — flush+close ровно один раз (sync.Once): роняется и из
// фоновой goroutine (поток кончился), и из Stop() (durable).
func (l *Logger) closeFile() {
	l.closing.Do(func() {
		l.mu.Lock()
		if l.fw != nil {
			l.fw.Flush()
			l.fw = nil
		}
		if l.file != nil {
			l.file.Close()
			l.file = nil
		}
		l.mu.Unlock()
	})
}

// push — строка в файл и в кольцо.
// Формат: «HOST_TS [buf] ORIG_LINE» — host-штамп (наш момент записи)
// + метка буфера (для main пусто) + исходная строка logcat. host-штамп
// — с запасом для hunt.ParseLine (он опционален) и для человека
// (сколько прошло от старта снятия до видимости строки).
func (l *Logger) push(tag, line string) {
	stamped := fmt.Sprintf("%s%s %s\n", time.Now().Format("2006-01-02 15:04:05.000"), tag, line)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.fw != nil {
		l.fw.WriteString(stamped)
	}
	l.ring = append(l.ring, stamped)
	if len(l.ring) > l.RingN*2 {
		l.ring = l.ring[len(l.ring)/2:]
	}
	l.lines++
}

// Stop — останавливает стрим (cancel), ждём до 300ms до закрытия файла,
// после чего flush+close (closeFile идемпотентен).
func (l *Logger) Stop() {
	l.mu.Lock()
	if l.cancel != nil {
		l.cancel()
	}
	l.mu.Unlock()
	time.Sleep(300 * time.Millisecond)
	l.closeFile()
}

// Lines — счётчик принятых строк.
func (l *Logger) Lines() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.lines
}

// Exposed — копия строковых снимков в памяти (для охотника без чтения
// файла).
func (l *Logger) Exposed() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, len(l.ring))
	copy(out, l.ring)
	return out
}

// WriteMeta — служебный снапшот в .meta.txt (время, буферы, uid, pid,
// pm path, версии — всё «как было» на момент снятия). label — строка
// «охотящихся фраз» (для быстрого взгляда на снимок без открытия log).
func (l *Logger) WriteMeta(ctx context.Context, metaPath string, label string) error {
	var b strings.Builder
	b.WriteString("logshot meta\n")
	b.WriteString("phrases: " + label + "\n")
	b.WriteString("time: " + time.Now().Format("2006-01-02 15:04:05 MST") + "\n")
	b.WriteString("bufrs: " + strings.Join(l.Bufrs, ",") + "\n")
	b.WriteString("lines: " + fmt.Sprint(l.Lines()) + "\n")
	b.WriteString("log: " + l.LogF + "\n\n")
	b.WriteString("--- state ---\n")
	vals := map[string]string{
		"uid":           l.a.ShellS(ctx, "id", "-u"),
		"device":        l.a.ShellS(ctx, "getprop", "ro.product.model"),
		"build_date":    l.a.ShellS(ctx, "getprop", "ro.build.date"),
		"locale":        l.a.Shell(ctx, "getprop persist.sys.locale 2>/dev/null; getprop persist.sys.language 2>/dev/null; getprop ro.product.locale 2>/dev/null"),
		"camera_pid":    l.a.Shell(ctx, "pidof com.tinnove.camera 2>/dev/null || echo -"),
		"camera_apk":    l.a.Shell(ctx, "pm path com.tinnove.camera 2>/dev/null | head -1 || echo -"),
		"scenemode_pid": l.a.Shell(ctx, "pidof com.tinnove.scenemode 2>/dev/null || echo -"),
	}
	for _, k := range []string{"uid", "device", "build_date", "locale", "camera_pid", "camera_apk", "scenemode_pid"} {
		b.WriteString(k + ": " + strings.TrimSpace(strings.ReplaceAll(vals[k], "\n", " | ")) + "\n")
	}
	return os.WriteFile(metaPath, []byte(b.String()), 0o644)
}

// Hunt — поиск фраз в кольце (без чтения файла): в памяти быстрее.
func (l *Logger) Hunt(ctx context.Context, phrases []string) hunt.Report {
	l.mu.Lock()
	lines := make([]string, len(l.ring))
	copy(lines, l.ring)
	l.mu.Unlock()
	return hunt.Search(lines, phrases)
}

// LoadLogFromFile — читает *.log в память (для оффлайн-охоты по старому
// снимку), ring не трогает.
func LoadLogFromFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines, sc.Err()
}

// SortedPaths — *.log в kаталоге по дате файла (новая → старая).
func SortedPaths(dir string) []string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	type fp struct {
		path string
		ts   time.Time
	}
	var out []fp
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".log") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		fi, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, fp{p, fi.ModTime()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ts.After(out[j].ts) })
	var res []string
	for _, o := range out {
		res = append(res, o.path)
	}
	return res
}
