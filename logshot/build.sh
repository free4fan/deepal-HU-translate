#!/bin/bash
# Сборка logshot (Go) — снимок logcat + охота за фразой.
#
# Использование:
#   logshot/build.sh            - gofmt + vet + тесты + сборка Linux/Windows
#   logshot/build.sh --no-test  - без тестов
#
# Модуль лежит в logshot/ (свой go.mod — независим от deepl);
# результат — в корень проекта: logshot / logshot.exe.
set -e
cd "$(dirname "$0")"
ROOT=$(cd .. && pwd)

CMD=./cmd/logshot
OUT_LOGSHOT="$ROOT/shot"
OUT_LOGSHOT_EXE="$ROOT/shot.exe"

command -v go >/dev/null 2>&1 || {
    echo "ERROR: go не найден в PATH."
    exit 1
}
echo "Go: $(go version)"

echo .
echo "gofmt (проверка)..."
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

echo .
echo "go vet..."
go vet ./...
echo "[OK] vet"

if [ "$1" != "--no-test" ]; then
    echo .
    echo "go test ./..."
    go test ./...
    echo "[OK] тесты"
fi

echo .
echo "Сборка shot (linux/amd64)..."
go build -o "$OUT_LOGSHOT" "$CMD"
echo "[OK] shot        (Linux amd64)"

echo .
echo "Сборка shot.exe (windows/amd64)..."
GOOS=windows GOARCH=amd64 go build -o "$OUT_LOGSHOT_EXE" "$CMD"
echo "[OK] shot.exe    (Windows amd64)"
echo .
echo "========================================================"
echo "  ГОТОВО (в корне проекта):"
echo "    ./shot        - Linux amd64"
echo "    ./shot.exe    - Windows amd64"
echo "========================================================"
