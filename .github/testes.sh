#!/usr/bin/env bash
# Portões de teste do CI: vet, testes e bench do hook. Corre no runner ou, para o Linux no Mac
# mini, dentro de um contentor golang (ci.yml).
set -euo pipefail
go vet ./...
go test ./...
# Políticas puras dos apps Apple, só onde há swiftc (macOS); não precisam de Xcode.
if command -v swiftc >/dev/null 2>&1; then apple/tools/test.sh; fi

export CGO_ENABLED=0
go build -o "towerman-bench$(go env GOEXE)" ./cmd/towerman
export TOWERMAN_HOME="${RUNNER_TEMP:-/tmp}/towerman"
# Porta ao acaso: o Mac mini corre vários jobs ao mesmo tempo, e uma porta fixa chocava.
export TOWERMAN_PORTA=$((20000 + RANDOM % 20000))
./towerman-bench bench -n 1000
# Num runner próprio a máquina não é apagada no fim: o servidor do bench sai com o script.
"./towerman-bench$(go env GOEXE)" serve &
trap "kill $!" EXIT
sleep 2
./towerman-bench bench -n 1000
./towerman-bench bench -n 1000 --evento SessionStart
