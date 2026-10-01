// Package adb — единственный источник работы с adb в проекте.
//
// В батниках (manage.bat / manage_static.bat) эвристика подключения
// (retry, unauthorized, kill-server) была продублирована 2 раза,
// `adb wait-for-device` мог блокировать интерфейс навсегда, а ping 127.0.0.1
// служил таймером. Здесь: явный цикл проверки с timeouts, Cancel через context.
package adb

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// State — состояние устройства по выводу `adb devices`.
type State int

const (
	StateNone   State = iota // строк с device/unauthorized нет
	StateUnauth              // есть unauthorized
	StateDevice              // есть device
)

func (s State) String() string {
	switch s {
	case StateDevice:
		return "device"
	case StateUnauth:
		return "unauthorized"
	default:
		return "none"
	}
}

// Client — интерфейс для потребителей (ui/flows/report), чтобы
// тесты могли подменить выполнение команд на мок.
type Client interface {
	// Check — полная проверка подключения: до 3 попыток `adb devices`,
	// рестарт демона при неудаче, ожидание авторизации (до 3×unauthWait).
	// false — если подключиться не удалось; true — устройство доступно.
	Check(ctx context.Context) bool
	// CheckFast — одна `adb devices`, без ожиданий (для баннера в меню).
	CheckFast(ctx context.Context) bool
	// Shell выполняет `adb shell <args...>`.
	Shell(ctx context.Context, args ...string) Result
	// ShellOut — как Shell, но только stdout (без stderr).
	ShellOut(ctx context.Context, args ...string) Result
	// Push выполняет `adb push local remote`.
	Push(ctx context.Context, local, remote string) Result
	// InstallR — `adb install -r apk`, совместный вывод (маркер "Success" в нём).
	InstallR(ctx context.Context, apk string) Result
	// Uninstall — `adb uninstall pkg`.
	Uninstall(ctx context.Context, pkg string) Result
	// Root — `adb root` (рестартует adbd).
	Root(ctx context.Context) Result
	// Reboot — `adb reboot` (перезагрузка ГУ).
	Reboot(ctx context.Context) Result
	// UID — `adb shell id -u`, 0 = root, 2000 = shell.
	UID(ctx context.Context) (int, string, error)
	// WaitDevice ждёт, пока device вернётся (polling, не `wait-for-device`).
	WaitDevice(ctx context.Context) error
	// KillServer / StartServer — рестарт demona.
	KillServer(ctx context.Context) Result
	StartServer(ctx context.Context) Result
	// Remount — `adb remount` (fallback после `mount -o rw,remount /vendor`).
	Remount(ctx context.Context) Result
	// Available — есть ли adb в PATH.
	Available() bool
	// Version — `adb version` на хосте.
	Version(ctx context.Context) Result
}

// Adb выполняет команды adb как подпроцесс.
type Adb struct {
	// Bin — путь к исполняемому файлу adb (по умолчанию ищется в PATH).
	Bin string

	// Logger — куда писать прогресс (по умолчанию os.Stdout).
	// Для тестов и тихих режимов можно заменить.
	Logger *strings.Builder

	// Настройки таймингов (в проде значения по умолчанию).
	UnauthWait  time.Duration // ожидание авторизации (по умолчанию 45s)
	RetryDelay  time.Duration // пауза между попытками (по умолчанию 3s)
	CmdTimeout  time.Duration // таймаут одной команды (по умолчанию 60s)
	InstallWait time.Duration // таймаут install (по умолчанию 5m)
	WaitDevPoll time.Duration // интервал polling WaitDevice (по умолчанию 1s)

	// execFn — точка подмены для тестов (не вызывается в проде).
	execFn func(ctx context.Context, name string, args ...string) (string, string, int)
	// availableFn — точка подмены проверки наличия adb (тесты).
	availableFn func() bool
}

// New создаёт Adb с настройками по умолчанию.
func New() *Adb {
	return &Adb{
		Bin:         "adb",
		UnauthWait:  45 * time.Second,
		RetryDelay:  3 * time.Second,
		CmdTimeout:  60 * time.Second,
		InstallWait: 5 * time.Minute,
		WaitDevPoll: time.Second,
	}
}

