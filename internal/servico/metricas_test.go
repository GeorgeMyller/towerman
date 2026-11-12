package servico

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/GeorgeMyller/towerman/internal/quadro"
)

func (a *ambiente) metricas() quadro.Metricas {
	a.t.Helper()
	a.esperar()
	var m quadro.Metricas
	a.obter("/v1/metricas?dias=1", &m)
	return m
}

// pedidoNaRede abre um pedido de permissão e espera que apareça no estado da rede.
func (a *ambiente) pedidoNaRede(token, sessao string) (agente, pedido string, resultado chan map[string]any) {
	a.t.Helper()
	resultado = make(chan map[string]any, 1)
	go func() {
		resultado <- a.responder(map[string]any{"session_id": sessao, "hook_event_name": "PermissionRequest", "tool_name": "Bash",
			"tool_input": map[string]any{"command": "make test"}})
	}()
	for prazo := time.Now().Add(3 * time.Second); pedido == "" && time.Now().Before(prazo); time.Sleep(20 * time.Millisecond) {
		_, st := a.rede("GET", "/v1/state", token, nil)
		for _, x := range st["agents"].([]any) {
			ag := x.(map[string]any)
			if w, ok := ag["waiting"].(map[string]any); ok && ag["id"] == sessao {
				agente, pedido = ag["id"].(string), w["id"].(string)
			}
		}
	}
	if pedido == "" {
		a.t.Fatal("o pedido não chegou ao estado da rede")
	}
	return agente, pedido, resultado
}

func TestCanalDaPermissaoPeloRelogio(t *testing.T) {
	a := novoAmbiente(t, nil)
	token := a.parearAparelho()
	agente, pedido, resultado := a.pedidoNaRede(token, "r")
	if c, _ := a.rede("POST", fmt.Sprintf("/v1/agents/%s/answer", agente), token,
		map[string]string{"promptId": pedido, "key": "allow", "origin": "watch"}); c != http.StatusNoContent {
		t.Fatalf("responder devia dar 204, deu %d", c)
	}
	<-resultado

	m := a.metricas()
	if m.Atencao.ForaDoTerminal != (quadro.Proporcao{Numerador: 1, Denominador: 1}) {
		t.Fatalf("fora do terminal: %+v", m.Atencao.ForaDoTerminal)
	}
	if len(m.Atencao.PorCanal) != 1 {
		t.Fatalf("por canal: %+v", m.Atencao.PorCanal)
	}
	l := m.Atencao.PorCanal[0]
	if l.Origem != quadro.OrigemRelogio || l.Nome != "relógio" || l.Canal == quadro.CanalPainel || l.Canal == "" {
		t.Fatalf("canal do relógio: %+v", l)
	}
}

func TestCanalDaPermissaoNoPainel(t *testing.T) {
	a := novoAmbiente(t, nil)
	a.postJSON("/v1/painel", map[string]bool{"responder_permissoes": true})
	abrirSSE(t, a, "/v1/ao-vivo")
	resultado := make(chan map[string]any, 1)
	go func() {
		resultado <- a.responder(map[string]any{"session_id": "p", "hook_event_name": "PermissionRequest", "tool_name": "Bash",
			"tool_input": map[string]any{"command": "ls"}})
	}()
	id := int(a.pendente()["id"].(float64))
	if c, _ := a.postJSON(fmt.Sprintf("/v1/permissoes/%d", id), map[string]string{"decisao": "deny"}); c != 200 {
		t.Fatalf("decidir no painel: %d", c)
	}
	<-resultado
	m := a.metricas()
	if len(m.Atencao.PorCanal) != 1 || m.Atencao.PorCanal[0].Canal != quadro.CanalPainel || m.Atencao.PorCanal[0].Nome != "Painel" {
		t.Fatalf("canal do painel: %+v", m.Atencao.PorCanal)
	}
}

