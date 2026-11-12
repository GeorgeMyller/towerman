package servico

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sync"
	"time"
)

// pedidoExtensao vai para todas as janelas do IDE ligadas; responde só a que tem o terminal.
type pedidoExtensao struct {
	ID    string `json:"id"`
	Acao  string `json:"acao"`
	Pids  []int  `json:"pids"`
	Texto string `json:"texto,omitempty"`
	Cwd   string `json:"cwd,omitempty"`
	// Fase (enviar e focar): «candidatar» pergunta o alcance de cada janela, «executar» manda agir
	// só a Janela escolhida. Sem fase, a janela age e responde logo (protocolo antigo).
	Fase   string `json:"fase,omitempty"`
	Janela string `json:"janela,omitempty"`
}

type respostaExtensao struct {
	Pedido    string `json:"pedido"`
	Ok        bool   `json:"ok"`
	Workspace string `json:"workspace"`
	// Pasta é o ficheiro do workspace ou a pasta aberta na janela que agiu; vazia numa extensão
	// antiga.
	Pasta string `json:"pasta,omitempty"`
	Erro  string `json:"erro"`
	// Focada responde ao pedido «foco»: a janela da sessão tem o foco do sistema.
	Focada bool `json:"focada"`
	// Janela identifica a janela que respondeu; Alcance vem na candidatura (máximo: tem o
	// terminal da sessão; senão, o comprimento da pasta aberta mais próxima). Uma extensão
	// antiga não manda alcance: já agiu.
	Janela  string   `json:"janela,omitempty"`
	Alcance *float64 `json:"alcance,omitempty"`
}

const (
	faseCandidatar = "candidatar"
	faseExecutar   = "executar"
)

// recolhaCandidaturas é quanto o serviço espera pelas candidaturas das janelas.
var recolhaCandidaturas = 150 * time.Millisecond

type hubExtensao struct {
	mu        sync.Mutex
	clientes  map[chan pedidoExtensao]struct{}
	respostas map[string]chan respostaExtensao
}

func novoHub() *hubExtensao {
	return &hubExtensao{clientes: map[chan pedidoExtensao]struct{}{}, respostas: map[string]chan respostaExtensao{}}
}

func (h *hubExtensao) ligadas() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clientes)
}

// pedirAbrir pede à janela do IDE que tem a pasta aberta um terminal novo a correr comando.
func (h *hubExtensao) pedirAbrir(cwd, comando string, espera time.Duration) (respostaExtensao, bool) {
	return h.enviarPedido(pedidoExtensao{Acao: "abrir", Cwd: cwd, Texto: comando}, espera)
}

func (h *hubExtensao) enviarPedido(p pedidoExtensao, espera time.Duration) (respostaExtensao, bool) {
	resp, fim := h.abrirPedido(&p)
	defer fim()
	h.difundir(p)
	select {
	case r := <-resp:
		return r, r.Ok
	case <-time.After(espera):
		return respostaExtensao{Erro: semJanela}, false
	}
}

const (
	semJanela   = "nenhuma janela do IDE respondeu"
	semTerminal = "nenhuma janela do IDE tem o terminal desta sessão"
)

// abrirPedido dá um id ao pedido e regista o canal das respostas; fim apaga o registo.
func (h *hubExtensao) abrirPedido(p *pedidoExtensao) (<-chan respostaExtensao, func()) {
	b := make([]byte, 8)
	rand.Read(b)
	p.ID = hex.EncodeToString(b)
	resp := make(chan respostaExtensao, 16)
	h.mu.Lock()
	h.respostas[p.ID] = resp
	h.mu.Unlock()
	return resp, func() {
		h.mu.Lock()
		delete(h.respostas, p.ID)
		h.mu.Unlock()
	}
}

func (h *hubExtensao) difundir(p pedidoExtensao) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clientes {
		select {
		case c <- p:
		default:
		}
	}
}

