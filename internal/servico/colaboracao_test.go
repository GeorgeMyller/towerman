package servico

import (
	"math"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/GeorgeMyller/towerman/internal/hook"
	"github.com/GeorgeMyller/towerman/internal/quadro"
)

// sessaoIDE deixa parada uma sessão do Antigravity IDE: VSCODE_PID sem TERM_PROGRAM.
func sessaoIDE(t *testing.T, a *ambiente, id string, term hook.Terminal) quadro.Sessao {
	t.Helper()
	evAgente(t, a.srv.proc, "antigravity", term, os.Getpid(), map[string]any{"session_id": id, "hook_event_name": "UserPromptSubmit", "prompt": "olá",
		"cwd": t.TempDir()})
	evAgente(t, a.srv.proc, "antigravity", term, os.Getpid(), map[string]any{"session_id": id, "hook_event_name": "Stop", "last_assistant_message": "Feito."})
	s := a.sessao(id)
	if s.Estado != quadro.Parada {
		t.Fatalf("a sessão %s devia estar parada: %q", id, s.Estado)
	}
	return s
}

// Sem extensão ligada, uma mensagem livre para o Antigravity fica para o Stop; uma resposta a uma
// decisão nunca fica na fila.
func TestAntigravitySemEntrega(t *testing.T) {
	a := novoAmbiente(t, nil)
	s := sessaoIDE(t, a, "g", hook.Terminal{VscodePid: strconv.Itoa(os.Getpid())})
	if ok, motivo := a.srv.entregavel(s); ok || motivo != semEntrega {
		t.Fatalf("sem extensão, não há entrega: %v %q", ok, motivo)
	}

	_, m := a.postJSON("/v1/sessoes/g/mensagens", map[string]string{"texto": "continua"})
	if m["estado"] != quadro.MensagemPendente || m["caminho"] != "fila" {
		t.Fatalf("a mensagem livre devia ficar pendente para o Stop: %v", m)
	}

	if err := a.srv.entregarResposta(httptest.NewRequest("POST", "/", nil), s, "1"); err == nil {
		t.Fatal("a resposta a uma decisão sem entrega devia falhar")
	}
	ms, _ := a.srv.Quadro().MensagensRecentes("g", 1)
	if len(ms) != 1 || ms[0].Estado != quadro.MensagemFalhada {
		t.Fatalf("a resposta a uma decisão nunca fica pendente: %+v", ms)
	}
}

// Com a extensão ligada, o terminal do IDE aceita texto mesmo sem TERM_PROGRAM=vscode.
func TestAntigravityPelaExtensao(t *testing.T) {
	a := novoAmbiente(t, nil)
	s := sessaoIDE(t, a, "g", hook.Terminal{VscodePid: strconv.Itoa(os.Getpid())})
	janela := janelaFalsa(t, a.srv.ext, candidata("w", math.MaxFloat64))
	if ok, motivo := a.srv.entregavel(s); !ok {
		t.Fatalf("com a extensão ligada, devia haver entrega: %q", motivo)
	}
	caminho, estado, _ := a.srv.entregarNoTerminal(s, "CONTINUAR")
	if caminho != "terminal-ide" || estado != quadro.MensagemEntregue {
		t.Fatalf("devia entregar pela extensão: %s %s", caminho, estado)
	}
	ps := fases(janela)
	if len(ps) == 0 || ps[0].Cwd == "" || ps[0].Cwd != s.Cwd || ps[0].Texto != "CONTINUAR" {
		t.Fatalf("o pedido à extensão leva a pasta e o texto: %+v", ps)
	}
}

// Com a extensão ligada mas sem o terminal da sessão (agente do painel do IDE), a mensagem não vai
// ao terminal ativo (R-01): vai ao painel do agente da janela dona da pasta.
func TestAntigravitySemTerminalVaiAoPainel(t *testing.T) {
	a := novoAmbiente(t, nil)
	s := sessaoIDE(t, a, "g", hook.Terminal{VscodePid: strconv.Itoa(os.Getpid())})
	janela := janelaFalsa(t, a.srv.ext, candidata("dona", 12))
	caminho, estado, motivo := a.srv.entregarNoTerminal(s, "CONTINUAR")
	if caminho != "painel-ide" || estado != quadro.MensagemEntregue {
		t.Fatalf("devia entregar no painel do agente: %s %s %q", caminho, estado, motivo)
	}
	time.Sleep(20 * time.Millisecond)
	painel := false
	for _, p := range fases(janela) {
		if p.Fase == faseExecutar && p.Acao == "enviar" {
			t.Fatalf("a janela dona da pasta não pode receber o texto no terminal: %+v", p)
		}
		if p.Fase == faseExecutar && p.Acao == "painel" && p.Texto == "CONTINUAR" && p.Cwd == s.Cwd {
			painel = true
		}
	}
	if !painel {
		t.Fatal("o pedido «painel» devia levar o texto e a pasta da sessão")
	}
}

// Uma extensão sem a ação «painel» (até 0.9.3) não responde: a mensagem fica na fila do Stop,
// com o motivo dito.
func TestAntigravitySemPainelFicaNaFila(t *testing.T) {
	a := novoAmbiente(t, nil)
	s := sessaoIDE(t, a, "g", hook.Terminal{VscodePid: strconv.Itoa(os.Getpid())})
	antiga := candidata("dona", 12)
	janelaFalsa(t, a.srv.ext, func(p pedidoExtensao) *respostaExtensao {
		if p.Acao == "painel" {
			return nil
		}
		return antiga(p)
	})
	caminho, estado, motivo := a.srv.entregarNoTerminal(s, "CONTINUAR")
	if caminho != "fila" || estado != quadro.MensagemPendente || !strings.Contains(motivo, "não encontrou o terminal desta sessão") {
		t.Fatalf("sem painel, devia ficar na fila com motivo claro: %s %s %q", caminho, estado, motivo)
	}
}

// O tmux continua antes da extensão: com TMUX_PANE, a extensão nem é chamada.
func TestTmuxAntesDaExtensao(t *testing.T) {
	a := novoAmbiente(t, nil)
	s := sessaoIDE(t, a, "t", hook.Terminal{TmuxPane: "%towerman-teste-inexistente", VscodePid: "1"})
	janela := janelaFalsa(t, a.srv.ext, candidata("w", math.MaxFloat64))
	if caminho, _, _ := a.srv.entregarNoTerminal(s, "1"); caminho == "terminal-ide" {
		t.Fatal("com TMUX_PANE, a entrega não vai pela extensão")
	}
	time.Sleep(20 * time.Millisecond)
	if ps := fases(janela); len(ps) != 0 {
		t.Fatalf("a extensão não devia receber pedidos: %+v", ps)
	}
}
