package servico

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/GeorgeMyller/towerman/internal/relay"
)

func (a *ambiente) parearAparelho() string {
	a.t.Helper()
	codigo, _ := a.srv.aparelhos.novoCodigo(time.Now())
	_, r := a.rede("POST", "/v1/pair", "", map[string]string{"code": codigo, "device": "relógio"})
	token, _ := r["token"].(string)
	if token == "" {
		a.t.Fatalf("pareamento falhou: %v", r)
	}
	return token
}

// enviarNoTmux manda o evento como se a sessão corresse num painel do tmux.
func (a *ambiente) enviarNoTmux(evento map[string]any) {
	a.t.Helper()
	contador++
	b := envelope(evento, fmt.Sprintf("2026-10-07T11:00:00.%09dZ", contador))
	corpo := strings.Replace(string(b), `"TERM_PROGRAM":"vscode"`, `"TMUX_PANE":"%9"`, 1)
	if c := a.enviarBruto([]byte(corpo), a.token); c != http.StatusOK && c != http.StatusAccepted {
		a.t.Fatalf("evento: %d", c)
	}
}

// tmuxFalso põe no PATH um tmux que só regista os argumentos.
func tmuxFalso(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("tmux falso em sh")
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "tmux"), []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \""+dir+"/args\"\n"), 0o755)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return filepath.Join(dir, "args")
}

func esperaDe(estado map[string]any, id string) map[string]any {
	agentes, _ := estado["agents"].([]any)
	for _, x := range agentes {
		ag, _ := x.(map[string]any)
		if ag["id"] == id {
			w, _ := ag["waiting"].(map[string]any)
			return w
		}
	}
	return nil
}

func TestDecisaoNoV1SemEntrega(t *testing.T) {
	a := novoAmbiente(t, nil)
	token := a.parearAparelho()
	a.enviar(map[string]any{"session_id": "s", "hook_event_name": "UserPromptSubmit", "prompt": "x"})
	a.responder(map[string]any{"session_id": "s", "hook_event_name": "Stop", "last_assistant_message": fimComMenu})
	a.esperar()

	_, estado := a.rede("GET", "/v1/state", token, nil)
	w := esperaDe(estado, "s")
	if w == nil || w["kind"] != "decision" || w["context"] != semEntrega {
		t.Fatalf("waiting da decisão: %v", w)
	}
	opcoes, _ := w["options"].([]any)
	if len(opcoes) != 2 || opcoes[0].(map[string]any)["disabled"] != true || opcoes[1].(map[string]any)["key"] != "d:1" {
		t.Fatalf("opções: %v", opcoes)
	}
	id := w["id"].(string)

	// Terminal sem entrega: a resposta é recusada e a decisão continua aberta (RF-19).
	if c, r := a.rede("POST", "/v1/agents/s/answer", token, map[string]string{"promptId": id, "key": "d:0"}); c != http.StatusConflict || r["error"] != semEntrega {
		t.Fatalf("resposta sem entrega: %d %v", c, r)
	}
	if _, ok := a.decisaoAberta("s"); !ok {
		t.Fatal("a decisão devia continuar aberta")
	}
	// Descartar não precisa de entrega.
	if c, _ := a.rede("POST", "/v1/agents/s/answer", token, map[string]string{"promptId": id, "key": "dismiss"}); c != http.StatusNoContent {
		t.Fatalf("descartar: %d", c)
	}
	if c, r := a.rede("POST", "/v1/agents/s/answer", token, map[string]string{"promptId": id, "key": "dismiss"}); c != http.StatusConflict || r["error"] != decisaoRespondida {
		t.Fatalf("segundo descarte: %d %v", c, r)
	}
	a.esperar()
	if _, estado = a.rede("GET", "/v1/state", token, nil); esperaDe(estado, "s") != nil {
		t.Fatal("a decisão descartada não devia aparecer")
	}
}

func TestDecisaoRespondidaPeloRelogio(t *testing.T) {
	args := tmuxFalso(t)
	a := novoAmbiente(t, nil)
	token := a.parearAparelho()
	a.enviarNoTmux(map[string]any{"session_id": "s", "hook_event_name": "UserPromptSubmit", "prompt": "x"})
	a.enviarNoTmux(map[string]any{"session_id": "s", "hook_event_name": "Stop", "last_assistant_message": fimComMenu})
	a.esperar()

	_, estado := a.rede("GET", "/v1/state", token, nil)
	w := esperaDe(estado, "s")
	if w == nil || w["context"] != "" {
		t.Fatalf("com tmux a decisão devia estar ativa: %v", w)
	}
	id := w["id"].(string)
	if c, _ := a.rede("POST", "/v1/agents/s/answer", token, map[string]string{"promptId": id, "key": "d:9"}); c != http.StatusBadRequest {
		t.Fatalf("opção inválida devia dar 400, deu %d", c)
	}
	if c, r := a.rede("POST", "/v1/agents/s/answer", token, map[string]string{"promptId": id, "key": "d:1"}); c != http.StatusNoContent {
		t.Fatalf("resposta: %d %v", c, r)
	}
	b, _ := os.ReadFile(args)
	if !strings.Contains(string(b), "send-keys -t %9 -l 2") {
		t.Fatalf("o tmux devia receber «2»: %q", b)
	}
	if _, ok := a.decisaoAberta("s"); ok {
		t.Fatal("a decisão respondida devia fechar")
	}
	if c, _ := a.rede("POST", "/v1/agents/s/answer", token, map[string]string{"promptId": id, "key": "d:0"}); c != http.StatusConflict {
		t.Fatalf("decisão fechada devia dar 409, deu %d", c)
	}
}

