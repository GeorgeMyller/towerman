package servico

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	detetor "github.com/GeorgeMyller/towerman/internal/decisao"
	"github.com/GeorgeMyller/towerman/internal/quadro"
	"github.com/GeorgeMyller/towerman/internal/relay"
)

// registarDecisao corre no Stop de uma sessão que parou de facto: procura no texto final uma
// decisão em texto livre («Digite CONTINUAR», menu, «Posso começar?») e abre-a no quadro. Sem
// decisão, a anterior deixa de valer (o agente já respondeu a outra coisa).
func (p *Processador) registarDecisao(s quadro.Sessao, doHook string, agora time.Time) error {
	if p.DecisoesLigadas != nil && !p.DecisoesLigadas() {
		return nil
	}
	texto := strings.TrimSpace(doHook)
	if texto == "" {
		texto = ultimaResposta(s.TranscriptPath)
	}
	d, ok := detetor.Detetar(texto)
	if !ok {
		return p.fecharDecisao(s.SessionID, quadro.DecisaoSubstituida, quadro.Resposta{}, agora)
	}
	if antiga, aberta, err := p.Quadro.DecisaoAberta(s.SessionID); err == nil && aberta {
		registarFecho(antiga, quadro.DecisaoSubstituida, agora)
	}
	nova := quadro.Decisao{SessionID: s.SessionID, Pergunta: d.Pergunta, Padrao: d.Padrao, Lingua: d.Lingua,
		Opcoes: []quadro.OpcaoDecisao{}}
	for _, o := range d.Opcoes {
		nova.Opcoes = append(nova.Opcoes, quadro.OpcaoDecisao{Rotulo: o.Rotulo, Envio: o.Envio})
	}
	_, err := p.Quadro.AbrirDecisao(nova, agora)
	return err
}

// fecharDecisao fecha a decisão aberta da sessão, se houver, e regista o desfecho.
func (p *Processador) fecharDecisao(sessionID, desfecho string, resp quadro.Resposta, agora time.Time) error {
	d, fechou, err := p.Quadro.FecharDecisaoDaSessao(sessionID, desfecho, resp, agora)
	if fechou {
		registarFecho(d, desfecho, agora)
	}
	return err
}

// registarFecho deixa no log o padrão e o desfecho de cada decisão, sem o texto: é assim que se
// mede a precisão da deteção no uso real (falsos positivos acabam «descartada»).
func registarFecho(d quadro.Decisao, desfecho string, agora time.Time) {
	duracao := "?"
	if criada, err := time.Parse(quadro.FormatoData, d.CriadaEm); err == nil {
		duracao = agora.Sub(criada).Round(time.Second).String()
	}
	log.Printf("towerman: decisão %d fechada: padrão=%s língua=%s desfecho=%s duração=%s",
		d.ID, d.Padrao, d.Lingua, desfecho, duracao)
}

// Protocolo /v1: a decisão aparece como «waiting» com kind "decision", id "d<n>" e chaves "d:<i>".
const (
	prefixoDecisao    = "d"
	prefixoOpcaoDec   = "d:"
	chaveDescartar    = "dismiss"
	decisaoRespondida = "a decisão já foi respondida"
)

func decisaoV1(d quadro.Decisao, entrega bool, motivo string) map[string]any {
	opcoes := []map[string]any{}
	for i, o := range d.Opcoes {
		op := map[string]any{"key": prefixoOpcaoDec + strconv.Itoa(i), "label": o.Rotulo, "primary": i == 0}
		if !entrega {
			op["disabled"] = true
		}
		opcoes = append(opcoes, op)
	}
	titulo := "Decisão"
	if d.Lingua == "en" {
		titulo = "Decision"
	}
	contexto := ""
	if !entrega {
		contexto = motivo
	}
	return map[string]any{"id": prefixoDecisao + strconv.FormatInt(d.ID, 10), "kind": "decision", "title": titulo,
		"detail": d.Pergunta, "context": contexto, "options": opcoes}
}

var errDecisaoFechada = errors.New(decisaoRespondida)

