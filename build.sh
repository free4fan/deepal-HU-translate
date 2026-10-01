#!/bin/bash
# Сборка deepl (Go) — проверяет, тестирует и собирает бинарники.
#
# Использование:
#   ./build.sh              - gofmt + vet + тесты + сборка Linux и Windows
#   ./build.sh --no-test    - без тестов (быстрее)
#
# Результат (в корне проекта):
#   deepl       - Linux amd64
#   deepl.exe   - Windows amd64

set -e
cd "$(dirname "$0")"

CMD=./cmd/deepl

command -v go >/dev/null 2>&1 || {
    echo "ERROR: go не найден в PATH."
    echo "  Установите Go (>= 1.22) и добавьте в PATH."
    exit 1
}
echo "Go: $(go version)"

# --- targets.txt: синхронизация встроенной копии (go:embed) ---
# Единственный источник списков целей — ./targets.txt (читают manage.bat,
# manage_static.bat и deepl). go:embed берёт копию internal/groups/targets.txt
# (требует файл в каталоге пакета), поэтому перед тестами/сборкой копируем.
if [ ! -f targets.txt ]; then
    echo "ERROR: нет targets.txt (список целей) — скрипты без списков невозможны."
    exit 1
fi
cp -f targets.txt internal/groups/targets.txt
echo "[OK] targets.txt -> internal/groups/targets.txt"

# --- формат ---
echo .
echo "gofmt (проверка)..."
echo .
if ! out=$(gofmt -l . 2>/dev/null); then
    echo "ERROR: gofmt завершился с ошибкой"
    exit 1
fi
if [ -n "$out" ]; then
    echo "ERROR: не отформатированные файлы (gofmt -l):"
    echo "$out"
    echo "  Исправить: gofmt -w ."
    exit 1
fi
echo "[OK] gofmt"

# --- vet ---
echo .
echo "go vet..."
echo .
go vet ./...
echo "[OK] vet"

# --- тесты ---
if [ "$1" != "--no-test" ]; then
    echo .
    echo "go test ./..."
    echo .
    go test ./...
    echo "[OK] тесты"
fi

# --- сборка ---
echo .
echo "Сборка deepl (linux/amd64)..."
echo .
go build -o deepl "$CMD"
echo "[OK] deepl        (Linux amd64)"

echo .
echo "Сборка deepl.exe (windows/amd64)..."
echo .
GOOS=windows GOARCH=amd64 go build -o deepl.exe "$CMD"
echo "[OK] deepl.exe    (Windows amd64)"
echo .
echo "========================================================"
echo "  ГОТОВО:"
echo "    ./deepl       - Linux amd64"
echo "    ./deepl.exe   - Windows amd64"
echo "========================================================"
