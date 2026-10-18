// Package adb — работа с adb для logshot: минимальный набор команд
// (devices, shell, exec, live-stream, pull, root), все команды бегут как
// подпроцессы. Точки подмены (execFn / streamFn) — только для тестов.
package adb

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Adb — тонкая обёртка над adb CLI.
type Adb struct {
	Bin string
	// Serial — опциональный целевой serial (`adb -s <serial> ...`).
	Serial string
	// Logger — куда писать служебные сообщения (nil = Stdout).
	Logger *strings.Builder
	// CmdTimeout — таймаут одиночных коротких команд (devices/pull/exec).
	CmdTimeout time.Duration
	// execFn / streamFn — точки подмены тестами (в проде не вызываются).
	execFn   func(ctx context.Context, name string, args ...string) (stdout, stderr string, code int)
	streamFn func(ctx context.Context, name string, args []string, lineFn func(string) error) error
}

// SetExecFn — подмена коротких команд (тесты).
func (a *Adb) SetExecFn(fn func(ctx context.Context, name string, args ...string) (string, string, int)) {
	a.execFn = fn
}

// SetStreamFn — подмена живого потока (тесты). Мок сам вызывает lineFn
// на каждую «пришлую» строку и отвечает, завершён ли поток.
func (a *Adb) SetStreamFn(fn func(ctx context.Context, name string, args []string, lineFn func(string) error) error) {
	a.streamFn = fn
}

// New.
func New() *Adb {
	return &Adb{Bin: "adb", CmdTimeout: 30 * time.Second}
}

func (a *Adb) logf(format string, args ...any) {
	s := fmt.Sprintf(format+"\n", args...)
	if a.Logger != nil {
		a.Logger.WriteString(s)
		return
	}
	os.Stdout.WriteString(s)
}

// Device — строка `adb devices`.
type Device struct {
	Serial string
	State  string
}

// Devices — список устройств из `adb devices` (без заголовка).
func (a *Adb) Devices(ctx context.Context) []Device {
	out := a.run(ctx, "devices")
	var res []Device
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			continue // заголовок «List of devices attached» (3 токенa) и мусор
		}
		if f[1] == "state" {
			continue
		}
		res = append(res, Device{Serial: f[0], State: f[1]})
	}
	return res
}

// prefixArgs — префикс `adb -s <serial>` (если Serial задан).
func (a *Adb) prefixArgs() []string {
	if a.Serial != "" {
		return []string{"-s", a.Serial}
	}
	return nil
}

func (a *Adb) run(ctx context.Context, args ...string) string {
	full := append(a.prefixArgs(), args...)
	if a.execFn != nil {
		out, _, _ := a.execFn(ctx, a.Bin, full...)
		return out
	}
	ctx2, cancel := context.WithTimeout(ctx, a.CmdTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx2, a.Bin, full...)
	var b strings.Builder
	cmd.Stdout = &b
	cmd.Stderr = &b
	if err := cmd.Run(); err != nil && ctx2.Err() == nil {
		a.logf("adb %s: %v", strings.Join(args, " "), err)
	}
	return b.String()
}

// Shell — `adb shell <cmd>` (ВЕСЬ cmd — ОДИН аргумент). Важно: НЕ
// `shell sh -lc <cmd>` отдельными аргументами — современный adb склизывает
// всё после `shell` в одну строку БЕЗ ПЕРЕКАВЫЧИВАНИЯ (`sh -lc getprop
// persist.sys.locale; getprop ...`), device-shell парсит `-c` только для
// ПЕРВОГО слова → `getprop` без аргумента = полный дамп пропсов (баг 04.10:
// meta.txt locale/camera_pid/camera_apk/scenemode_pid битые + online-аттр pids).
// Один аргумент → adbd передаёт device-shell строку ЦЕЛИКОМ, `;|<` 2>/dev/null
// разбираются штатно (sh -c эквивалентно).
func (a *Adb) Shell(ctx context.Context, shCmd string) string {
	return a.run(ctx, "shell", shCmd)
}

// ShellS — `adb shell <parts...>` (parts без пробелов).
func (a *Adb) ShellS(ctx context.Context, parts ...string) string {
	full := append([]string{"shell"}, parts...)
	return a.run(ctx, full...)
}

// Exec — `adb exec-out <args...>` (чистый stdout без перевода \n, нужен
// для `logcat -b <name> -c`).
func (a *Adb) Exec(ctx context.Context, args ...string) string {
	full := append([]string{"exec-out"}, args...)
	return a.run(ctx, full...)
}

// Stream — долгоживущая сессия `adb <args...>` (логкат): stdout читается
// попстрочно, каждая строка идёт в fn. ok=false — adb не запустился/
// контекст отменён до старта; fn возвращает error — поток обрываем (это
// штатное завершение, ok=true).
func (a *Adb) Stream(ctx context.Context, fn func(line string) error, args ...string) bool {
	if a.streamFn != nil {
		arr := make([]string, len(args))
		copy(arr, args)
		return a.streamFn(ctx, a.Bin, arr, fn) == nil
	}
	full := append(a.prefixArgs(), args...)
	cmd := exec.CommandContext(ctx, a.Bin, full...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		a.logf("adb %s (pipe): %v", strings.Join(full, " "), err)
		return false
	}
	var errb strings.Builder
	cmd.Stderr = &errb
	if err := cmd.Start(); err != nil {
		a.logf("adb %s (запуск): %v", strings.Join(full, " "), err)
		return false
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
		for sc.Scan() {
			if fn(sc.Text()) != nil {
				return
			}
		}
	}()
	werr := cmd.Wait()
	<-done
	if werr != nil && ctx.Err() == nil {
		a.logf("adb %s (поток): %v %s", strings.Join(args, " "), werr, errb.String())
		return false
	}
	return true
}

// Pull — `adb pull remote local`.
func (a *Adb) Pull(ctx context.Context, remote, local string) string {
	return a.run(ctx, "pull", remote, local)
}

// UID — `adb shell id -u` (0=root, 2000=shell); -3 = нераспознано.
func (a *Adb) UID(ctx context.Context) int {
	out := strings.TrimSpace(a.Shell(ctx, "id -u"))
	n, err := strconv.Atoi(out)
	if err != nil {
		return -3
	}
	return n
}

// Root — `adb root` (adbd перезапустится; вызывающий должен Потом
// WaitDevice).
func (a *Adb) Root(ctx context.Context) string {
	return a.run(ctx, "root")
}

// WaitDevice — polling до появления device (700ms), false — timeout/отмена.
func (a *Adb) WaitDevice(ctx context.Context, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, d := range a.Devices(ctx) {
			if d.State == "device" {
				return true
			}
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(700 * time.Millisecond):
		}
	}
	return false
}
