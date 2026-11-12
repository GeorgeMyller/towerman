package servico

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/GeorgeMyller/towerman/internal/quadro"
	"github.com/GeorgeMyller/towerman/internal/transcricao"
)

const (
	maxPassosPadrao = 10
	tamanhoResposta = 6000
	tamanhoDiff     = 12000
	modeloPadrao    = "Mensagem automática do fluxo «{fluxo}» (Towerman): a sessão «{de}» terminou um passo.\n\n" +
		"O que ela respondeu:\n{resposta}\n\nFicheiros que tocou: {toques}\nPasta de trabalho dela: {pasta}\n\n" +
		"Alterações por gravar na pasta dela:\n{diff}"
)

// passagemPendente é uma passagem registada no escritor que ainda tem de ser entregue ao destino.
type passagemPendente struct {
	ID    int64
	Para  string
	Texto string
}

// ultimaResposta junta o que o agente escreveu depois do último pedido do utilizador.
func ultimaResposta(transcript string) string {
	if transcript == "" {
		return ""
	}
	ms, err := transcricao.Ultimas(transcript, 80)
	if err != nil {
		return ""
	}
	var partes []string
	for i := len(ms) - 1; i >= 0; i-- {
		if ms[i].Papel == "utilizador" {
			break
		}
		if ms[i].Papel == "assistente" {
			partes = append([]string{ms[i].Texto}, partes...)
		}
	}
	r := strings.TrimSpace(strings.Join(partes, "\n\n"))
	if rs := []rune(r); len(rs) > tamanhoResposta {
		r = "…" + string(rs[len(rs)-tamanhoResposta:])
	}
	return r
}

// dispararArestas corre no Stop de uma sessão que parou de facto (sem mensagens em fila).
func (p *Processador) dispararArestas(s quadro.Sessao, doHook string, agora time.Time) ([]passagemPendente, error) {
	arestas, err := p.Quadro.ArestasAtivasDe(s.SessionID)
	if err != nil || len(arestas) == 0 {
		return nil, err
	}
	resposta := strings.TrimSpace(doHook)
	if resposta == "" {
		resposta = ultimaResposta(s.TranscriptPath)
	} else if rs := []rune(resposta); len(rs) > tamanhoResposta {
		resposta = "…" + string(rs[len(rs)-tamanhoResposta:])
	}
	if resposta == "" {
		resposta = "(sem texto de resposta; pedido original: " + s.Resumo + ")"
	}
	var fs []string
	if ts, err := p.Quadro.ToquesRecentes(s.SessionID, 8); err == nil {
		for _, t := range ts {
			if t.Tipo != "git" {
				fs = append(fs, t.Caminho)
			}
		}
	}
	toques := "nenhum"
	if len(fs) > 0 {
		toques = strings.Join(fs, ", ")
	}
	diff := ""
	var r []passagemPendente
	for _, a := range arestas {
		if diff == "" && strings.Contains(a.Modelo, "{diff}") {
			diff = alteracoes(s.Cwd)
		}
		if a.Gatilho == quadro.GatilhoMarca && !strings.Contains(resposta, a.Marca) {
			continue
		}
		f, err := p.Quadro.ObterFluxo(a.Fluxo)
		if err != nil {
			return r, err
		}
		if f.Estado != quadro.FluxoAtivo {
			continue
		}
		if f.Passos >= f.MaxPassos {
			if err := p.Quadro.DefinirEstadoFluxo(f.ID, quadro.FluxoLimite); err != nil {
				return r, err
			}
			continue
		}
		texto := strings.NewReplacer("{de}", nomeOuID(s.Nome, s.SessionID), "{para}", nomeOuID(a.NomePara, a.Para),
			"{resposta}", resposta, "{toques}", toques, "{fluxo}", f.Nome, "{pasta}", s.Cwd, "{diff}", diff).Replace(a.Modelo)
		pass := quadro.Passagem{Fluxo: f.ID, Aresta: a.ID, De: s.SessionID, Para: a.Para, Texto: texto, Estado: quadro.MensagemPendente}
		destino, ok, err := p.Quadro.ObterSessao(a.Para)
		if err != nil {
			return r, err
		}
		if !ok || destino.Estado == quadro.Terminada {
			pass.Estado, pass.Motivo = quadro.MensagemFalhada, "a sessão de destino já terminou"
			if _, err := p.Quadro.RegistarPassagem(pass, agora); err != nil {
				return r, err
			}
			continue
		}
		id, err := p.Quadro.RegistarPassagem(pass, agora)
		if err != nil {
			return r, err
		}
		r = append(r, passagemPendente{ID: id, Para: a.Para, Texto: texto})
	}
	return r, nil
}

