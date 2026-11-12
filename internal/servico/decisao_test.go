package servico

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/GeorgeMyller/towerman/internal/quadro"
)

const fimComMenu = "Qual caminho prefere?\n\n1. Sincronizar agora\n2. Aguardar\n\nDigite 1 ou 2."

func (a *ambiente) decisaoAberta(id string) (quadro.Decisao, bool) {
	a.t.Helper()
	a.esperar()
	d, ok, err := a.srv.Quadro().DecisaoAberta(id)
	if err != nil {
		a.t.Fatal(err)
	}
	return d, ok
}

func TestStopAbreEFechaDecisao(t *testing.T) {
	a := novoAmbiente(t, nil)
	a.enviar(map[string]any{"session_id": "s", "hook_event_name": "UserPromptSubmit", "prompt": "sincroniza"})
	a.responder(map[string]any{"session_id": "s", "hook_event_name": "Stop", "last_assistant_message": fimComMenu})

	d, ok := a.decisaoAberta("s")
	if !ok || d.Padrao != "menu" || len(d.Opcoes) != 2 || d.Opcoes[1].Envio != "2" {
		t.Fatalf("decisão depois do Stop: %+v %v", d, ok)
	}

	// A resposta no terminal fecha a decisão.
	a.enviar(map[string]any{"session_id": "s", "hook_event_name": "UserPromptSubmit", "prompt": "2"})
	if _, ok := a.decisaoAberta("s"); ok {
		t.Fatal("o UserPromptSubmit devia fechar a decisão")
	}

	// Um Stop sem pergunta não abre nada; o SessionEnd fecha uma aberta.
	a.responder(map[string]any{"session_id": "s", "hook_event_name": "Stop", "last_assistant_message": "Feito. Tudo verde."})
	if _, ok := a.decisaoAberta("s"); ok {
		t.Fatal("um fim sem pergunta não devia abrir decisão")
	}
	a.responder(map[string]any{"session_id": "s", "hook_event_name": "Stop", "last_assistant_message": "Posso fazer o commit?"})
	if d, ok := a.decisaoAberta("s"); !ok || d.Padrao != "sim_nao" {
		t.Fatalf("pergunta fechada: %+v %v", d, ok)
	}
	a.enviar(map[string]any{"session_id": "s", "hook_event_name": "SessionEnd"})
	if _, ok := a.decisaoAberta("s"); ok {
		t.Fatal("o SessionEnd devia fechar a decisão")
	}
}

func TestStopComMensagemEmFilaNaoAbreDecisao(t *testing.T) {
	a := novoAmbiente(t, nil)
	a.enviar(map[string]any{"session_id": "s", "hook_event_name": "UserPromptSubmit", "prompt": "trabalha"})
	a.esperar()
	if c, _ := a.postJSON("/v1/sessoes/s/mensagens", map[string]any{"texto": "continua"}); c != 200 {
		t.Fatalf("enviar mensagem: %d", c)
	}
	saida := a.responder(map[string]any{"session_id": "s", "hook_event_name": "Stop", "last_assistant_message": fimComMenu})
	if saida == nil || saida["decision"] != "block" {
		t.Fatalf("o Stop devia entregar a mensagem em fila: %v", saida)
	}
	if _, ok := a.decisaoAberta("s"); ok {
		t.Fatal("com mensagem entregue no Stop não nasce decisão")
	}
}

func TestDecisoesDesligadas(t *testing.T) {
	a := novoAmbiente(t, nil)
	if err := os.WriteFile(filepath.Join(a.home, "config.json"), []byte(`{"decisoes": false}`), 0o600); err != nil {
		t.Fatal(err)
	}
	a.enviar(map[string]any{"session_id": "s", "hook_event_name": "UserPromptSubmit", "prompt": "x"})
	a.responder(map[string]any{"session_id": "s", "hook_event_name": "Stop", "last_assistant_message": fimComMenu})
	if _, ok := a.decisaoAberta("s"); ok {
		t.Fatal("com a preferência desligada não nasce decisão")
	}
}

func TestStopLeTranscricaoSemUltimaResposta(t *testing.T) {
	a := novoAmbiente(t, nil)
	tr := escreverTranscricao(t, `Digite **CONTINUAR** para prosseguir.`)
	a.enviar(map[string]any{"session_id": "s", "hook_event_name": "UserPromptSubmit", "prompt": "x", "transcript_path": tr})
	a.responder(map[string]any{"session_id": "s", "hook_event_name": "Stop", "transcript_path": tr})
	if d, ok := a.decisaoAberta("s"); !ok || d.Padrao != "comando" || d.Opcoes[0].Envio != "CONTINUAR" {
		t.Fatalf("decisão pela transcrição: %+v %v", d, ok)
	}
}
