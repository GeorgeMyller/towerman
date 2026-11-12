#!/usr/bin/env bash
# Demonstração da coordenação entre sessões do Towerman, sem tocar na instalação real:
# regra 1 (worktree com dona), regra 2 (ask/deny/off), regra 3 (outra branch),
# GET /v1/avisos, towerman ls, towerman wt e towerman wt limpar.
# Tudo corre num TOWERMAN_HOME temporário, num repo git temporário e numa porta livre.
set -euo pipefail

RAIZ="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d /tmp/towerman-demo.XXXXXX)"
BIN="$TMP/towerman"
export TOWERMAN_HOME="$TMP/home"
export TOWERMAN_CLAUDE_DIR="$TMP/claude"
export TOWERMAN_TIMEOUT_MS=5000
export TOWERMAN=
mkdir -p "$TOWERMAN_HOME" "$TOWERMAN_CLAUDE_DIR/sessions"

# ---------- utilitários ----------
B=$'\033[1m'; C=$'\033[36m'; Y=$'\033[33m'; G=$'\033[32m'; N=$'\033[0m'
titulo() { printf '\n%s%s━━━ %s ━━━%s\n' "$B" "$C" "$1" "$N"; }
nota()   { printf '%s%s%s\n' "$Y" "$1" "$N"; }
comando(){ printf '%s$ %s%s\n' "$G" "$1" "$N"; }

PIDS=()
limpar() {
  for p in "${PIDS[@]:-}"; do [ -n "$p" ] && kill "$p" 2>/dev/null || true; done
  [ -n "${SERVE_PID:-}" ] && kill -INT "$SERVE_PID" 2>/dev/null || true
  rm -rf "$TMP"
}
trap limpar EXIT

# Cada "sessão" da demo é um processo vivo (sleep) registado como o Claude Code regista as suas:
# $TOWERMAN_CLAUDE_DIR/sessions/<pid>.json. É daí que o Towerman tira o nome da sessão.
sessao() { # sessao <id> <nome> <cwd>
  sleep 600 & local pid=$!; disown "$pid"
  PIDS+=("$pid")
  printf '{"pid":%d,"sessionId":"%s","cwd":"%s","name":"%s"}' "$pid" "$1" "$3" "$2" \
    > "$TOWERMAN_CLAUDE_DIR/sessions/$pid.json"
}

# hook <descrição> <json do evento>: corre "towerman hook" como o agente faria e mostra a resposta.
hook() {
  comando "towerman hook  ←  $1"
  local saida
  saida="$(printf '%s' "$2" | "$BIN" hook)"
  if [ -z "$saida" ]; then
    echo "   (sem resposta: nada a avisar)"
  else
    printf '%s' "$saida" | jq -r '
      .hookSpecificOutput as $h
      | if $h.permissionDecision then
          "   permissionDecision: \($h.permissionDecision)\n   motivo: \($h.permissionDecisionReason)"
        elif $h.additionalContext then
          "   additionalContext:\n" + ($h.additionalContext | split("\n") | map("     " + .) | join("\n"))
        else . end'
  fi
  sleep 0.3
}

