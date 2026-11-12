package servico

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/GeorgeMyller/towerman/internal/config"
	"github.com/GeorgeMyller/towerman/internal/relay"
)

// pedidoAcaoCifrada é o JSON claro dentro do envelope AES-256-GCM enviado pelo iPhone/Watch via Relay.
type pedidoAcaoCifrada struct {
	// Op preenchido é uma ação do servidor (catalog, launch), sem agente.
	Op       string          `json:"op,omitempty"`
	Launch   json.RawMessage `json:"launch,omitempty"`
	AgentID  string          `json:"agent_id"`
	Action   string          `json:"action"`
	PromptID string          `json:"promptId,omitempty"`
	Key      string          `json:"key,omitempty"`
	Text     string          `json:"text,omitempty"`
	// Origin: iphone, watch ou mac (métricas); Days: período de «metrics».
	Origin string `json:"origin,omitempty"`
	Days   int    `json:"days,omitempty"`
}

func segredoHostRelay(home string) string {
	p := filepath.Join(home, "relay-secret")
	if b, err := os.ReadFile(p); err == nil && len(bytes.TrimSpace(b)) >= 32 {
		return string(bytes.TrimSpace(b))
	}
	s := aleatorio(24)
	_ = config.EscreverAtomico(p, []byte(s), 0o600)
	return s
}

func normalizarURLRelay(base, serverID string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return ""
	}
	return base + "/s/" + serverID
}

// iniciarRelay liga o Mac ao servidor Relay WebSocket com E2EE e economia de cota.
func (s *Servico) iniciarRelay(ctx context.Context, baseRelay string) {
	urlSala := normalizarURLRelay(baseRelay, s.aparelhos.servidor)
	if urlSala == "" {
		return
	}
	segredo := segredoHostRelay(s.op.Home)
	go func() {
		espera := esperaRelayMin
		for ctx.Err() == nil {
			inicio := time.Now()
			// O token é relido a cada tentativa: corrigir o config.json não exige reiniciar.
			token := tokenRelay(s.op.Home)
			err := s.correrSessaoRelay(ctx, urlSala, segredo, token)
			if time.Since(inicio) > time.Minute {
				espera = esperaRelayMin
			}
			var depois time.Duration
			espera, depois = esperasRelay(espera, err)
			if tokenRecusado(err) {
				motivo := "token errado"
				if token == "" {
					motivo = "o relay exige token e não há nenhum"
				}
				log.Printf("towerman: relay recusou o token (%s): %s; defina relay_token no config.json ou TOWERMAN_RELAY_TOKEN; nova tentativa em %s",
					urlSala, motivo, espera)
			}
			select {
			case <-ctx.Done():
				return
			case <-s.fechado:
				return
			case <-time.After(espera):
			}
			espera = depois
		}
	}()
}

const (
	esperaRelayMin = 2 * time.Second
	esperaRelayMax = 2 * time.Minute
	// Com o token recusado, insistir não resolve: espera-se muito mais para não martelar o relay.
	esperaTokenRecusado    = 10 * time.Minute
	esperaTokenRecusadoMax = time.Hour
)

// tokenRecusado diz se o relay recusou o host com 401 (token ausente ou errado).
func tokenRecusado(err error) bool {
	var eh *relay.ErroHandshake
	return errors.As(err, &eh) && eh.Status == http.StatusUnauthorized
}

// esperasRelay devolve quanto esperar agora e a espera seguinte (backoff exponencial). Um 401 sobe
// a espera para pelo menos esperaTokenRecusado, com teto de uma hora.
func esperasRelay(espera time.Duration, err error) (agora, depois time.Duration) {
	if tokenRecusado(err) {
		agora = max(espera, esperaTokenRecusado)
		return agora, min(agora*2, esperaTokenRecusadoMax)
	}
	return espera, min(espera*2, esperaRelayMax)
}