// alteracoes resume o trabalho por gravar numa pasta: o diff dos ficheiros seguidos e o conteúdo
// dos novos. Só lê; não usa «git add -N», que mexeria no índice da worktree da outra sessão.
func alteracoes(cwd string) string {
	if cwd == "" {
		return "(pasta desconhecida)"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var b strings.Builder
	if out, err := exec.CommandContext(ctx, "git", "-C", cwd, "diff", "HEAD", "--stat", "--patch").Output(); err == nil {
		b.Write(out)
	}
	if out, err := exec.CommandContext(ctx, "git", "-C", cwd, "ls-files", "--others", "--exclude-standard").Output(); err == nil {
		for _, f := range strings.Fields(string(out)) {
			if b.Len() > tamanhoDiff {
				break
			}
			dados, err := os.ReadFile(filepath.Join(cwd, f))
			if err != nil || len(dados) > tamanhoDiff || bytes.IndexByte(dados, 0) >= 0 {
				fmt.Fprintf(&b, "\n+++ ficheiro novo %s (não incluído)\n", f)
				continue
			}
			fmt.Fprintf(&b, "\n+++ ficheiro novo %s\n%s", f, dados)
		}
	}
	r := strings.TrimSpace(b.String())
	if r == "" {
		return "(nenhuma)"
	}
	if len(r) > tamanhoDiff {
		r = r[:tamanhoDiff] + "\n… (cortado)"
	}
	return r
}

// entregarPassagens corre fora do escritor: a entrega pode esperar pela extensão do IDE.
func (s *Servico) entregarPassagens(ps []passagemPendente) {
	for _, p := range ps {
		sessao, ok, err := s.quadro.ObterSessao(p.Para)
		if err != nil || !ok {
			continue
		}
		feito := make(chan int64, 1)
		s.enfileirar(context.Background(), func() {
			id, _ := s.quadro.CriarMensagem(p.Para, p.Texto, "fluxo", time.Now())
			feito <- id
		})
		idMsg := <-feito
		caminho, estado, motivo := s.encaminhar(sessao, p.Texto)
		s.enfileirar(context.Background(), func() {
			s.quadro.MarcarMensagem(idMsg, estado, caminho, motivo, time.Now())
			s.quadro.MarcarPassagem(p.ID, estado, motivo)
		})
	}
}

func (s *Servico) posProcessar(r Resposta) {
	if len(r.Passagens) > 0 {
		go s.entregarPassagens(r.Passagens)
	}
	if r.Fim != nil {
		s.anotarFim(*r.Fim)
	}
}

// maxFins limita os avisos «terminou» à espera do relay (sem relay ligado, não saem).
const maxFins = 20

// anotarFim guarda o aviso «terminou» para o relay o enviar por push, marca a hora no estado dos
// aparelhos (doneAt: o iPhone e o relógio avisam sem push) e acorda quem acompanha.
func (s *Servico) anotarFim(f fimSessao) {
	s.finsMu.Lock()
	s.fins = append(s.fins, f)
	if len(s.fins) > maxFins {
		s.fins = s.fins[len(s.fins)-maxFins:]
	}
	if s.fimEm == nil {
		s.fimEm = map[string]string{}
	}
	s.fimEm[f.SessionID] = time.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00")
	s.finsMu.Unlock()
	s.painel.Difusor.Mudou()
}

// fimDe devolve a hora do último aviso «terminou» da sessão ("" sem aviso).
func (s *Servico) fimDe(sessionID string) string {
	s.finsMu.Lock()
	defer s.finsMu.Unlock()
	return s.fimEm[sessionID]
}

// tirarFins devolve e esvazia os avisos «terminou» pendentes.
func (s *Servico) tirarFins() []fimSessao {
	s.finsMu.Lock()
	defer s.finsMu.Unlock()
	f := s.fins
	s.fins = nil
	return f
}

func (s *Servico) listarFluxos(w http.ResponseWriter, r *http.Request) {
	fs, err := s.quadro.Fluxos(30)
	if err != nil {
		http.Error(w, `{"erro":"quadro"}`, http.StatusInternalServerError)
		return
	}
	if fs == nil {
		fs = []quadro.Fluxo{}
	}
	escreverJSON(w, fs)
}

type pedidoAresta struct {
	De        string `json:"de"`
	Para      string `json:"para"`
	Gatilho   string `json:"gatilho"`
	Marca     string `json:"marca"`
	Modelo    string `json:"modelo"`
	MaxPassos int    `json:"max_passos"`
	Nome      string `json:"nome"`
}

func (s *Servico) criarAresta(w http.ResponseWriter, r *http.Request) {
	var p pedidoAresta
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&p); err != nil {
		http.Error(w, `{"erro":"json"}`, http.StatusBadRequest)
		return
	}
	if p.Gatilho == "" {
		p.Gatilho = quadro.GatilhoStop
	}
	p.Marca = strings.TrimSpace(p.Marca)
	switch {
	case p.De == "" || p.Para == "" || p.De == p.Para:
		http.Error(w, `{"erro":"escolhe duas sessões diferentes"}`, http.StatusBadRequest)
		return
	case p.Gatilho != quadro.GatilhoStop && p.Gatilho != quadro.GatilhoMarca:
		http.Error(w, `{"erro":"gatilho inválido"}`, http.StatusBadRequest)
		return
	case p.Gatilho == quadro.GatilhoMarca && p.Marca == "":
		http.Error(w, `{"erro":"indica a marca que dispara a passagem"}`, http.StatusBadRequest)
		return
	}
	if p.MaxPassos == 0 {
		p.MaxPassos = maxPassosPadrao
	}
	if p.MaxPassos < 1 || p.MaxPassos > 100 {
		http.Error(w, `{"erro":"máximo de passos entre 1 e 100"}`, http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(p.Modelo) == "" {
		p.Modelo = modeloPadrao
	}
	a, err := tarefa(s, r, func() (quadro.Aresta, error) {
		de, ok1, err1 := s.quadro.ObterSessao(p.De)
		para, ok2, err2 := s.quadro.ObterSessao(p.Para)
		if err1 != nil || err2 != nil || !ok1 || !ok2 || de.Estado == quadro.Terminada || para.Estado == quadro.Terminada {
			return quadro.Aresta{}, fmt.Errorf("as duas sessões têm de estar vivas")
		}
		fluxo, err := s.quadro.FluxoDaSessao(p.De)
		if err == nil && fluxo == 0 {
			fluxo, err = s.quadro.FluxoDaSessao(p.Para)
		}
		if err != nil {
			return quadro.Aresta{}, err
		}
		if fluxo == 0 {
			nome := strings.TrimSpace(p.Nome)
			if nome == "" {
				nome = nomeOuID(de.Nome, de.SessionID) + " → " + nomeOuID(para.Nome, para.SessionID)
			}
			if fluxo, err = s.quadro.CriarFluxo(nome, p.MaxPassos, time.Now()); err != nil {
				return quadro.Aresta{}, err
			}
		}
		a := quadro.Aresta{Fluxo: fluxo, De: p.De, Para: p.Para, Gatilho: p.Gatilho, Marca: p.Marca, Modelo: p.Modelo}
		a.ID, err = s.quadro.CriarAresta(a, time.Now())
		return a, err
	})
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"erro":%q}`, err.Error()), http.StatusConflict)
		return
	}
	escreverJSON(w, a)
}

func (s *Servico) apagarAresta(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, `{"erro":"id"}`, http.StatusBadRequest)
		return
	}
	if _, err := tarefa(s, r, func() (bool, error) { return true, s.quadro.ApagarAresta(id) }); err != nil {
		http.Error(w, `{"erro":"ligação desconhecida"}`, http.StatusNotFound)
		return
	}
	escreverJSON(w, map[string]bool{"ok": true})
}

// estadoFluxo para ou retoma um fluxo; ao retomar pode aumentar o máximo de passos.
func (s *Servico) estadoFluxo(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var p struct {
		Estado    string `json:"estado"`
		MaxPassos int    `json:"max_passos"`
	}
	if err != nil || json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&p) != nil ||
		(p.Estado != quadro.FluxoAtivo && p.Estado != quadro.FluxoParado) || p.MaxPassos < 0 || p.MaxPassos > 100 {
		http.Error(w, `{"erro":"pedido inválido"}`, http.StatusBadRequest)
		return
	}
	if _, err := tarefa(s, r, func() (bool, error) {
		if p.MaxPassos > 0 {
			if err := s.quadro.DefinirMaxPassos(id, p.MaxPassos); err != nil {
				return false, err
			}
		}
		return true, s.quadro.DefinirEstadoFluxo(id, p.Estado)
	}); err != nil {
		http.Error(w, `{"erro":"fluxo desconhecido"}`, http.StatusNotFound)
		return
	}
	escreverJSON(w, map[string]bool{"ok": true})
}
