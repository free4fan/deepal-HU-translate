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
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	// ResolveBin — поиск бинарника adb: env DEEPL_ADB -> PATH -> рядом с
	// бинарником вызывающей программы (deepl.exe/adb.exe в одной папке).
	// error —
	// adb не найден нигде (или DEEPL_ADB указывает на несуществующий файл).
	// Печатает [ADB] using: <путь> в stderr. Вызывать 1 раз до Check/Shell.
	ResolveBin() error
	// Check — полная проверка подключения: до 3 попыток `adb devices`,
	// рестарт демона при неудаче, ожидание авторизации (до 3×unauthWait).
	// false — если подключиться не удалось; true — устройство доступно.
	Check(ctx context.Context) bool
	// CheckFast — одна `adb devices`, без ожиданий (для баннера в меню).
	CheckFast(ctx context.Context) bool
	// DevicesState — состояние по одной `adb devices` (для баннера меню:
	// none vs unauthorized — разный текст подсказки).
	DevicesState(ctx context.Context) State
	// CheckResult — как Check/CheckFast, но возвращает результат:
	// Ok — device доступен, Err — что не так (adb не найден / устройство
	// не найдено / unauthorized после 3 проверок). Строки пишутся в лог
	// (Logger/stdout) — вызывающий может показать их пользователю.
	CheckResult(ctx context.Context, fast bool) CheckResult
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

// CheckResult — результат проверки подключения (fast или полная Check).
// Строки, какие уже напечатал вызов в лог (Logger/stdout).
type CheckResult struct {
	OK  bool
	Err error // нет adb / устройство не найдено / unauthorized (3 проверки)
}

// Adb выполняет команды adb как подпроцесс.
type Adb struct {
	// Bin — путь к исполняемому файлу adb (заполняется ResolveBin:
	// env DEEPL_ADB -> PATH -> рядом с бинарником deepl).
	Bin string

	// Logger — куда писать прогресс (по умолчанию os.Stdout).
	// io.Writer: меню подставляет свой out, чтобы строки [R] были
	// видны в терминале; тесты — буфер.
	Logger io.Writer

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
	// selfBin — путь к бинарнику вызывающей программы (тесты; прод —
	// os.Executable).
	selfBin func() (string, error)
}

// SelfBin — путь к бинарнику вызывающей программы.
func selfBinFallback() (string, error) { return os.Executable() }

// ResolveBin — поиск бинарника adb (приоритет — сверху вниз):
//
//  1. env DEEPL_ADB — явный путь (используется как есть; ошибка, если файла нет);
//  2. PATH — `adb`/`adb.exe` из PATH (`exec.LookPath`);
//  3. рядом с бинарником deepl (каталог `os.Executable()`) — `adb.exe`
//     (Windows) или `adb` (прочие ОС).
//
// Найденный путь кладёт в a.Bin и печатает `[ADB] using: <путь>` в stderr
// (чтобы было видно, каким adb работаем). a.Bin="" — инициализация без
// поиска (тесты подставляют Bin/execFn сами).
func (a *Adb) ResolveBin() error {
	if p := os.Getenv("DEEPL_ADB"); p != "" {
		if fi, err := os.Stat(p); err != nil || fi.IsDir() {
			return fmt.Errorf("DEEPL_ADB указывает на несуществующий файл: %q", p)
		}
		a.Bin = p
		a.showADB(p)
		return nil
	}
	if p, err := exec.LookPath("adb"); err == nil {
		a.Bin = p
		a.showADB(p)
		return nil
	}
	fn := a.selfBin
	if fn == nil {
		fn = selfBinFallback
	}
	if self, err := fn(); err == nil {
		name := "adb"
		if runtime.GOOS == "windows" {
			name = "adb.exe"
		}
		if p := filepath.Join(filepath.Dir(self), name); isExecutable(p) {
			a.Bin = p
			a.showADB(p)
			return nil
		}
	}
	return fmt.Errorf("adb не найден: нет в PATH и нет рядом с бинарником deepl. " +
		"Положите adb.exe в ту же папку или укажите путь: DEEPL_ADB=/путь/adb.exe")
}

