package servico

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/GeorgeMyller/towerman/internal/quadro"
)

func (a *ambiente) responder(evento map[string]any) map[string]any {
	a.t.Helper()
	contador++
	corpo := envelope(evento, fmt.Sprintf("2026-10-07T11:00:00.%09dZ", contador))
	req, _ := http.NewRequest(http.MethodPost, a.url+"/v1/eventos?responder=1", bytes.NewReader(corpo))
	req.Header.Set("Authorization", "Bearer "+a.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer resp.Body.Close()
	var r struct {
		Saida map[string]any `json:"saida"`
	}
	json.NewDecoder(resp.Body).Decode(&r)
	return r.Saida
}

func (a *ambiente) postJSON(caminho string, corpo any) (int, map[string]any) {
	a.t.Helper()
	b, _ := json.Marshal(corpo)
	req, _ := http.NewRequest(http.MethodPost, a.url+caminho, bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+a.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer resp.Body.Close()
	var r map[string]any
	json.NewDecoder(resp.Body).Decode(&r)
	return resp.StatusCode, r
}

func contexto(saida map[string]any) string {
	h, _ := saida["hookSpecificOutput"].(map[string]any)
	c, _ := h["additionalContext"].(string)
	return c
}

func TestRegra1MesmaWorktree(t *testing.T) {
	_, wt := novoRepo(t)
	a := novoAmbiente(t, nil)
	if s := a.responder(map[string]any{"session_id": "dona", "hook_event_name": "SessionStart", "cwd": wt}); s != nil {
		t.Fatalf("primeira sessão não devia receber contexto: %v", s)
	}
	a.enviar(map[string]any{"session_id": "dona", "hook_event_name": "PostToolUse", "cwd": wt, "tool_name": "Edit",
		"tool_input": map[string]any{"file_path": wt + "/api/x.go"}})
	c := contexto(a.responder(map[string]any{"session_id": "segunda", "hook_event_name": "SessionStart", "cwd": wt}))
	if !strings.Contains(c, "já tem dona") || !strings.Contains(c, "dona") || !strings.Contains(c, "api/x.go") {
		t.Fatalf("contexto da regra 1 inesperado:\n%s", c)
	}
}

func TestRegra3OutraBranchSemRepetir(t *testing.T) {
	repo, wt := novoRepo(t)
	a := novoAmbiente(t, nil)
	a.enviar(map[string]any{"session_id": "a", "hook_event_name": "PostToolUse", "cwd": repo, "tool_name": "Edit",
		"tool_input": map[string]any{"file_path": repo + "/a.txt"}})
	editar := func() string {
		return contexto(a.responder(map[string]any{"session_id": "b", "hook_event_name": "PostToolUse", "cwd": wt, "tool_name": "Edit",
			"tool_input": map[string]any{"file_path": wt + "/a.txt"}}))
	}
	c := editar()
	if !strings.Contains(c, "«a.txt»") || !strings.Contains(c, "branch main") || !strings.Contains(c, "conflito") {
		t.Fatalf("aviso da regra 3 inesperado: %q", c)
	}
	if c := editar(); c != "" {
		t.Fatalf("o mesmo aviso não devia repetir-se na mesma hora: %q", c)
	}
	a.enviar(map[string]any{"session_id": "a", "hook_event_name": "SessionEnd"})
	a.esperar()
	c = contexto(a.responder(map[string]any{"session_id": "c", "hook_event_name": "PostToolUse", "cwd": wt, "tool_name": "Edit",
		"tool_input": map[string]any{"file_path": wt + "/a.txt"}}))
	if strings.Contains(c, "«a»") {
		t.Fatalf("sessão terminada não devia gerar aviso: %q", c)
	}
}

func TestMensagemEntregueNoStop(t *testing.T) {
	a := novoAmbiente(t, nil)
	a.enviar(map[string]any{"session_id": "m", "hook_event_name": "UserPromptSubmit", "prompt": "faz x"})
	a.esperar()
	codigo, m := a.postJSON("/v1/sessoes/m/mensagens", map[string]string{"texto": "lembra-te de y"})
	if codigo != 200 || m["estado"] != quadro.MensagemPendente || m["caminho"] != "stop" {
		t.Fatalf("mensagem inesperada: %d %v", codigo, m)
	}
	saida := a.responder(map[string]any{"session_id": "m", "hook_event_name": "Stop"})
	if saida["decision"] != "block" || !strings.Contains(saida["reason"].(string), "lembra-te de y") {
		t.Fatalf("Stop devia entregar a mensagem: %v", saida)
	}
	if s := a.responder(map[string]any{"session_id": "m", "hook_event_name": "Stop"}); s != nil {
		t.Fatalf("a mensagem não devia ser entregue duas vezes: %v", s)
	}
	ms, _ := a.srv.Quadro().MensagensRecentes("m", 5)
	if len(ms) != 1 || ms[0].Estado != quadro.MensagemEntregue {
		t.Fatalf("estado final inesperado: %+v", ms)
	}
}

func TestMensagemFalhaQuandoSessaoTermina(t *testing.T) {
	a := novoAmbiente(t, nil)
	a.enviar(map[string]any{"session_id": "f", "hook_event_name": "UserPromptSubmit", "prompt": "x"})
	a.esperar()
	a.postJSON("/v1/sessoes/f/mensagens", map[string]string{"texto": "olá"})
	a.enviar(map[string]any{"session_id": "f", "hook_event_name": "SessionEnd"})
	a.esperar()
	ms, _ := a.srv.Quadro().MensagensRecentes("f", 5)
	if len(ms) != 1 || ms[0].Estado != quadro.MensagemFalhada || ms[0].Motivo == "" {
		t.Fatalf("mensagem devia falhar com motivo: %+v", ms)
	}
	if codigo, _ := a.postJSON("/v1/sessoes/f/mensagens", map[string]string{"texto": "outra"}); codigo != http.StatusConflict {
		t.Fatalf("sessão terminada devia dar 409, deu %d", codigo)
	}
	if codigo, _ := a.postJSON("/v1/sessoes/f/mensagens", map[string]string{"texto": "  "}); codigo != http.StatusBadRequest {
		t.Fatalf("texto vazio devia dar 400, deu %d", codigo)
	}
}

func abrirSSE(t *testing.T, a *ambiente, caminho string) *bufio.Scanner {
	t.Helper()
	ctx, cancelar := context.WithCancel(context.Background())
	t.Cleanup(cancelar)
	req, _ := http.NewRequestWithContext(ctx, "GET", a.url+caminho, nil)
	req.Header.Set("Authorization", "Bearer "+a.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	sc := bufio.NewScanner(resp.Body)
	sc.Scan()
	return sc
}

func TestPermissaoDecididaNoPainel(t *testing.T) {
	a := novoAmbiente(t, nil)
	pedido := map[string]any{"session_id": "p", "hook_event_name": "PermissionRequest", "tool_name": "Bash",
		"tool_input": map[string]any{"command": "rm -rf build"}}
	if s := a.responder(pedido); s != nil {
		t.Fatalf("sem o painel a responder, o pedido devia seguir para o terminal: %v", s)
	}
	a.postJSON("/v1/painel", map[string]bool{"responder_permissoes": true})
	if s := a.responder(pedido); s != nil {
		t.Fatalf("sem painel aberto, o pedido devia seguir para o terminal: %v", s)
	}
	abrirSSE(t, a, "/v1/ao-vivo")

	resultado := make(chan map[string]any, 1)
	go func() { resultado <- a.responder(pedido) }()
	var id float64
	for prazo := time.Now().Add(3 * time.Second); id == 0 && time.Now().Before(prazo); time.Sleep(20 * time.Millisecond) {
		req, _ := http.NewRequest("GET", a.url+"/v1/painel", nil)
		req.Header.Set("Authorization", "Bearer "+a.token)
		resp, _ := http.DefaultClient.Do(req)
		var e struct{ Permissoes []map[string]any }
		json.NewDecoder(resp.Body).Decode(&e)
		resp.Body.Close()
		if len(e.Permissoes) == 1 && e.Permissoes[0]["resumo"] == "rm -rf build" {
			id = e.Permissoes[0]["id"].(float64)
		}
	}
	if id == 0 {
		t.Fatal("a permissão não apareceu no painel")
	}
	if codigo, _ := a.postJSON(fmt.Sprintf("/v1/permissoes/%d", int(id)), map[string]string{"decisao": "allow"}); codigo != 200 {
		t.Fatalf("decisão devia ser aceite, deu %d", codigo)
	}
	saida := <-resultado
	d := saida["hookSpecificOutput"].(map[string]any)["decision"].(map[string]any)
	if d["behavior"] != "allow" {
		t.Fatalf("decisão inesperada: %v", saida)
	}
	if codigo, _ := a.postJSON(fmt.Sprintf("/v1/permissoes/%d", int(id)), map[string]string{"decisao": "deny"}); codigo != http.StatusGone {
		t.Fatalf("segunda decisão devia dar 410, deu %d", codigo)
	}
}

func TestEntregaPelaExtensao(t *testing.T) {
	a := novoAmbiente(t, nil)
	sc := abrirSSE(t, a, "/v1/extensao/eventos")
	ev, _ := json.Marshal(map[string]any{"session_id": "x", "hook_event_name": "Stop"})
	corpo, _ := json.Marshal(map[string]any{"agente": "claude", "recebido_em": "2026-10-07T12:00:00Z", "ppid": os.Getpid(),
		"terminal": map[string]string{"TERM_PROGRAM": "vscode"}, "evento": json.RawMessage(ev)})
	a.enviarBruto(corpo, a.token)
	a.esperar()

	pedidos := make(chan map[string]any, 1)
	go func() {
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), "data: ") {
				var p map[string]any
				json.Unmarshal([]byte(strings.TrimPrefix(sc.Text(), "data: ")), &p)
				pedidos <- p
				return
			}
		}
	}()
	resposta := make(chan map[string]any, 1)
	go func() {
		_, m := a.postJSON("/v1/sessoes/x/mensagens", map[string]string{"texto": "corre\nos testes"})
		resposta <- m
	}()
	p := <-pedidos
	pids := p["pids"].([]any)
	if p["acao"] != "enviar" || p["texto"] != "corre os testes" || int(pids[0].(float64)) != os.Getpid() {
		t.Fatalf("pedido à extensão inesperado: %v", p)
	}
	a.postJSON("/v1/extensao/resposta", map[string]any{"pedido": p["id"], "ok": true, "workspace": "loja"})
	m := <-resposta
	if m["estado"] != quadro.MensagemEntregue || m["caminho"] != "terminal-ide" {
		t.Fatalf("mensagem devia ser entregue pela extensão: %v", m)
	}
}
