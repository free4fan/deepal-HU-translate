#!/bin/bash
# Сборка Go-программ: deepl (go/…) и shot (logshot/…) — Linux amd64 + Windows amd64.
#
# Использование:
#   build_go.sh              - все модули (deepl + shot): gofmt + vet + тесты + сборка
#   build_go.sh --no-test    - без `go test` (только gofmt + vet + сборка)
#   build_go.sh deepl        - только deepl (go/)
#   build_go.sh shot         - только shot (logshot/)
#   build_go.sh deepl shot   - явный список модулей
#
# Результат — в корень проекта (держим в git — проект работает и на Windows
# (.bat), и на Linux): ./deepl, ./deepl.exe, ./shot, ./shot.exe.
# После правок кода: commit этих 4 бинарников вместе с исходниками.
set -euo pipefail
cd "$(dirname "$0")"
ROOT=$(pwd)

command -v go >/dev/null 2>&1 || {
    echo "ERROR: go не найден в PATH."
    exit 1
}
echo "Go: $(go version)"

NOTEST=0
ARGS=()
for a in "$@"; do
    case "$a" in
        --no-test) NOTEST=1 ;;
        deepl|shot) ARGS+=("$a") ;;
        -h|--help)
            grep '^# ' "$0" | sed 's/^# \{0,1\}//'
            exit 0
            ;;
        *)
            echo "ERROR: неизвестный параметр \"$a\" (deepl | shot | --no-test)"
            exit 1
            ;;
    esac
done
[ ${#ARGS[@]} -eq 0 ] && ARGS=(deepl shot)

build_module() {
    local mod="$1" dir name cmd
    case "$mod" in
        deepl) dir=go;      name=deepl; cmd=./cmd/deepl ;;
        shot)  dir=logshot; name=shot;  cmd=./cmd/logshot ;;
    esac

    echo .
    echo "════════ $name (модуль $dir/) ════════"

    cd "$ROOT/$dir"

    echo .
    echo "[$name] gofmt (проверка)..."
    local out
    out=$(gofmt -l . 2>&1) || { echo "ERROR: [$name] gofmt завершился с ошибкой"; exit 1; }
    if [ -n "$out" ]; then
        echo "ERROR: [$name] не отформатированные файлы (gofmt -l):"
        echo "$out"
        echo "  Исправить: (cd $dir && gofmt -w .)"
        exit 1
    fi
    echo "[OK] [$name] gofmt"

    echo .
    echo "[$name] go vet..."
    (cd "$ROOT/$dir" && go vet ./...)
    echo "[OK] [$name] vet"

    if [ "$NOTEST" != "1" ]; then
        echo .
        echo "[$name] go test..."
        (cd "$ROOT/$dir" && go test ./...)
        echo "[OK] [$name] тесты"
    fi

    echo .
    echo "[$name] сборка $name (linux/amd64)..."
    (cd "$ROOT/$dir" && CGO_ENABLED=0 go build -o "$ROOT/$name" "$cmd")
    echo "[OK] $name        (Linux amd64)"

    echo .
    echo "[$name] сборка $name.exe (windows/amd64)..."
    (cd "$ROOT/$dir" && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o "$ROOT/$name.exe" "$cmd")
    echo "[OK] $name.exe    (Windows amd64)"
}

for m in "${ARGS[@]}"; do
    build_module "$m"
done

echo .
echo "========================================================"
echo "  ГОТОВО (в корне проекта $ROOT):"
ls -la "$ROOT/deepl" "$ROOT/deepl.exe" "$ROOT/shot" "$ROOT/shot.exe" 2>/dev/null | awk '{print "    " $NF, "($" $5 " байт)"}'
echo .
echo "  Не забудьте: git add deepl deepl.exe shot shot.exe"
echo "========================================================"