// Result — результат одной команды: вывод и код возврата.
type Result struct {
	Out  string // stdout+stderr (совокупно) или stdout (см. Shell/ShellOut)
	Code int    // 0 = успех
	Err  error  // неуспех запуска/таймаута (не rc≠0)
}

// OK сообщает, завершилась ли команда с кодом 0.
func (r Result) OK() bool { return r.Err == nil && r.Code == 0 }

// run — базовое выполнение одной команды с таймаутом.
// Возвращает (stdout, stderr, code).
func (a *Adb) run(ctx context.Context, args ...string) (string, string, int) {
	if a.execFn != nil {
		return a.execFn(ctx, a.Bin, args...)
	}
	ctx, cancel := context.WithTimeout(ctx, a.CmdTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, a.Bin, args...)
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			// контекст отменён/таймаут/не найдено: код -1
			code = -1
			if ctx.Err() != nil {
				return out.String(), fmt.Sprintf("%v\n%v", err, errb.String()), code
			}
			return out.String(), err.Error(), code
		}
	}
	return out.String(), errb.String(), code
}

// runResult — обёртка run → Result (совокупный вывод).
func (a *Adb) runResult(ctx context.Context, args ...string) Result {
	out, errb, code := a.run(ctx, args...)
	return Result{Out: strings.TrimRight(out+errb, "\n"), Code: code}
}

// runStdout — только stdout.
func (a *Adb) runStdout(ctx context.Context, args ...string) Result {
	out, _, code := a.run(ctx, args...)
	return Result{Out: strings.TrimRight(out, "\n"), Code: code}
}

// Available — есть ли adb в PATH (аналог `where adb`).
func (a *Adb) Available() bool {
	if a.availableFn != nil {
		return a.availableFn()
	}
	if a.Bin != "adb" {
		_, err := os.Stat(a.Bin)
		return err == nil
	}
	_, err := exec.LookPath("adb")
	return err == nil
}

// devicesState разбирает вывод `adb devices`.
// Формат (стр 2..N): "<serial>\t<state>", state ∈ {device, unauthorized, offline, ...}.
// Батник ищет состояние device$ и unauthorized$ в 2-м токене; здесь то же:
// если есть device — StateDevice (приоритет), иначе unauthorized — StateUnauth.
func devicesState(out string) State {
	st := StateNone
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch fields[len(fields)-1] {
		case "device":
			return StateDevice
		case "unauthorized":
			st = StateUnauth
		}
	}
	return st
}

// DevicesState — состояние по одной `adb devices` (публично для report/ui).
func (a *Adb) DevicesState(ctx context.Context) State {
	r := a.runStdout(ctx, "devices")
	return devicesState(r.Out)
}

// CheckFast — быстрая проверка (одна `adb devices`), для баннера меню:
// меню показывается всегда, независимо от статуса.
func (a *Adb) CheckFast(ctx context.Context) bool {
	if !a.Available() {
		return false
	}
	return a.DevicesState(ctx) == StateDevice
}

// Check — полная проверка подключения. Логия повторяет manage.bat:ADB_CHECK:
//
//	попытка 1..3:
//	  device       -> [OK] ADB connected. -> true
//	  unauthorized -> до 3×UnauthWait ожидания (Ctrl+C -> отмена); после 3 лож — стоп, false
//	  none         -> 3-я попытка: список советов, false
//	                 иначе: kill-server/start-server (1 раз), далее паузы RetryDelay
//
// Функция никогда не прерывает вызывающий — только true/false.
func (a *Adb) Check(ctx context.Context) bool {
	logf := a.logf
	if !a.Available() {
		logf("ERROR: adb not found in PATH.")
		logf("  Установите Android platform-tools и добавьте папку c adb в PATH.")
		return false
	}
	unauthTotal := 0
	killed := false
	for attempt := 1; attempt <= 3; attempt++ {
		select {
		case <-ctx.Done():
			return false
		default:
		}
		logf("ADB: проверка подключения (попытка %d/3)...", attempt)
		st := a.DevicesState(ctx)
		if st == StateDevice {
			logf("[OK] ADB connected.")
			return true
		}
		if st == StateUnauth {
			unauthTotal++
			if unauthTotal >= 3 {
				logf("  Авторизации нет после 3 проверок - остановка.")
				logf("  Разрешите USB-отладку на устройстве и повторите [R].")
				return false
			}
			logf("  Устройство подключено, но USB-отладка НЕ АВТОРИЗОВАНА.")
			logf("  НА УСТРОЙСТВЕ: нажмите \"АВТОРИЗОВАТЬ\", желательна галка")
			logf("  \"Всегда разрешать\". Ожидание %d сек... Ctrl+C - отмена.", int(a.UnauthWait.Seconds()))
			if !sleepCtx(ctx, a.UnauthWait) {
				return false
			}
			continue
		}
		// StateNone — device не найден.
		logf("  Устройство не найдено - нет строки с state=device.")
		if attempt >= 3 {
			logf("")
			logf("ERROR: ADB device not available after 3 attempts.")
			logf("  Попробуйте:")
			logf("   - USB-кабель и режим передачи файлов, а не только зарядки;")
			logf("   - на устройстве: Параметры - Для разработчиков - USB-отладка - вкл;")
			logf("   - галочка \"АВТОРИЗОВАТЬ\" на экране устройства;")
			logf("   - вручную: adb kill-server, затем adb devices")
			return false
		}
		if !killed {
			logf("  Перезапуск adb-демона: adb kill-server, adb start-server")
			a.KillServer(ctx)
			a.StartServer(ctx)
			killed = true
		}
		if !sleepCtx(ctx, a.RetryDelay) {
			return false
		}
	}
	return false
}

