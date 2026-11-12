package servico

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/GeorgeMyller/towerman/internal/hook"
	"github.com/GeorgeMyller/towerman/internal/quadro"
)

// evAgente envia ao processador um evento de outro agente, com o terminal e o ppid do hook.
func evAgente(t *testing.T, p *Processador, agente string, term hook.Terminal, ppid int, m map[string]any) {
	t.Helper()
	ev, _ := json.Marshal(m)
	env := hook.Envelope{Agente: agente, RecebidoEm: time.Now().UTC().Format(time.RFC3339Nano), Ppid: ppid,
		Terminal: term, Evento: ev}
	if _, err := p.Processar(env); err != nil {
		t.Fatal(err)
	}
}

// Cada volta do agente do Antigravity IDE é atividade: reativa a sessão e avança o visto_em, sem
// fechar a decisão aberta, sem mudar o resumo e sem contar como pedido.
func TestContinuacaoEAtividade(t *testing.T) {
	a := novoAmbiente(t, nil)
	p := a.srv.proc
	relogio := time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC)
	p.Agora = func() time.Time { relogio = relogio.Add(time.Second); return relogio }
	ag := func(m map[string]any) { evAgente(t, p, "antigravity", hook.Terminal{}, os.Getpid(), m) }

	ag(map[string]any{"session_id": "g", "hook_event_name": "UserPromptSubmit", "prompt": "corrige o teste"})
	ag(map[string]any{"session_id": "g", "hook_event_name": "Stop", "last_assistant_message": fimComMenu})
	if _, ok := a.decisaoAberta("g"); !ok {
		t.Fatal("o Stop devia abrir uma decisão")
	}
	antes := a.sessao("g")
	if antes.Estado != quadro.Parada {
		t.Fatalf("depois do Stop devia estar parada: %q", antes.Estado)
	}
	desde, ate := relogio.Add(-time.Hour), relogio.Add(time.Hour)
	m0, err := a.srv.Quadro().Metricas(desde, ate)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 12; i++ {
		ag(map[string]any{"session_id": "g", "hook_event_name": "Continuacao"})
	}
	depois := a.sessao("g")
	if depois.Estado != quadro.Ativa {
		t.Fatalf("a continuação devia pôr a sessão ativa: %q", depois.Estado)
	}
	if depois.VistoEm <= antes.VistoEm {
		t.Fatalf("visto_em devia avançar: %q -> %q", antes.VistoEm, depois.VistoEm)
	}
	if depois.Resumo != "corrige o teste" {
		t.Fatalf("a continuação não muda o resumo: %q", depois.Resumo)
	}
	if _, ok := a.decisaoAberta("g"); !ok {
		t.Fatal("a continuação não fecha a decisão aberta")
	}
	m1, _ := a.srv.Quadro().Metricas(desde, ate)
	j0, _ := json.Marshal(m0)
	j1, _ := json.Marshal(m1)
	if string(j0) != string(j1) {
		t.Fatalf("12 continuações mudaram as métricas:\n%s\n%s", j0, j1)
	}
}

// Para o Antigravity IDE, o PID guardado é o do IDE (VSCODE_PID), que dura enquanto a janela
// está aberta; o ppid do hook é efémero.
func TestIdentificarAntigravityGuardaVscodePid(t *testing.T) {
	a := novoAmbiente(t, nil)
	p := a.srv.proc
	ide, ppid := os.Getpid(), os.Getppid()
	term := hook.Terminal{VscodePid: strconv.Itoa(ide)}
	inicio := map[string]any{"hook_event_name": "UserPromptSubmit", "prompt": "olá"}
	com := func(id string) map[string]any {
		m := map[string]any{"session_id": id}
		for k, v := range inicio {
			m[k] = v
		}
		return m
	}

	evAgente(t, p, "antigravity", term, ppid, com("ide"))
	if s := a.sessao("ide"); s.Pid != ide {
		t.Fatalf("antigravity com VSCODE_PID vivo devia guardar %d, guardou %d", ide, s.Pid)
	}
	evAgente(t, p, "antigravity", hook.Terminal{}, ppid, com("sem"))
	if s := a.sessao("sem"); s.Pid != ppid {
		t.Fatalf("sem VSCODE_PID devia guardar o ppid %d, guardou %d", ppid, s.Pid)
	}
	evAgente(t, p, "antigravity", hook.Terminal{VscodePid: "abc"}, ppid, com("lixo"))
	if s := a.sessao("lixo"); s.Pid != ppid {
		t.Fatalf("VSCODE_PID inválido devia guardar o ppid %d, guardou %d", ppid, s.Pid)
	}
	evAgente(t, p, "claude", term, ppid, com("claude"))
	if s := a.sessao("claude"); s.Pid != ppid {
		t.Fatalf("fora do antigravity, o VSCODE_PID não conta: guardou %d", s.Pid)
	}
}