// correrSessaoRelay liga como host e serve a sala até a ligação cair. Devolve o erro da ligação
// (nil quando chegou a ligar).
func (s *Servico) correrSessaoRelay(ctx context.Context, urlSala, segredo, token string) error {
	wsURL := urlSala + "/ws?role=host"
	cab := map[string]string{"X-Towerman-Host-Secret": segredo}
	if token != "" {
		cab[relay.CabecalhoTokenRelay] = token
	}
	// Tempo limite de 65 s para aguardar o arranque a frio (cold start) do Render Free Tier.
	ligarCtx, cancel := context.WithTimeout(ctx, 65*time.Second)
	ws, err := relay.LigarWS(ligarCtx, wsURL, cab)
	cancel()
	if err != nil {
		if !tokenRecusado(err) {
			log.Printf("towerman: relay indisponível (%s): %v", urlSala, err)
		}
		return err
	}
	urlTunel.Store(urlSala)
	esquecerEnderecos()
	log.Printf("towerman: relay E2EE ativo em %s", urlSala)

	defer func() {
		_ = ws.Fechar()
		if enderecoTunel() == urlSala {
			urlTunel.Store("")
			esquecerEnderecos()
		}
	}()

	var (
		mu          sync.Mutex
		viewers     []string
		notificadas = make(map[string]struct{})
		ultVersao   string
	)

	// enviarEstadoCifrado cifra e envia o estadoV1 apenas para os aparelhos com a tela aberta.
	enviarEstadoCifrado := func(destinos []string, forcar bool) {
		if len(destinos) == 0 {
			return
		}
		estado := s.estadoV1()
		v := versaoEstado(estado)
		if !forcar && v == ultVersao {
			return
		}
		ultVersao = v
		claro, err := json.Marshal(estado)
		if err != nil {
			return
		}
		for _, devID := range destinos {
			ap, ok := s.aparelhos.obterPorID(devID)
			if !ok || ap.ChavePublica == "" {
				continue
			}
			chave, err := relay.DerivarChaveSimetrica(s.aparelhos.chavePriv, ap.ChavePublica, s.aparelhos.servidor)
			if err != nil {
				continue
			}
			cifrado, err := relay.Selar(chave, claro, []byte(ap.ID))
			if err != nil {
				continue
			}
			_ = ws.EscreverJSON(relay.Mensagem{
				Tipo:    "state",
				Device:  ap.ID,
				Payload: cifrado,
			})
		}
	}

	// verificarPermissoesPush aplica a Regra 2: com viewers == 0, só envia pacote quando surge
	// uma permissão nova em espera para acordar o iPhone/Watch via APNs.
	verificarPermissoesPush := func() {
		tokens := s.aparelhos.tokensPush()
		if len(tokens) == 0 {
			return
		}
		estado := s.estadoV1()
		agentes, _ := estado["agents"].([]map[string]any)
		atuais := make(map[string]struct{})
		var novos []relay.PedidoPush

		for _, ag := range agentes {
			espera, ok := ag["waiting"].(map[string]any)
			if !ok || espera == nil {
				continue
			}
			agID, _ := ag["id"].(string)
			prID, _ := espera["id"].(string)
			if agID == "" || prID == "" {
				continue
			}
			chave := agID + "#" + prID
			atuais[chave] = struct{}{}
			if _, ja := notificadas[chave]; !ja {
				notificadas[chave] = struct{}{}
				tituloAg, _ := ag["title"].(string)
				hostAg, _ := ag["host"].(string)
				if hostAg != "" {
					tituloAg = tituloAg + " · " + hostAg
				}
				if espera["kind"] == "decision" {
					novos = append(novos, s.pushesDecisao(tituloAg, agID, prID, espera)...)
					continue
				}
				sub, _ := espera["title"].(string)
				det, _ := espera["detail"].(string)
				for _, tk := range tokens {
					novos = append(novos, relay.PedidoPush{
						Token:     tk,
						Titulo:    tituloAg,
						Subtitulo: sub,
						Corpo:     det,
						AgentID:   agID,
						PromptID:  prID,
					})
				}
			}
		}
		for k := range notificadas {
			if _, ainda := atuais[k]; !ainda {
				delete(notificadas, k)
			}
		}
		if len(novos) > 0 {
			_ = ws.EscreverJSON(relay.Mensagem{
				Tipo:   "push_wake",
				Pushes: novos,
			})
		}
	}

	// enviarFins manda o push «terminou» das sessões que pararam com a pessoa longe ou noutra
	// janela, e das lançadas à distância. Vai mesmo com aparelhos na sala: o ecrã aberto pode ser
	// de outro aparelho. O corpo é genérico; o início da resposta só segue cifrado, em enc.
	enviarFins := func() {
		fins := s.tirarFins()
		if len(fins) == 0 {
			return
		}
		host := nomeMaquina()
		var pushes []relay.PedidoPush
		for _, f := range fins {
			pushes = append(pushes, s.pushesFim(f, host)...)
		}
		if len(pushes) == 0 {
			return
		}
		_ = ws.EscreverJSON(relay.Mensagem{Tipo: "push_wake", Pushes: pushes})
	}

	mudou, sair := s.painel.Difusor.SubscreverRemoto(s.urgenciaV1)
	defer sair()

	pararLoop := make(chan struct{})
	defer close(pararLoop)

	// Ping a cada 30 s: o proxy do Render não entrega os pings do relay, e sem nenhum frame a
	// leitura expira aos 90 s e a ligação cai (em silêncio, com o ecrã do iPhone fechado).
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.fechado:
				return
			case <-pararLoop:
				return
			case <-t.C:
				if ws.Ping() != nil {
					return
				}
			}
		}
	}()

	// Contorno anti-hibernação (Render Free Tier): envia um GET /health HTTP a cada 10 minutos
	// enquanto o Mac estiver ligado, impedindo que o Render adormeça a instância após 15 min.
	go func() {
		cli := &http.Client{Timeout: 10 * time.Second}
		urlHealth := urlSala + "/health"
		t := time.NewTicker(10 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.fechado:
				return
			case <-pararLoop:
				return
			case <-t.C:
				resp, err := cli.Get(urlHealth)
				if err == nil {
					resp.Body.Close()
				}
			}
		}
	}()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.fechado:
				return
			case <-pararLoop:
				return
			case <-mudou:
				mu.Lock()
				ativos := append([]string(nil), viewers...)
				mu.Unlock()
				if len(ativos) > 0 {
					// Regra 1: há aparelhos com o ecrã aberto na sala -> envia estado cifrado.
					enviarEstadoCifrado(ativos, false)
				} else {
					// Regra 1 + Regra 2: nenhum aparelho na sala -> silêncio total exceto novas permissões.
					verificarPermissoesPush()
				}
				enviarFins()
			}
		}
	}()

	for {
		var msg relay.Mensagem
		if err := ws.LerJSON(&msg); err != nil {
			return nil
		}
		switch msg.Tipo {
		case "viewers":
			mu.Lock()
			viewers = append([]string(nil), msg.Devices...)
			ativos := append([]string(nil), viewers...)
			mu.Unlock()
			if len(ativos) > 0 {
				enviarEstadoCifrado(ativos, true)
			}
		case "action":
			go s.processarAcaoRelay(ws, msg)
		}
	}
}