func TestDuasRespostasSimultaneas(t *testing.T) {
	tmuxFalso(t)
	a := novoAmbiente(t, nil)
	token := a.parearAparelho()
	a.enviarNoTmux(map[string]any{"session_id": "s", "hook_event_name": "UserPromptSubmit", "prompt": "x"})
	a.enviarNoTmux(map[string]any{"session_id": "s", "hook_event_name": "Stop", "last_assistant_message": "Posso fazer o commit?"})
	d, ok := a.decisaoAberta("s")
	if !ok {
		t.Fatal("sem decisão")
	}
	id := fmt.Sprintf("d%d", d.ID)
	var mu sync.Mutex
	codigos := map[int]int{}
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(chave string) {
			defer wg.Done()
			c, _ := a.rede("POST", "/v1/agents/s/answer", token, map[string]string{"promptId": id, "key": chave})
			mu.Lock()
			codigos[c]++
			mu.Unlock()
		}(fmt.Sprintf("d:%d", i))
	}
	wg.Wait()
	if codigos[http.StatusNoContent] != 1 || codigos[http.StatusConflict] != 1 {
		t.Fatalf("esperava um 204 e um 409: %v", codigos)
	}
}

func TestInputFechaDecisao(t *testing.T) {
	tmuxFalso(t)
	a := novoAmbiente(t, nil)
	token := a.parearAparelho()
	a.enviarNoTmux(map[string]any{"session_id": "s", "hook_event_name": "UserPromptSubmit", "prompt": "x"})
	a.enviarNoTmux(map[string]any{"session_id": "s", "hook_event_name": "Stop", "last_assistant_message": "Qual nome prefere para a branch?"})
	if d, ok := a.decisaoAberta("s"); !ok || d.Padrao != "aberta" {
		t.Fatalf("pergunta aberta: %+v %v", d, ok)
	}
	if c, r := a.rede("POST", "/v1/agents/s/input", token, map[string]string{"text": "feat/x"}); c != 200 || r["state"] != "entregue" {
		t.Fatalf("input: %d %v", c, r)
	}
	if _, ok := a.decisaoAberta("s"); ok {
		t.Fatal("o texto entregue devia responder à decisão")
	}
}

// O push de decisão vai cifrado para cada aparelho: só o aparelho, com a sua chave, lê a pergunta.
func TestPushDeDecisaoCifradoPorAparelho(t *testing.T) {
	a := novoAmbiente(t, nil)
	priv, pub, err := relay.GerarParX25519()
	if err != nil {
		t.Fatal(err)
	}
	codigo, _ := a.srv.aparelhos.novoCodigo(time.Now())
	if _, _, err := a.srv.aparelhos.parear(codigo, "iPhone", pub, "token-apns", time.Now()); err != nil {
		t.Fatal(err)
	}
	espera := map[string]any{"kind": "decision", "detail": "Posso fazer o commit?",
		"options": []map[string]any{{"key": "d:0", "label": "Sim"}, {"key": "d:1", "label": "Não"}}}
	ps := a.srv.pushesDecisao("s · mac", "s", "d7", espera)
	if len(ps) != 1 {
		t.Fatalf("esperava 1 push, veio %d", len(ps))
	}
	p := ps[0]
	if p.Token != "token-apns" || p.Categoria != "decisao" || strings.Contains(p.Corpo, "commit") || p.Enc == "" {
		t.Fatalf("push: %+v", p)
	}
	chave, err := relay.DerivarChaveSimetrica(priv, a.srv.aparelhos.chavePub, p.Server)
	if err != nil {
		t.Fatal(err)
	}
	claro, err := relay.Abrir(chave, p.Enc, []byte(p.Device))
	if err != nil {
		t.Fatalf("o aparelho não consegue abrir: %v", err)
	}
	if !strings.Contains(string(claro), "Posso fazer o commit?") || !strings.Contains(string(claro), `"d:1"`) {
		t.Fatalf("conteúdo decifrado: %s", claro)
	}
}

// O deck web vê as decisões no /v1/painel e responde pela rota local.
func TestDecisaoNoPainelLocal(t *testing.T) {
	a := novoAmbiente(t, nil)
	a.enviar(map[string]any{"session_id": "s", "hook_event_name": "UserPromptSubmit", "prompt": "x"})
	a.responder(map[string]any{"session_id": "s", "hook_event_name": "Stop", "last_assistant_message": fimComMenu})
	a.esperar()
	var painel struct {
		Decisoes []map[string]any `json:"decisoes"`
	}
	a.obter("/v1/painel", &painel)
	if len(painel.Decisoes) != 1 || painel.Decisoes[0]["session_id"] != "s" || painel.Decisoes[0]["kind"] != "decision" {
		t.Fatalf("decisões no painel: %v", painel.Decisoes)
	}
	id := painel.Decisoes[0]["id"].(string)
	if c, r := a.postJSON("/v1/sessoes/s/decisao", map[string]string{"id": id, "chave": "d:0"}); c != http.StatusConflict || r["erro"] != semEntrega {
		t.Fatalf("sem entrega: %d %v", c, r)
	}
	if c, _ := a.postJSON("/v1/sessoes/s/decisao", map[string]string{"id": id, "chave": "dismiss"}); c != http.StatusNoContent {
		t.Fatalf("descartar: %d", c)
	}
	a.obter("/v1/painel", &painel)
	if len(painel.Decisoes) != 0 {
		t.Fatalf("a decisão descartada continua no painel: %v", painel.Decisoes)
	}
}