// responderDecisao trata «answer» numa decisão: fecha-a primeiro no escritor (só uma resposta
// ganha) e depois entrega o texto da opção. Se a entrega falhar, a decisão volta a abrir.
func (s *Servico) responderDecisao(w http.ResponseWriter, r *http.Request, sessaoID, promptID, chave string,
	responder func(int, any)) {
	id, err := strconv.ParseInt(strings.TrimPrefix(promptID, prefixoDecisao), 10, 64)
	if err != nil {
		responder(400, map[string]string{"error": "pedido desconhecido"})
		return
	}
	type reivindicada struct {
		decisao quadro.Decisao
		sessao  quadro.Sessao
		envio   string
	}
	rv, err := tarefa(s, r, func() (reivindicada, error) {
		d, aberta, err := s.quadro.DecisaoAberta(sessaoID)
		if err != nil {
			return reivindicada{}, err
		}
		if !aberta || d.ID != id {
			return reivindicada{}, errDecisaoFechada
		}
		sessao, _, err := s.quadro.ObterSessao(sessaoID)
		if err != nil {
			return reivindicada{}, err
		}
		if chave == chaveDescartar {
			return reivindicada{decisao: d}, s.proc.fecharDecisao(sessaoID, quadro.DecisaoDescartada, canalDoPedido(r), time.Now())
		}
		i, err := strconv.Atoi(strings.TrimPrefix(chave, prefixoOpcaoDec))
		if !strings.HasPrefix(chave, prefixoOpcaoDec) || err != nil || i < 0 || i >= len(d.Opcoes) {
			return reivindicada{}, errOpcaoDesconhecida
		}
		if ok, motivo := s.entregavel(sessao); !ok {
			return reivindicada{}, errors.New(motivo)
		}
		resp := canalDoPedido(r)
		resp.Forma = quadro.FormaOpcao
		if err := s.proc.fecharDecisao(sessaoID, quadro.DecisaoRespondida, resp, time.Now()); err != nil {
			return reivindicada{}, err
		}
		return reivindicada{decisao: d, sessao: sessao, envio: d.Opcoes[i].Envio}, nil
	})
	switch {
	case errors.Is(err, errOpcaoDesconhecida):
		responder(400, map[string]string{"error": err.Error()})
		return
	case err != nil:
		responder(409, map[string]string{"error": err.Error()})
		return
	}
	if rv.envio == "" { // descartada
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.entregarResposta(r, rv.sessao, rv.envio); err != nil {
		// A resposta não chegou: a decisão volta a estar pendente, com as mesmas opções.
		tarefa(s, r, func() (struct{}, error) { return struct{}{}, s.quadro.ReabrirDecisao(rv.decisao) })
		responder(409, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

var errOpcaoDesconhecida = errors.New("opção desconhecida")

// entregarResposta grava a mensagem e entrega-a já. Uma resposta a uma decisão não fica na fila:
// a sessão está parada à espera dela e nunca chegaria ao próximo Stop.
func (s *Servico) entregarResposta(r *http.Request, sessao quadro.Sessao, texto string) error {
	id, err := tarefa(s, r, func() (int64, error) {
		return s.quadro.CriarMensagem(sessao.SessionID, texto, "stop", time.Now())
	})
	if err != nil {
		return err
	}
	caminho, estado, motivo := s.encaminhar(sessao, texto)
	if estado != quadro.MensagemEntregue {
		estado, motivo = quadro.MensagemFalhada, "a resposta não chegou ao terminal"
	}
	if _, err := tarefa(s, r, func() (struct{}, error) {
		return struct{}{}, s.quadro.MarcarMensagem(id, estado, caminho, motivo, time.Now())
	}); err != nil {
		return err
	}
	if estado != quadro.MensagemEntregue {
		return errors.New(motivo)
	}
	return nil
}

// pushesDecisao gera um push por aparelho com a decisão cifrada para esse aparelho (mesma chave e
// AAD do estado pelo relay). Em claro só vai um texto genérico: a pergunta pode ter dados do projeto.
func (s *Servico) pushesDecisao(titulo, agentID, promptID string, espera map[string]any) []relay.PedidoPush {
	claro, err := json.Marshal(map[string]any{"title": titulo, "detail": espera["detail"], "options": espera["options"]})
	if err != nil {
		return nil
	}
	var r []relay.PedidoPush
	for _, ap := range s.aparelhos.comPush() {
		if ap.ChavePublica == "" {
			continue
		}
		chave, err := relay.DerivarChaveSimetrica(s.aparelhos.chavePriv, ap.ChavePublica, s.aparelhos.servidor)
		if err != nil {
			continue
		}
		enc, err := relay.Selar(chave, claro, []byte(ap.ID))
		if err != nil {
			continue
		}
		r = append(r, relay.PedidoPush{Token: ap.PushToken, Titulo: "Towerman", Corpo: "Uma sessão espera uma decisão.",
			Categoria: "decisao", AgentID: agentID, PromptID: promptID, Enc: enc, Device: ap.ID, Server: s.aparelhos.servidor})
	}
	return r
}

// decisoesPainel: as decisões abertas no formato do /v1, com a sessão e a hora, para o deck web.
func (s *Servico) decisoesPainel() []map[string]any {
	r := []map[string]any{}
	ds, err := s.quadro.DecisoesAbertas()
	if err != nil {
		return r
	}
	for _, d := range ds {
		sessao, ok, err := s.quadro.ObterSessao(d.SessionID)
		if err != nil || !ok || sessao.Estado == quadro.Terminada {
			continue
		}
		entrega, motivo := s.entregavel(sessao)
		v := decisaoV1(d, entrega, motivo)
		v["session_id"], v["criada_em"] = d.SessionID, d.CriadaEm
		r = append(r, v)
	}
	return r
}

// responderDecisaoPainel é a mesma resposta do relógio, pela API local do deck ({"id","chave"}).
func (s *Servico) responderDecisaoPainel(w http.ResponseWriter, r *http.Request) {
	var pedido struct {
		ID    string `json:"id"`
		Chave string `json:"chave"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&pedido) != nil || pedido.ID == "" {
		http.Error(w, `{"erro":"json"}`, http.StatusBadRequest)
		return
	}
	s.responderDecisao(w, r, r.PathValue("id"), pedido.ID, pedido.Chave, func(codigo int, v any) {
		erro := ""
		if m, ok := v.(map[string]string); ok {
			erro = m["error"]
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(codigo)
		json.NewEncoder(w).Encode(map[string]string{"erro": erro})
	})
}