func isExecutable(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

// showADB — заметка, каким adb работаем (stderr: как строка targets в main).
func (a *Adb) showADB(p string) {
	fmt.Fprintf(os.Stderr, "[ADB] using: %s\n\n", p)
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

// CheckResult — один прогон проверки (быстрый или полный); все строки
// печатаются в лог (Logger/stdout), результат в OK/Err.
//
// fast=true  — одна `adb devices` (баннер меню [R] не блокирует), без
// ожиданий и рестарта демона;
// fast=false — полный цикл из manage.bat:ADB_CHECK:
//
//	попытка 1..3:
//	  device       -> [OK] ADB connected.
//	  unauthorized -> до 3×UnauthWait ожидания (Ctrl+C -> отмена); после 3 лож — стоп
//	  none         -> 3-я попытка: список советов; иначе kill-server/start-server (1 раз)
//
// Функция никогда не прерывает вызывающего — только OK/Err.
func (a *Adb) CheckResult(ctx context.Context, fast bool) CheckResult {
	logf := a.logf
	if !a.Available() {
		logf("ERROR: adb not found: нет в PATH и нет рядом с бинарником deepl.")
		logf("  Положите adb.exe в ту же папку, что deepl.exe, или укажите путь:")
		logf("  set DEEPL_ADB=C:\\путь\\adb.exe  (или добавьте папку в PATH).")
		return CheckResult{Err: fmt.Errorf("adb не найден: нет в PATH и рядом с бинарником deepl (укажите DEEPL_ADB=путь к adb)")}
	}
	if fast {
		switch a.DevicesState(ctx) {
		case StateDevice:
			logf("[OK] ADB connected.")
			return CheckResult{OK: true}
		case StateUnauth:
			logf("  ГУ подключен, но ADB-ключ НЕ АВТОРИЗОВАН: публичный ключ adb этого ПК")
			logf("  не добавлен на ГУ (устройство «unauthorized»).")
			logf("  Исправление: запустите adbauth (на этом ПК) и добавьте публичный")
			logf("  ключ adb этого ПК (adbkey.pub).")
			return CheckResult{Err: fmt.Errorf("устройство не авторизовано: публичный ключ adb этого ПК не добавлен на ГУ - добавьте его через adbauth (adbkey.pub)")}
		default:
			logf("  Устройство не найдено - нет строки с state=device.")
			logf("  Подключите USB-кабель к ГУ и включите ADB: инженерное меню ГУ")
			logf("  или adbauth (программа, которая передаёт ключ USB-отладки).")
			return CheckResult{Err: fmt.Errorf("устройство не найдено: подключите USB-кабель и включите ADB (инженерное меню ГУ или adbauth)")}
		}
	}
	unauthTotal := 0
	killed := false
	for attempt := 1; attempt <= 3; attempt++ {
		select {
		case <-ctx.Done():
			return CheckResult{Err: ctx.Err()}
		default:
		}
		logf("ADB: проверка подключения (попытка %d/3)...", attempt)
		st := a.DevicesState(ctx)
		if st == StateDevice {
			logf("[OK] ADB connected.")
			return CheckResult{OK: true}
		}
		if st == StateUnauth {
			unauthTotal++
			if unauthTotal >= 3 {
				logf("  После 3 проверок устройство не авторизовано - остановка.")
				logf("  Добавьте публичный ключ adb этого ПК через adbauth и повторите [R].")
				return CheckResult{Err: fmt.Errorf("после 3 проверок устройство не авторизовано: публичный ключ adb этого ПК не добавлен на ГУ - добавьте его через adbauth")}
			}
			logf("  ГУ подключен, но ADB-ключ НЕ АВТОРИЗОВАН: публичный ключ adb этого ПК")
			logf("  не добавлен на ГУ (устройство «unauthorized»).")
			logf("  Исправление: запустите adbauth (на этом ПК) и добавьте публичный")
			logf("  ключ adb этого ПК (adbkey.pub).")
			logf("  (Если ADB ещё не включён - включите в инженерном меню ГУ.)")
			logf("  Ожидание %d сек... Ctrl+C - отмена.", int(a.UnauthWait.Seconds()))
			if !sleepCtx(ctx, a.UnauthWait) {
				return CheckResult{Err: ctx.Err()}
			}
			continue
		}
		// StateNone — device не найден.
		logf("  Устройство не найдено - нет строки с state=device.")
		if attempt >= 3 {
			logf("")
			logf("ERROR: ADB device not available after 3 attempts.")
			logf("  Попробуйте:")
			logf("   - USB-кабель к ГУ (правильный порт, не через USB-хаб);")
			logf("   - включить ADB на ГУ: инженерное меню (adbauth передаёт ключ USB-отладки);")
			logf("   - вручную: adb kill-server, затем adb devices")
			return CheckResult{Err: fmt.Errorf("устройство не найдено после 3 попыток: подключите кабель и включите ADB (инженерное меню ГУ; ключ - через adbauth)")}
		}
		if !killed {
			logf("  Перезапуск adb-демона: adb kill-server, adb start-server")
			a.KillServer(ctx)
			a.StartServer(ctx)
			killed = true
		}
		if !sleepCtx(ctx, a.RetryDelay) {
			return CheckResult{Err: ctx.Err()}
		}
	}
	return CheckResult{Err: fmt.Errorf("устройство не найдено")}
}

// Check — полная проверка подключения. false — подключиться не удалось
// (строки уже в логе).
func (a *Adb) Check(ctx context.Context) bool {
	return a.CheckResult(ctx, false).OK
}

// CheckFast — быстрая проверка (одна `adb devices`), для баннера меню:
// меню показывается всегда, независимо от статуса. ТИШАЯ: строки
// CheckResult напечатал бы ПОД СТРОКОЙ БАННЕРА (дубль; симптом 27.10
// «[OK] ADB connected. + ADB: [OK] ...»), баннер печатает своё.
func (a *Adb) CheckFast(ctx context.Context) bool {
	prev := a.Logger
	a.Logger = io.Discard
	defer func() { a.Logger = prev }()
	return a.CheckResult(ctx, true).OK
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
	if a.Logger == nil {
		a.Logger = os.Stdout
	}
	_, _ = io.WriteString(a.Logger, w.String())
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