// O texto ditado no relógio fecha a decisão como resposta por texto, mesmo com o UserPromptSubmit
// da entrega a chegar logo a seguir (antes fechava como «terminal»).
func TestTextoLivreFechaDecisaoComoTexto(t *testing.T) {
	tmuxFalso(t)
	a := novoAmbiente(t, nil)
	token := a.parearAparelho()
	a.enviarNoTmux(map[string]any{"session_id": "s", "hook_event_name": "UserPromptSubmit", "prompt": "x"})
	a.enviarNoTmux(map[string]any{"session_id": "s", "hook_event_name": "Stop", "last_assistant_message": "Qual nome prefere para a branch?"})
	if _, ok := a.decisaoAberta("s"); !ok {
		t.Fatal("sem decisão")
	}
	if c, r := a.rede("POST", "/v1/agents/s/input", token, map[string]string{"text": "feat/x", "origin": "watch"}); c != 200 || r["state"] != "entregue" {
		t.Fatalf("input: %d %v", c, r)
	}
	a.enviarNoTmux(map[string]any{"session_id": "s", "hook_event_name": "UserPromptSubmit", "prompt": "feat/x"})

	m := a.metricas()
	if m.Decisoes.PorBotao != (quadro.Proporcao{Numerador: 0, Denominador: 1}) {
		t.Fatalf("a resposta por texto devia contar como texto: %+v", m.Decisoes.PorBotao)
	}
	if len(m.Atencao.PorCanal) != 1 || m.Atencao.PorCanal[0].Origem != quadro.OrigemRelogio {
		t.Fatalf("canal: %+v", m.Atencao.PorCanal)
	}
}

// Sem entrega imediata, a mensagem fica na fila e a decisão continua por responder.
func TestTextoSemEntregaReabreDecisao(t *testing.T) {
	a := novoAmbiente(t, nil)
	token := a.parearAparelho()
	a.enviar(map[string]any{"session_id": "s", "hook_event_name": "UserPromptSubmit", "prompt": "x"})
	a.responder(map[string]any{"session_id": "s", "hook_event_name": "Stop", "last_assistant_message": "Qual nome prefere para a branch?"})
	a.esperar()
	antes, ok := a.decisaoAberta("s")
	if !ok {
		t.Fatal("sem decisão")
	}
	if c, r := a.rede("POST", "/v1/agents/s/input", token, map[string]string{"text": "feat/x"}); c != 200 || r["state"] == "entregue" {
		t.Fatalf("input sem terminal não devia ser entregue: %d %v", c, r)
	}
	a.esperar()
	depois, ok := a.decisaoAberta("s")
	if !ok || depois.ID != antes.ID || depois.Pergunta == "" {
		t.Fatalf("a decisão devia continuar aberta, com o mesmo id: %+v %v", depois, ok)
	}
}

func TestRotasDeMetricas(t *testing.T) {
	a := novoAmbiente(t, nil)
	if r := a.pedir("GET", "/v1/metricas", nil); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("sem token devia dar 401, deu %d", r.StatusCode)
	}
	comToken := func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+a.token) }
	if r := a.pedir("GET", "/v1/metricas?dias=90", comToken); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("dias=90 devia dar 400, deu %d", r.StatusCode)
	}
	var m quadro.Metricas
	a.obter("/v1/metricas", &m)
	if m.Dias != 7 || m.Computador == "" {
		t.Fatalf("omissão devia ser 7 dias: %+v", m)
	}

	token := a.parearAparelho()
	if c, _ := a.rede("GET", "/v1/metrics?days=30", "", nil); c != http.StatusUnauthorized {
		t.Fatalf("rede sem token devia dar 401, deu %d", c)
	}
	if c, r := a.rede("GET", "/v1/metrics?days=30", token, nil); c != 200 || r["dias"] != float64(30) {
		t.Fatalf("rede: %d %v", c, r)
	}
	if c, _ := a.rede("GET", "/v1/metrics?days=2", token, nil); c != http.StatusBadRequest {
		t.Fatalf("days=2 devia dar 400, deu %d", c)
	}
	_, st := a.rede("GET", "/v1/state", token, nil)
	caps := fmt.Sprint(st["capabilities"])
	if caps != "[launch metrics]" {
		t.Fatalf("capacidades: %s", caps)
	}
	if c, r := a.envelopeRelay(map[string]any{"op": "metrics"}); c != 200 || r["dias"] != float64(7) {
		t.Fatalf("relay sem days: %d %v", c, r)
	}
	if c, r := a.envelopeRelay(map[string]any{"op": "metrics", "days": 1}); c != 200 || r["dias"] != float64(1) {
		t.Fatalf("relay: %d %v", c, r)
	}
}
