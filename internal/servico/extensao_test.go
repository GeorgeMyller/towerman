package servico

import (
	"math"
	"testing"
	"time"
)

// janelaFalsa liga ao hub uma janela do IDE de mentira. Cada pedido recebido segue para o canal
// devolvido; responder diz o que a janela responde (nil: fica calada).
func janelaFalsa(t *testing.T, h *hubExtensao, responder func(p pedidoExtensao) *respostaExtensao) <-chan pedidoExtensao {
	t.Helper()
	c := make(chan pedidoExtensao, 8)
	recebidos := make(chan pedidoExtensao, 32)
	h.mu.Lock()
	h.clientes[c] = struct{}{}
	h.mu.Unlock()
	fim := make(chan struct{})
	go func() {
		for {
			select {
			case p := <-c:
				recebidos <- p
				if responder != nil {
					if r := responder(p); r != nil {
						r.Pedido = p.ID
						h.responder(*r)
					}
				}
			case <-fim:
				return
			}
		}
	}()
	t.Cleanup(func() {
		h.mu.Lock()
		delete(h.clientes, c)
		h.mu.Unlock()
		close(fim)
	})
	return recebidos
}

// candidata responde à candidatura com o alcance dado e à execução com ok, se for a escolhida.
func candidata(janela string, alcance float64) func(p pedidoExtensao) *respostaExtensao {
	return func(p pedidoExtensao) *respostaExtensao {
		switch p.Fase {
		case faseCandidatar:
			return &respostaExtensao{Ok: true, Janela: janela, Alcance: &alcance}
		case faseExecutar:
			if p.Janela == janela {
				return &respostaExtensao{Ok: true, Janela: janela, Workspace: janela}
			}
		}
		return nil
	}
}

// fases devolve as fases dos pedidos recebidos até agora.
func fases(c <-chan pedidoExtensao) []pedidoExtensao {
	var r []pedidoExtensao
	for {
		select {
		case p := <-c:
			r = append(r, p)
		default:
			return r
		}
	}
}

func TestPedirEntregaEscolheOMaiorAlcance(t *testing.T) {
	h := novoHub()
	curta := janelaFalsa(t, h, candidata("curta", 5))
	longa := janelaFalsa(t, h, candidata("longa", 12))
	resp, ok := h.pedirEntrega("focar", []int{1}, "/w/projeto/sub", "")
	if !ok || resp.Janela != "longa" {
		t.Fatalf("devia executar na janela de maior alcance: %+v %v", resp, ok)
	}
	time.Sleep(20 * time.Millisecond)
	pc, pl := fases(curta), fases(longa)
	for _, p := range append(pc, pl...) {
		if p.Fase == faseExecutar && p.Janela != "longa" {
			t.Fatalf("a execução só pode nomear a janela escolhida: %+v", p)
		}
	}
	if len(pl) != 2 || pl[1].Fase != faseExecutar || pl[1].Janela != "longa" || pl[1].ID != pl[0].ID {
		t.Fatalf("a escolhida recebe executar com o mesmo id: %+v", pl)
	}
	if pl[0].Cwd != "/w/projeto/sub" || pl[0].Acao != "focar" {
		t.Fatalf("a candidatura leva a ação e o cwd: %+v", pl[0])
	}
}

// Em «enviar», só a janela com o terminal da sessão executa: a dona da pasta (alcance parcial,
// candidatura de uma extensão 0.9.0) nunca recebe o texto no terminal ativo (R-01).
func TestPedirEntregaEnviarSoNoTerminalDaSessao(t *testing.T) {
	h := novoHub()
	dona := janelaFalsa(t, h, candidata("dona", 12))
	resp, ok := h.pedirEntrega("enviar", []int{1}, "/w/projeto", "CONTINUAR")
	if ok || resp.Erro != semTerminal {
		t.Fatalf("sem o terminal da sessão, «enviar» devia falhar com motivo claro: %+v %v", resp, ok)
	}
	time.Sleep(20 * time.Millisecond)
	for _, p := range fases(dona) {
		if p.Fase == faseExecutar {
			t.Fatalf("a dona da pasta não pode executar «enviar»: %+v", p)
		}
	}

	h2 := novoHub()
	janelaFalsa(t, h2, candidata("dona", 12))
	janelaFalsa(t, h2, candidata("terminal", math.MaxFloat64))
	if resp, ok := h2.pedirEntrega("enviar", []int{1}, "/w/projeto", "CONTINUAR"); !ok || resp.Janela != "terminal" {
		t.Fatalf("a janela com o terminal da sessão devia executar: %+v %v", resp, ok)
	}
}

func TestPedirEntregaAlcanceMaximoNaoEspera(t *testing.T) {
	h := novoHub()
	janelaFalsa(t, h, candidata("dona", math.MaxFloat64))
	inicio := time.Now()
	if _, ok := h.pedirEntrega("enviar", []int{1}, "/w", "1"); !ok {
		t.Fatal("a janela com o terminal devia executar")
	}
	if d := time.Since(inicio); d >= recolhaCandidaturas {
		t.Fatalf("com alcance máximo, a recolha devia acabar logo: %v", d)
	}
}

func TestPedirEntregaSemCandidatura(t *testing.T) {
	h := novoHub()
	calada := janelaFalsa(t, h, nil)
	resp, ok := h.pedirEntrega("focar", []int{1}, "/w", "")
	if ok || resp.Erro == "" {
		t.Fatalf("sem candidatura devia falhar com motivo: %+v %v", resp, ok)
	}
	for _, p := range fases(calada) {
		if p.Fase == faseExecutar {
			t.Fatal("sem candidatura, nada a executar")
		}
	}
}

// A extensão antiga ignora a fase, age logo e responde sem alcance: conta como execução feita.
func TestPedirEntregaExtensaoAntiga(t *testing.T) {
	h := novoHub()
	antiga := janelaFalsa(t, h, func(p pedidoExtensao) *respostaExtensao {
		return &respostaExtensao{Ok: true, Workspace: "loja"}
	})
	resp, ok := h.pedirEntrega("enviar", []int{1}, "/w", "1")
	if !ok || resp.Workspace != "loja" {
		t.Fatalf("a resposta antiga devia valer como entrega: %+v %v", resp, ok)
	}
	time.Sleep(20 * time.Millisecond)
	for _, p := range fases(antiga) {
		if p.Fase == faseExecutar {
			t.Fatal("a resposta sem alcance não gera executar")
		}
	}
}