// pushesFim monta o aviso «terminou» de uma sessão, um por aparelho com push. O texto visível é
// genérico; com a chave do aparelho, o título e o início da resposta vão em enc, que a extensão
// de notificação do iPhone decifra (como as decisões).
func (s *Servico) pushesFim(f fimSessao, host string) []relay.PedidoPush {
	titulo := f.Titulo
	if host != "" {
		titulo += " · " + host
	}
	resumo := []rune(strings.Join(strings.Fields(f.Resposta), " "))
	if len(resumo) > 140 {
		resumo = resumo[:140]
	}
	claro, _ := json.Marshal(map[string]string{"kind": "done", "title": titulo, "body": string(resumo)})
	var ps []relay.PedidoPush
	vistos := map[string]bool{}
	for _, ap := range s.aparelhos.copia() {
		if ap.PushToken == "" || vistos[ap.PushToken] {
			continue
		}
		vistos[ap.PushToken] = true
		p := relay.PedidoPush{Token: ap.PushToken, Titulo: titulo, Subtitulo: "Terminou",
			Corpo: "O agente terminou e espera o seu próximo pedido.", Categoria: "session-done", AgentID: f.SessionID}
		if len(resumo) > 0 && ap.ChavePublica != "" {
			if chave, err := relay.DerivarChaveSimetrica(s.aparelhos.chavePriv, ap.ChavePublica, s.aparelhos.servidor); err == nil {
				if enc, err := relay.Selar(chave, claro, []byte(ap.ID)); err == nil {
					p.Enc, p.Device, p.Server = enc, ap.ID, s.aparelhos.servidor
				}
			}
		}
		ps = append(ps, p)
	}
	return ps
}