// SleepCtx — sleep с учётом отмены контекста. false — контекст отменён.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

func (a *Adb) logf(format string, args ...any) {
	var w strings.Builder
	fmt.Fprintf(&w, format+"\n", args...)
	if a.Logger != nil {
		a.Logger.WriteString(w.String())
		return
	}
	fmt.Fprint(os.Stdout, w.String())
}

// --- команды (реализация Client) ---

func (a *Adb) Shell(ctx context.Context, args ...string) Result {
	full := append([]string{"shell"}, args...)
	return a.runResult(ctx, full...)
}

func (a *Adb) ShellOut(ctx context.Context, args ...string) Result {
	full := append([]string{"shell"}, args...)
	return a.runStdout(ctx, full...)
}

func (a *Adb) Push(ctx context.Context, local, remote string) Result {
	return a.runResult(ctx, "push", local, remote)
}

// InstallR — `adb install -r`. Таймаут InstallWait (большой APK).
func (a *Adb) InstallR(ctx context.Context, apk string) Result {
	ctx2, cancel := context.WithTimeout(ctx, a.InstallWait)
	defer cancel()
	return a.runResult(ctx2, "install", "-r", apk)
}

func (a *Adb) Uninstall(ctx context.Context, pkg string) Result {
	return a.runResult(ctx, "uninstall", pkg)
}

func (a *Adb) Root(ctx context.Context) Result {
	return a.runResult(ctx, "root")
}

// UID — `adb shell id -u`. Возвращает (uid, raw, err).
func (a *Adb) UID(ctx context.Context) (int, string, error) {
	r := a.ShellOut(ctx, "id", "-u")
	s := strings.TrimSpace(r.Out)
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, s, fmt.Errorf("не удалось разобрать uid %q", s)
	}
	if r.Code != 0 {
		return 0, s, fmt.Errorf("adb shell id -u: rc=%d: %s", r.Code, r.Out)
	}
	return n, s, nil
}

// WaitDevice — polling до появления device (в отличие от `adb wait-for-device`,
// блокировавшего батник навсегда). Интервал WaitDevPoll.
func (a *Adb) WaitDevice(ctx context.Context) error {
	for {
		if a.DevicesState(ctx) == StateDevice {
			return nil
		}
		if !sleepCtx(ctx, a.WaitDevPoll) {
			return ctx.Err()
		}
	}
}

func (a *Adb) KillServer(ctx context.Context) Result  { return a.runResult(ctx, "kill-server") }
func (a *Adb) StartServer(ctx context.Context) Result { return a.runResult(ctx, "start-server") }

// Remount — `adb remount` (fallback после `mount -o rw,remount /vendor`).
func (a *Adb) Remount(ctx context.Context) Result { return a.runResult(ctx, "remount") }
func (a *Adb) Reboot(ctx context.Context) Result  { return a.runResult(ctx, "reboot") }

func (a *Adb) Version(ctx context.Context) Result { return a.runStdout(ctx, "version") }