ev() { # ev <session_id> <evento> <cwd> [tool_name] [file_path]
  if [ $# -ge 5 ]; then
    printf '{"session_id":"%s","hook_event_name":"%s","cwd":"%s","tool_name":"%s","tool_input":{"file_path":"%s"}}' "$1" "$2" "$3" "$4" "$5"
  else
    printf '{"session_id":"%s","hook_event_name":"%s","cwd":"%s"}' "$1" "$2" "$3"
  fi
}

# ---------- 0. compilar e preparar o repo ----------
titulo "0. Compilar o towerman e criar um repo de brincar"
comando "go build -o $BIN ./cmd/towerman"
(cd "$RAIZ" && CGO_ENABLED=0 go build -o "$BIN" ./cmd/towerman)

REPO="$TMP/demo-repo"
WT_LOGIN="$TMP/demo-repo-wt/feat-login"
WT_API="$TMP/demo-repo-wt/feat-api"
WT_VELHA="$TMP/demo-repo-wt/feat-velha"
git init -q -b main "$REPO"
git -C "$REPO" config user.name "Demo"
git -C "$REPO" config user.email "demo@exemplo.pt"
echo "olá" > "$REPO/a.txt"; echo "api" > "$REPO/api.go"
# O commit inicial tem 3 dias: o «wt limpar» só propõe branches criadas há mais de um dia.
HA3D="$(python3 -c 'import time;print(int(time.time())-3*86400)') +0000"
git -C "$REPO" add . && GIT_AUTHOR_DATE="$HA3D" GIT_COMMITTER_DATE="$HA3D" git -C "$REPO" commit -q -m "inicial"
git -C "$REPO" worktree add -q -b feat/login "$WT_LOGIN"
git -C "$REPO" worktree add -q -b feat/api "$WT_API"
# Branch «velha»: sem commits próprios → integrada na base, limpa e sem sessão.
git -C "$REPO" branch feat/velha main
git -C "$REPO" worktree add -q "$WT_VELHA" feat/velha
# O repo fica conhecido pelas sessões; repos.txt mostra a outra via (um caminho por linha).
(cd "$REPO" && pwd -P) > "$TOWERMAN_HOME/repos.txt"
echo "repo:       $REPO (main)"
echo "worktrees:  feat/login  → $WT_LOGIN"
echo "            feat/api    → $WT_API"
echo "            feat/velha  → $WT_VELHA (integrada, 3 dias)"

# ---------- 1. arrancar o serviço ----------
titulo "1. Arrancar o serviço numa porta livre"
PORTA="$(python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1])')"
export TOWERMAN_PORTA="$PORTA"
comando "towerman serve   (TOWERMAN_HOME=$TOWERMAN_HOME, porta $PORTA)"
"$BIN" serve >"$TMP/serve.log" 2>&1 & SERVE_PID=$!
for _ in $(seq 1 100); do [ -f "$TOWERMAN_HOME/token" ] && curl -sf -o /dev/null -H "Authorization: Bearer $(cat "$TOWERMAN_HOME/token")" "http://127.0.0.1:$PORTA/estado" && break; sleep 0.1; done
TOKEN="$(cat "$TOWERMAN_HOME/token")"
echo "serviço pronto; painel em http://127.0.0.1:$PORTA/"

# ---------- 2. regra 1: uma árvore, uma dona ----------
titulo "2. Regra 1 — «uma árvore, uma dona» (SessionStart)"
sessao ana   "Ana"   "$WT_LOGIN"
sessao bruno "Bruno" "$WT_LOGIN"
sessao carla "Carla" "$WT_API"
hook "Ana arranca em feat/login (sozinha)"     "$(ev ana SessionStart "$WT_LOGIN")"
hook "Ana edita a.txt"                         "$(ev ana PostToolUse "$WT_LOGIN" Edit "$WT_LOGIN/a.txt")"
nota "→ Bruno abre uma segunda sessão NA MESMA worktree. Ao arrancar fica a saber quem lá está e o que mexeu:"
hook "Bruno arranca em feat/login"             "$(ev bruno SessionStart "$WT_LOGIN")"

# ---------- 3. aviso sem bloquear (regra 2 off) ----------
titulo "3. Aviso de colisão na mesma branch, sem bloquear (regra 2 desligada)"
comando "towerman regra2 off"; "$BIN" regra2 off
hook "Bruno vai editar a.txt (PreToolUse)"     "$(ev bruno PreToolUse "$WT_LOGIN" Edit "$WT_LOGIN/a.txt")"
nota "→ Sem bloqueio. Mas depois de editar, o aviso chega na mesma (PostToolUse):"
hook "Bruno editou a.txt (PostToolUse)"        "$(ev bruno PostToolUse "$WT_LOGIN" Edit "$WT_LOGIN/a.txt")"

# ---------- 4. regra 2: ask / deny ----------
titulo "4. Regra 2 — pedir confirmação antes de editar (ask, o padrão)"
comando "towerman regra2 ask"; "$BIN" regra2 ask
hook "Bruno tenta editar a.txt outra vez"      "$(ev bruno PreToolUse "$WT_LOGIN" Edit "$WT_LOGIN/a.txt")"
nota "→ O Claude Code mostra este motivo e pede confirmação ao utilizador. Um ficheiro que a Ana não tocou passa livre:"
hook "Bruno edita api.go (ninguém mexeu)"      "$(ev bruno PreToolUse "$WT_LOGIN" Edit "$WT_LOGIN/api.go")"

titulo "5. Regra 2 em modo deny — recusa logo"
comando "towerman regra2 deny"; "$BIN" regra2 deny
hook "Bruno tenta editar a.txt"                "$(ev bruno PreToolUse "$WT_LOGIN" Edit "$WT_LOGIN/a.txt")"
"$BIN" regra2 ask >/dev/null

# ---------- 6. regra 3: outra branch ----------
titulo "6. Regra 3 — o mesmo ficheiro noutra branch (aviso de conflito futuro)"
hook "Carla arranca em feat/api"               "$(ev carla SessionStart "$WT_API")"
hook "Carla edita a.txt na branch feat/api"    "$(ev carla PostToolUse "$WT_API" Edit "$WT_API/a.txt")"
nota "→ Noutra branch a regra 2 não trava (só avisa do merge futuro):"
hook "Carla vai editar a.txt (PreToolUse)"     "$(ev carla PreToolUse "$WT_API" Edit "$WT_API/a.txt")"

# ---------- 7. sessão terminada liberta o ficheiro ----------
titulo "7. Quando a dona termina, o bloqueio desaparece"
hook "Ana termina a sessão (SessionEnd)"       "$(ev ana SessionEnd "$WT_LOGIN")"
sleep 0.5
hook "Bruno tenta editar a.txt"                "$(ev bruno PreToolUse "$WT_LOGIN" Edit "$WT_LOGIN/a.txt")"

# ---------- 8. o que ficou registado ----------
titulo "8. Avisos registados no quadro (GET /v1/avisos) e sessões vivas (towerman ls)"
comando "curl -H 'Authorization: Bearer \$TOKEN' http://127.0.0.1:$PORTA/v1/avisos"
curl -s -H "Authorization: Bearer $TOKEN" "http://127.0.0.1:$PORTA/v1/avisos" \
  | jq -r '.[] | "   regra \(.regra)  \(.caminho)  \(.nome_de // .de) → \(.nome_para // .para)"'
comando "towerman ls --todas"
"$BIN" ls --todas | sed 's/^/   /'

# ---------- 9. inventário de worktrees ----------
titulo "9. towerman wt — inventário de worktrees e branches"
comando "towerman wt"
"$BIN" wt | sed 's/^/   /'

titulo "10. towerman wt limpar — propõe só o que é seguro e pede confirmação"
nota "→ feat/velha: integrada, limpa, sem sessão e com mais de um dia. feat/login e feat/api têm sessões vivas."
comando "towerman wt limpar   (resposta: n)"
echo n | "$BIN" wt limpar | sed 's/^/   /'
comando "towerman wt limpar   (resposta: s)"
echo s | "$BIN" wt limpar | sed 's/^/   /'
comando "towerman wt   (depois da limpeza)"
"$BIN" wt | sed 's/^/   /'

titulo "Fim"
echo "Painel desta demo (enquanto o script corre): http://127.0.0.1:$PORTA/"
echo "Tudo foi criado em $TMP e será apagado ao sair."
if [ "${DEMO_PAUSA:-}" = "1" ]; then
  nota "DEMO_PAUSA=1: o serviço fica no ar. Abra o painel e veja os separadores Mapa, Worktrees e Avisos. Enter para terminar."
  read -r _
fi
