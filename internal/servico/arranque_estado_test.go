package servico

import (
	"encoding/json"
	"testing"

	"github.com/GeorgeMyller/towerman/internal/quadro"
)

// evArranque envia um evento com campos extra (source, prompt, tool_name) ao processador.
func evArranque(t *testing.T, p *Processador, m map[string]any) {
	t.Helper()
	j, _ := json.Marshal(m)
	if _, err := p.Processar(envelopeDe(j)); err != nil {
		t.Fatal(err)
	}
}

// Uma sessão terminada e reaberta com claude --resume («Focar») espera pelo primeiro pedido:
// fica parada, e uma mensagem do telemóvel não vai para a fila de um Stop que nunca chega.
func TestRetomadaInterativaFicaParada(t *testing.T) {
	a := novoAmbiente(t, nil)
	p := a.srv.proc
	evArranque(t, p, map[string]any{"session_id": "r", "hook_event_name": "SessionStart", "source": "startup"})
	evArranque(t, p, map[string]any{"session_id": "r", "hook_event_name": "UserPromptSubmit", "prompt": "olá"})
	p.mustProcessar(t, "r", "Stop", "")
	p.mustProcessar(t, "r", "SessionEnd", "")
	if s := a.sessao("r"); s.Estado != quadro.Terminada {
		t.Fatalf("estado = %q, devia estar terminada", s.Estado)
	}

	evArranque(t, p, map[string]any{"session_id": "r", "hook_event_name": "SessionStart", "source": "resume"})
	s := a.sessao("r")
	if s.Estado != quadro.Parada {
		t.Fatalf("retomada sem pedido: estado = %q, devia estar parada", s.Estado)
	}
	if s.TerminadaEm != "" {
		t.Fatalf("terminada_em devia ficar vazio, está %q", s.TerminadaEm)
	}
	if caminho, _, _ := a.srv.encaminhar(s, "do telemóvel"); caminho == "stop" {
		t.Fatal("uma sessão parada não devia pôr a mensagem à espera do Stop")
	}

	evArranque(t, p, map[string]any{"session_id": "r", "hook_event_name": "UserPromptSubmit", "prompt": "continua"})
	if s := a.sessao("r"); s.Estado != quadro.Ativa {
		t.Fatalf("depois do pedido: estado = %q, devia estar ativa", s.Estado)
	}
}

// Uma sessão nova (ou de um agente que não diz a origem) também começa parada, e passa a ativa
// na primeira ferramenta.
func TestArranqueSemPedidoFicaParada(t *testing.T) {
	a := novoAmbiente(t, nil)
	p := a.srv.proc
	for _, origem := range []string{"startup", "clear", ""} {
		id := "n-" + origem
		m := map[string]any{"session_id": id, "hook_event_name": "SessionStart"}
		if origem != "" {
			m["source"] = origem
		}
		evArranque(t, p, m)
		if s := a.sessao(id); s.Estado != quadro.Parada {
			t.Fatalf("source %q: estado = %q, devia estar parada", origem, s.Estado)
		}
		evArranque(t, p, map[string]any{"session_id": id, "hook_event_name": "PreToolUse", "tool_name": "Bash",
			"tool_input": map[string]any{"command": "ls"}})
		if s := a.sessao(id); s.Estado != quadro.Ativa {
			t.Fatalf("source %q depois da ferramenta: estado = %q, devia estar ativa", origem, s.Estado)
		}
	}
}

// A compactação automática acontece a meio de um passo: o estado fica como estava.
func TestCompactacaoNaoMudaOEstado(t *testing.T) {
	a := novoAmbiente(t, nil)
	p := a.srv.proc
	evArranque(t, p, map[string]any{"session_id": "c", "hook_event_name": "UserPromptSubmit", "prompt": "trabalha"})
	evArranque(t, p, map[string]any{"session_id": "c", "hook_event_name": "SessionStart", "source": "compact"})
	if s := a.sessao("c"); s.Estado != quadro.Ativa {
		t.Fatalf("compact a meio do passo: estado = %q, devia continuar ativa", s.Estado)
	}

	p.mustProcessar(t, "c", "Stop", "")
	evArranque(t, p, map[string]any{"session_id": "c", "hook_event_name": "SessionStart", "source": "compact"})
	if s := a.sessao("c"); s.Estado != quadro.Parada {
		t.Fatalf("/compact com a sessão parada: estado = %q, devia continuar parada", s.Estado)
	}
}