// pedirEntrega escreve texto (enviar) ou mostra o terminal (focar) numa só janela do IDE. Primeiro
// cada janela candidata-se com o seu alcance; ao fim de recolhaCandidaturas (ou logo, com alcance
// máximo), só a de maior alcance executa. Uma resposta sem alcance vem de uma extensão antiga,
// que já agiu: conta como entrega. Em «enviar» só conta a janela com o terminal da sessão
// (alcance máximo): o terminal ativo da janela dona da pasta pode ser uma shell (R-01), e uma
// extensão 0.9.0 ainda se candidatava com ele.
func (h *hubExtensao) pedirEntrega(acao string, pids []int, cwd, texto string) (respostaExtensao, bool) {
	p := pedidoExtensao{Acao: acao, Pids: pids, Cwd: cwd, Texto: texto, Fase: faseCandidatar}
	resp, fim := h.abrirPedido(&p)
	defer fim()
	h.difundir(p)

	var melhor *respostaExtensao
	prazo := time.NewTimer(recolhaCandidaturas)
	defer prazo.Stop()
recolha:
	for {
		select {
		case r := <-resp:
			if r.Alcance == nil {
				return r, true
			}
			if acao == "enviar" && *r.Alcance < math.MaxFloat64 {
				continue
			}
			if melhor == nil || *r.Alcance > *melhor.Alcance {
				melhor = &r
			}
			if *r.Alcance >= math.MaxFloat64 {
				break recolha
			}
		case <-prazo.C:
			break recolha
		}
	}
	if melhor == nil && acao == "enviar" {
		return respostaExtensao{Erro: semTerminal}, false
	}
	if melhor == nil {
		return respostaExtensao{Erro: semJanela}, false
	}

	p.Fase, p.Janela = faseExecutar, melhor.Janela
	h.difundir(p)
	limite := time.After(esperaExtensao)
	for {
		select {
		case r := <-resp:
			// Candidaturas atrasadas de outras janelas não contam.
			if r.Janela == melhor.Janela && r.Alcance == nil {
				return r, true
			}
		case <-limite:
			return respostaExtensao{Erro: "a janela do IDE escolhida não confirmou"}, false
		}
	}
}

func (h *hubExtensao) responder(r respostaExtensao) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	c, ok := h.respostas[r.Pedido]
	if !ok || !r.Ok {
		return ok
	}
	select {
	case c <- r:
	default:
	}
	return true
}

func (s *Servico) eventosExtensao(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "sem streaming", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	c := make(chan pedidoExtensao, 8)
	s.ext.mu.Lock()
	s.ext.clientes[c] = struct{}{}
	s.ext.mu.Unlock()
	defer func() {
		s.ext.mu.Lock()
		delete(s.ext.clientes, c)
		s.ext.mu.Unlock()
		s.painel.Difusor.Mudou()
	}()
	s.painel.Difusor.Mudou()
	fmt.Fprint(w, "retry: 3000\n\n")
	fl.Flush()
	vivo := time.NewTicker(25 * time.Second)
	defer vivo.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.fechado:
			return
		case p := <-c:
			dados, _ := json.Marshal(p)
			fmt.Fprintf(w, "event: pedido\ndata: %s\n\n", dados)
		case <-vivo.C:
			fmt.Fprint(w, ": vivo\n\n")
		}
		fl.Flush()
	}
}

// esperaFoco é curta: o hook do agy pergunta isto antes de cada comando a aprovar.
var esperaFoco = 700 * time.Millisecond

// presenca diz ao hook se a janela do IDE com o terminal (ou a pasta) da sessão tem o foco;
// sabe é falso sem extensão ligada ou quando nenhuma janela é dona da sessão.
func (s *Servico) presenca(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Pids []int  `json:"pids"`
		Cwd  string `json:"cwd"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024)).Decode(&p); err != nil {
		http.Error(w, `{"erro":"json"}`, http.StatusBadRequest)
		return
	}
	if s.ext.ligadas() == 0 {
		escreverJSON(w, map[string]bool{"sabe": false, "focada": false})
		return
	}
	resp, ok := s.ext.enviarPedido(pedidoExtensao{Acao: "foco", Pids: p.Pids, Cwd: p.Cwd}, esperaFoco)
	escreverJSON(w, map[string]bool{"sabe": ok, "focada": resp.Focada})
}

func (s *Servico) respostaExtensao(w http.ResponseWriter, r *http.Request) {
	var resp respostaExtensao
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&resp); err != nil {
		http.Error(w, `{"erro":"json"}`, http.StatusBadRequest)
		return
	}
	if !s.ext.responder(resp) {
		http.Error(w, `{"erro":"pedido desconhecido"}`, http.StatusNotFound)
		return
	}
	escreverJSON(w, map[string]bool{"ok": true})
}

func escreverJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}