func (s *Servico) processarAcaoRelay(ws *relay.ConnWS, msg relay.Mensagem) {
	_ = ws.EscreverJSON(s.responderAcaoRelay(msg))
}

// responderAcaoRelay abre o envelope do aparelho, executa a ação pela mesma lógica da rede local e
// devolve a resposta selada para o mesmo aparelho.
func (s *Servico) responderAcaoRelay(msg relay.Mensagem) relay.Mensagem {
	falha := func(status int) relay.Mensagem {
		return relay.Mensagem{Tipo: "action_reply", ReqID: msg.ReqID, Device: msg.Device, Status: status}
	}
	ap, ok := s.aparelhos.obterPorID(msg.Device)
	if !ok || ap.ChavePublica == "" {
		return falha(401)
	}
	chave, err := relay.DerivarChaveSimetrica(s.aparelhos.chavePriv, ap.ChavePublica, s.aparelhos.servidor)
	if err != nil {
		return falha(400)
	}
	claro, err := relay.Abrir(chave, msg.Payload, []byte(ap.ID))
	if err != nil {
		return falha(403)
	}
	var ped pedidoAcaoCifrada
	if json.Unmarshal(claro, &ped) != nil {
		return falha(400)
	}

	rec := httptest.NewRecorder()
	switch {
	case ped.Op == "catalog":
		req := httptest.NewRequest("GET", "http://127.0.0.1/v1/launch/catalog", nil)
		req.Header.Set("X-Towerman-Device-ID", ap.ID)
		s.redeCatalogo(rec, req)
	case ped.Op == "launch":
		req := httptest.NewRequest("POST", "http://127.0.0.1/v1/launch", bytes.NewReader(ped.Launch))
		req.Header.Set("X-Towerman-Device-ID", ap.ID)
		s.redeLancar(rec, req)
	case ped.Op == "metrics":
		dias := ""
		if ped.Days != 0 {
			dias = strconv.Itoa(ped.Days)
		}
		req := httptest.NewRequest("GET", "http://127.0.0.1/v1/metrics?days="+dias, nil)
		req.Header.Set("X-Towerman-Device-ID", ap.ID)
		s.redeMetricas(rec, req)
	case ped.Op != "" || !idSessao.MatchString(ped.AgentID):
		return falha(400)
	default:
		corpoReq, _ := json.Marshal(map[string]string{
			"promptId": ped.PromptID,
			"key":      ped.Key,
			"text":     ped.Text,
			"origin":   ped.Origin,
		})
		req := httptest.NewRequest("POST", "http://127.0.0.1/v1/agents/"+ped.AgentID+"/"+ped.Action, bytes.NewReader(corpoReq))
		req.SetPathValue("id", ped.AgentID)
		req.SetPathValue("acao", ped.Action)
		s.redeAcao(rec, req)
	}

	respCifrada, err := relay.Selar(chave, rec.Body.Bytes(), []byte(ap.ID))
	if err != nil {
		return falha(500)
	}
	return relay.Mensagem{
		Tipo:    "action_reply",
		ReqID:   msg.ReqID,
		Device:  ap.ID,
		Status:  rec.Code,
		Payload: respCifrada,
	}
}

func inicializarChaveAparelhos(home string) (*ecdh.PrivateKey, string) {
	priv, pub, err := relay.CarregarOuCriarChaveX25519(home)
	if err != nil {
		priv, pub, _ = relay.GerarParX25519()
	}
	return priv, pub
}
