package servico

import (
	"net/http"
	"time"

	"github.com/GeorgeMyller/towerman/internal/quadro"
)

// Cabeçalhos internos com que a rede passa à API local quem respondeu (ver
// _reversa_forward/012-metricas/interfaces/origem-resposta.md).
const (
	cabecalhoAparelho = "X-Towerman-Device-ID"
	cabecalhoOrigem   = "X-Towerman-Origem"
)

// canalDoPedido diz quem respondeu: o aparelho autenticado pela rede, ou o painel local.
func canalDoPedido(r *http.Request) quadro.Resposta {
	ap := r.Header.Get(cabecalhoAparelho)
	if ap == "" {
		return quadro.Resposta{Canal: quadro.CanalPainel}
	}
	return quadro.Resposta{Canal: ap, Origem: origemValida(r.Header.Get(cabecalhoOrigem))}
}

// origemValida aceita só as origens conhecidas; o resto fica vazio.
func origemValida(o string) string {
	switch o {
	case quadro.OrigemIPhone, quadro.OrigemRelogio, quadro.OrigemMac:
		return o
	}
	return ""
}

// origemDoCliente converte o campo «origin» das apps (watch, iphone, mac) na origem gravada.
func origemDoCliente(o string) string {
	switch o {
	case "watch":
		return quadro.OrigemRelogio
	case "iphone":
		return quadro.OrigemIPhone
	case "mac":
		return quadro.OrigemMac
	}
	return ""
}

// periodosMetricas são os períodos aceites, em dias (RN-02).
var periodosMetricas = map[string]int{"1": 1, "7": 7, "30": 30}

// metricas calcula as métricas do período pedido; vazio vale 7 dias.
func (s *Servico) metricas(param string) (quadro.Metricas, bool, error) {
	if param == "" {
		param = "7"
	}
	dias, ok := periodosMetricas[param]
	if !ok {
		return quadro.Metricas{}, false, nil
	}
	ate := time.Now()
	m, err := s.quadro.Metricas(ate.Add(-time.Duration(dias)*24*time.Hour), ate)
	if err != nil {
		return m, true, err
	}
	m.Computador, m.Dias = nomeMaquina(), dias
	for i := range m.Atencao.PorCanal {
		l := &m.Atencao.PorCanal[i]
		switch l.Canal {
		case "":
			l.Nome = "Desconhecido"
		case quadro.CanalPainel:
			l.Nome = "Painel"
		default:
			l.Nome = "Aparelho removido"
			if ap, ok := s.aparelhos.obterPorID(l.Canal); ok {
				l.Nome = ap.Nome
			}
		}
	}
	return m, true, nil
}

// obterMetricas é a rota local do painel: GET /v1/metricas?dias=N.
func (s *Servico) obterMetricas(w http.ResponseWriter, r *http.Request) {
	m, ok, err := s.metricas(r.URL.Query().Get("dias"))
	switch {
	case !ok:
		http.Error(w, `{"erro":"dias tem de ser 1, 7 ou 30"}`, http.StatusBadRequest)
	case err != nil:
		http.Error(w, `{"erro":"quadro"}`, http.StatusInternalServerError)
	default:
		escreverJSON(w, m)
	}
}

// redeMetricas é a rota dos aparelhos pareados: GET /v1/metrics?days=N (porta de rede e relay).
func (s *Servico) redeMetricas(w http.ResponseWriter, r *http.Request) {
	m, ok, err := s.metricas(r.URL.Query().Get("days"))
	switch {
	case !ok:
		http.Error(w, `{"error":"days must be 1, 7 or 30"}`, http.StatusBadRequest)
	case err != nil:
		http.Error(w, `{"error":"board unavailable"}`, http.StatusInternalServerError)
	default:
		escreverJSON(w, m)
	}
}
