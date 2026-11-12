package servico

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// pendente espera que o painel mostre um pedido de permissão e devolve-o.
func (a *ambiente) pendente() map[string]any {
	a.t.Helper()
	for prazo := time.Now().Add(3 * time.Second); time.Now().Before(prazo); time.Sleep(20 * time.Millisecond) {
		req, _ := http.NewRequest("GET", a.url+"/v1/painel", nil)
		req.Header.Set("Authorization", "Bearer "+a.token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			a.t.Fatal(err)
		}
		var e struct{ Permissoes []map[string]any }
		json.NewDecoder(resp.Body).Decode(&e)
		resp.Body.Close()
		if len(e.Permissoes) == 1 {
			return e.Permissoes[0]
		}
	}
	a.t.Fatal("a permissão não apareceu no painel")
	return nil
}

func (a *ambiente) semPendentes() bool {
	for prazo := time.Now().Add(3 * time.Second); time.Now().Before(prazo); time.Sleep(20 * time.Millisecond) {
		req, _ := http.NewRequest("GET", a.url+"/v1/painel", nil)
		req.Header.Set("Authorization", "Bearer "+a.token)
		resp, _ := http.DefaultClient.Do(req)
		var e struct{ Permissoes []map[string]any }
		json.NewDecoder(resp.Body).Decode(&e)
		resp.Body.Close()
		if len(e.Permissoes) == 0 {
			return true
		}
	}
	return false
}

func TestPermissaoRespondidaNoTerminalLibertaAEspera(t *testing.T) {
	a := novoAmbiente(t, nil)
	a.postJSON("/v1/painel", map[string]bool{"responder_permissoes": true})
	abrirSSE(t, a, "/v1/ao-vivo")
	entrada := map[string]any{"command": "echo two > spike2.txt"}
	pedido := map[string]any{"session_id": "t", "hook_event_name": "PermissionRequest", "tool_name": "Bash", "tool_input": entrada}

	resultado := make(chan map[string]any, 1)
	go func() { resultado <- a.responder(pedido) }()
	id := int(a.pendente()["id"].(float64))

	// Uma ferramenta paralela que acaba não liberta o pedido do Bash.
	a.responder(map[string]any{"session_id": "t", "hook_event_name": "PostToolUse", "tool_name": "Read",
		"tool_input": map[string]any{"file_path": "/tmp/x"}})
	select {
	case s := <-resultado:
		t.Fatalf("um PostToolUse de outra ferramenta não devia libertar o pedido: %v", s)
	case <-time.After(100 * time.Millisecond):
	}

	a.responder(map[string]any{"session_id": "t", "hook_event_name": "PostToolUse", "tool_name": "Bash", "tool_input": entrada})
	select {
	case s := <-resultado:
		if s != nil {
			t.Fatalf("respondido no terminal, o hook não devia decidir nada: %v", s)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("o PostToolUse do mesmo comando devia libertar a espera")
	}
	if !a.semPendentes() {
		t.Fatal("o cartão devia sair do painel")
	}
	if codigo, _ := a.postJSON(fmt.Sprintf("/v1/permissoes/%d", id), map[string]string{"decisao": "deny"}); codigo != http.StatusGone {
		t.Fatalf("decisão atrasada devia dar 410, deu %d", codigo)
	}
}

func TestStopLibertaPedidosDaSessao(t *testing.T) {
	a := novoAmbiente(t, nil)
	a.postJSON("/v1/painel", map[string]bool{"responder_permissoes": true})
	abrirSSE(t, a, "/v1/ao-vivo")
	resultado := make(chan map[string]any, 1)
	go func() {
		resultado <- a.responder(map[string]any{"session_id": "s", "hook_event_name": "PermissionRequest", "tool_name": "Bash",
			"tool_input": map[string]any{"command": "ls"}})
	}()
	a.pendente()
	a.responder(map[string]any{"session_id": "s", "hook_event_name": "Stop"})
	select {
	case s := <-resultado:
		if s != nil {
			t.Fatalf("o Stop devia libertar sem decisão: %v", s)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("o Stop devia libertar a espera")
	}
}

func TestPerguntaRespondidaComOpcao(t *testing.T) {
	a := novoAmbiente(t, nil)
	a.postJSON("/v1/painel", map[string]bool{"responder_permissoes": true})
	abrirSSE(t, a, "/v1/ao-vivo")
	perguntas := []any{map[string]any{"question": "Which drink do you prefer?", "header": "Drink", "multiSelect": false,
		"options": []any{map[string]any{"label": "Tea"}, map[string]any{"label": "Coffee"}}}}
	resultado := make(chan map[string]any, 1)
	go func() {
		resultado <- a.responder(map[string]any{"session_id": "q", "hook_event_name": "PermissionRequest",
			"tool_name": "AskUserQuestion", "tool_input": map[string]any{"questions": perguntas}})
	}()
	p := a.pendente()
	if p["resumo"] != "Which drink do you prefer?" {
		t.Fatalf("o resumo devia ser a pergunta: %v", p["resumo"])
	}
	if e, _ := p["entrada"].(map[string]any); e == nil || len(e["questions"].([]any)) != 1 {
		t.Fatalf("o painel devia mostrar as perguntas e opções: %v", p)
	}
	id := int(p["id"].(float64))
	if codigo, _ := a.postJSON(fmt.Sprintf("/v1/permissoes/%d", id), map[string]any{"decisao": "deny",
		"respostas": map[string]string{"Which drink do you prefer?": "Tea"}}); codigo != http.StatusBadRequest {
		t.Fatalf("recusar com respostas devia dar 400, deu %d", codigo)
	}
	if codigo, _ := a.postJSON(fmt.Sprintf("/v1/permissoes/%d", id), map[string]any{"decisao": "allow",
		"respostas": map[string]string{"Which drink do you prefer?": "Coffee"}}); codigo != 200 {
		t.Fatalf("a resposta devia ser aceite, deu %d", codigo)
	}
	saida := <-resultado
	d := saida["hookSpecificOutput"].(map[string]any)["decision"].(map[string]any)
	entrada, _ := d["updatedInput"].(map[string]any)
	if d["behavior"] != "allow" || entrada == nil {
		t.Fatalf("faltou updatedInput: %v", saida)
	}
	if entrada["answers"].(map[string]any)["Which drink do you prefer?"] != "Coffee" || entrada["questions"] == nil {
		t.Fatalf("updatedInput devia levar as perguntas e a resposta: %v", entrada)
	}
}

func TestNotificacaoOciosaNaoBloqueia(t *testing.T) {
	casos := []struct {
		ev   evento
		quer bool
	}{
		{evento{TipoNotificacao: "idle_prompt"}, true},
		{evento{TipoNotificacao: "permission_prompt", Mensagem: "Claude needs your permission to use Bash"}, false},
		{evento{Mensagem: "Claude is waiting for your input"}, true},
		{evento{Mensagem: "Claude needs your permission to use Bash"}, false},
	}
	for _, c := range casos {
		if got := ociosa(c.ev); got != c.quer {
			t.Errorf("ociosa(%+v) = %v, queria %v", c.ev, got, c.quer)
		}
	}
}
