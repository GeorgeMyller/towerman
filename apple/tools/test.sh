#!/usr/bin/env bash
# Testes das políticas puras dos apps Apple, sem Xcode nem simulador: compila os modelos
# partilhados e os testes num executável com o swiftc e corre-o. Chamado pelo CI no macOS.
set -euo pipefail
cd "$(dirname "$0")/.."
out="${TMPDIR:-/tmp}/towerman-apple-tests"
swiftc -parse-as-library -O \
  Shared/AgentModels.swift Shared/WatchSyncPolicy.swift \
  tests/WatchSyncPolicyTests.swift -o "$out"
"$out"
