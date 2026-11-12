package servico

// Porta de rede para o iPhone e o Apple Watch. Fala o protocolo /v1 do Agents Deck (estado,
// eventos, responder, focar, escrever), aceita só aparelhos pareados por código de uso único
// (o QR de «towerman pair») e anuncia-se na rede local por Bonjour, para o app reencontrar o
// computador quando o IP muda. O painel e os hooks continuam só em 127.0.0.1.

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/GeorgeMyller/towerman/internal/config"
)

const (
	validadeCodigo     = 2 * time.Minute
	falhasPorMinuto    = 10
	prefixoOpcao       = "opt:"
	nomeServicoBonjour = "_towerman._tcp"
)

// MARK: - Aparelhos pareados

type aparelho struct {
	ID           string `json:"id"`
	Nome         string `json:"nome"`
	Hash         string `json:"hash"` // sha256 do token; o token só existe no aparelho
	ChavePublica string `json:"pk,omitempty"`
	PushToken    string `json:"apns,omitempty"`
	CriadoEm     string `json:"criado_em"`
	VistoEm      string `json:"visto_em,omitempty"`
}

type aparelhos struct {
	mu        sync.Mutex
	caminho   string
	lista     []aparelho
	codigos   map[string]time.Time
	falhas    []time.Time
	servidor  string
	chavePriv *ecdh.PrivateKey
	chavePub  string
}

func novosAparelhos(home string) *aparelhos {
	a := &aparelhos{caminho: filepath.Join(home, "aparelhos.json"), codigos: map[string]time.Time{}}
	if dados, err := os.ReadFile(a.caminho); err == nil {
		json.Unmarshal(dados, &a.lista)
	}
	a.servidor = idServidor(home)
	a.chavePriv, a.chavePub = inicializarChaveAparelhos(home)
	return a
}

// idServidor é estável por máquina: o app reconhece o mesmo computador com outro IP.
func idServidor(home string) string {
	p := filepath.Join(home, "servidor-id")
	if dados, err := os.ReadFile(p); err == nil && len(bytes.TrimSpace(dados)) > 0 {
		return string(bytes.TrimSpace(dados))
	}
	id := aleatorio(8)
	config.EscreverAtomico(p, []byte(id), 0o600)
	return id
}

func aleatorio(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func hashToken(t string) string {
	h := sha256.Sum256([]byte(t))
	return hex.EncodeToString(h[:])
}

func (a *aparelhos) quantos() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.lista)
}

func (a *aparelhos) gravar() error {
	dados, _ := json.MarshalIndent(a.lista, "", "  ")
	return config.EscreverAtomico(a.caminho, dados, 0o600)
}

// novoCodigo cria um código de 8 caracteres, de uso único, válido 2 minutos.
func (a *aparelhos) novoCodigo(agora time.Time) (string, time.Time) {
	b := make([]byte, 5)
	rand.Read(b)
	codigo := base32.StdEncoding.EncodeToString(b)[:8]
	a.mu.Lock()
	defer a.mu.Unlock()
	for c, exp := range a.codigos {
		if agora.After(exp) {
			delete(a.codigos, c)
		}
	}
	exp := agora.Add(validadeCodigo)
	a.codigos[codigo] = exp
	return codigo, exp
}

var errCodigo = fmt.Errorf("código inválido ou expirado")

// parear troca um código válido por um token novo, só deste aparelho.
func (a *aparelhos) parear(codigo, nome, chavePub, pushToken string, agora time.Time) (string, aparelho, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	recentes := a.falhas[:0]
	for _, f := range a.falhas {
		if agora.Sub(f) < time.Minute {
			recentes = append(recentes, f)
		}
	}
	a.falhas = recentes
	if len(a.falhas) >= falhasPorMinuto {
		return "", aparelho{}, fmt.Errorf("demasiadas tentativas; espera um minuto")
	}
	codigo = strings.ToUpper(strings.TrimSpace(codigo))
	exp, ok := a.codigos[codigo]
	if !ok || agora.After(exp) {
		a.falhas = append(a.falhas, agora)
		return "", aparelho{}, errCodigo
	}
	delete(a.codigos, codigo)
	nome = strings.TrimSpace(nome)
	if nome == "" {
		nome = "iPhone"
	}
	token := aleatorio(32)
	ap := aparelho{
		ID:           aleatorio(4),
		Nome:         nome,
		Hash:         hashToken(token),
		ChavePublica: strings.TrimSpace(chavePub),
		PushToken:    strings.TrimSpace(pushToken),
		CriadoEm:     agora.UTC().Format(time.RFC3339),
	}
	a.lista = append(a.lista, ap)
	if err := a.gravar(); err != nil {
		a.lista = a.lista[:len(a.lista)-1]
		return "", aparelho{}, err
	}
	return token, ap, nil
}

func (a *aparelhos) obterPorID(id string) (aparelho, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, ap := range a.lista {
		if ap.ID == id {
			return ap, true
		}
	}
	return aparelho{}, false
}

func (a *aparelhos) atualizarPushToken(id, pushToken string) {
	pushToken = strings.TrimSpace(pushToken)
	if id == "" || pushToken == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := range a.lista {
		if a.lista[i].ID == id && a.lista[i].PushToken != pushToken {
			a.lista[i].PushToken = pushToken
			_ = a.gravar()
			return
		}
	}
}

// comPush devolve os aparelhos com token de push, um por token (o mesmo iPhone pareado duas vezes
// só recebe uma notificação).
func (a *aparelhos) comPush() []aparelho {
	a.mu.Lock()
	defer a.mu.Unlock()
	var lista []aparelho
	vistos := make(map[string]struct{})
	for _, ap := range a.lista {
		if _, ja := vistos[ap.PushToken]; ap.PushToken != "" && !ja {
			vistos[ap.PushToken] = struct{}{}
			lista = append(lista, ap)
		}
	}
	return lista
}

func (a *aparelhos) tokensPush() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var lista []string
	vistos := make(map[string]struct{})
	for _, ap := range a.lista {
		if ap.PushToken != "" {
			if _, ja := vistos[ap.PushToken]; !ja {
				vistos[ap.PushToken] = struct{}{}
				lista = append(lista, ap.PushToken)
			}
		}
	}
	return lista
}

func (a *aparelhos) autenticar(token string, agora time.Time) (aparelho, bool) {
	if token == "" {
		return aparelho{}, false
	}
	h := hashToken(token)
	a.mu.Lock()
	defer a.mu.Unlock()
	for i, ap := range a.lista {
		if subtle.ConstantTimeCompare([]byte(ap.Hash), []byte(h)) == 1 {
			// Gravar o "visto" no máximo uma vez por hora: não escrever no disco a cada pedido.
			if v, err := time.Parse(time.RFC3339, ap.VistoEm); err != nil || agora.Sub(v) > time.Hour {
				a.lista[i].VistoEm = agora.UTC().Format(time.RFC3339)
				a.gravar()
			}
			return ap, true
		}
	}
	return aparelho{}, false
}

func (a *aparelhos) revogar(chave string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i, ap := range a.lista {
		if ap.ID == chave || strings.EqualFold(ap.Nome, chave) {
			a.lista = append(a.lista[:i], a.lista[i+1:]...)
			a.gravar()
			return true
		}
	}
	return false
}

func (a *aparelhos) copia() []aparelho {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]aparelho, len(a.lista))
	copy(out, a.lista)
	for i := range out {
		out[i].Hash = ""
	}
	return out
}

// MARK: - Identidade e endereços

func nomeMaquina() string {
	if runtime.GOOS == "darwin" {
		if out, err := exec.Command("scutil", "--get", "ComputerName").Output(); err == nil {
			if n := strings.TrimSpace(string(out)); n != "" {
				return n
			}
		}
	}
	h, _ := os.Hostname()
	return strings.Split(h, ".")[0]
}

// tailscaleCLI encontra o comando do Tailscale: no PATH (Linux, Homebrew) ou dentro do app do
// Mac, que não o põe no PATH.
func tailscaleCLI() string {
	if p, err := exec.LookPath("tailscale"); err == nil {
		return p
	}
	if runtime.GOOS == "darwin" {
		p := "/Applications/Tailscale.app/Contents/MacOS/Tailscale"
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

var cacheEnderecos struct {
	sync.Mutex
	lista []string
	em    time.Time
}

// enderecos lista por onde o iPhone pode chegar a este computador: nome .local (estável com
// IP novo), IPs da rede (incluindo Tailscale, 100.64/10) e o nome MagicDNS do Tailscale. Vai em
// cada estado, por isso a lista fica 30 s em cache (o tailscale status é um processo).
func enderecos() []string {
	c := &cacheEnderecos
	c.Lock()
	defer c.Unlock()
	if c.lista == nil || time.Since(c.em) > 30*time.Second {
		c.lista, c.em = lerEnderecos(), time.Now()
	}
	lista := append([]string(nil), c.lista...)
	// O túnel (fora de casa) vai no fim: em casa, os endereços locais respondem primeiro.
	if u := enderecoTunel(); u != "" {
		lista = append(lista, u)
	}
	return lista
}

// esquecerEnderecos força a próxima leitura (ex.: o túnel mudou de endereço).
func esquecerEnderecos() {
	cacheEnderecos.Lock()
	cacheEnderecos.lista = nil
	cacheEnderecos.Unlock()
}

func lerEnderecos() []string {
	var nomes, ips []string
	if runtime.GOOS == "darwin" {
		if out, err := exec.Command("scutil", "--get", "LocalHostName").Output(); err == nil {
			if n := strings.TrimSpace(string(out)); n != "" {
				nomes = append(nomes, n+".local")
			}
		}
	} else if h, err := os.Hostname(); err == nil {
		nomes = append(nomes, strings.Split(h, ".")[0]+".local")
	}
	if ts := tailscaleCLI(); ts != "" {
		if out, err := exec.Command(ts, "status", "--json").Output(); err == nil {
			var st struct {
				Self struct{ DNSName string }
			}
			if json.Unmarshal(out, &st) == nil && st.Self.DNSName != "" {
				nomes = append(nomes, strings.TrimSuffix(st.Self.DNSName, "."))
			}
		}
	}
	ifs, _ := net.Interfaces()
	for _, i := range ifs {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := i.Addrs()
		for _, ad := range addrs {
			if ipn, ok := ad.(*net.IPNet); ok {
				if ip4 := ipn.IP.To4(); ip4 != nil && !ip4.IsLinkLocalUnicast() {
					ips = append(ips, ip4.String())
				}
			}
		}
	}
	sort.Strings(ips)
	return append(nomes, ips...)
}

// MARK: - Rotas de rede

func (s *Servico) rotasRede() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		escreverJSON(w, map[string]any{"ok": true, "id": s.aparelhos.servidor})
	})
	mux.HandleFunc("POST /v1/pair", s.redeParear)
	autenticado := func(h http.HandlerFunc) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			ap, ok := s.aparelhos.autenticar(token, time.Now())
			if !ok {
				http.Error(w, `{"error":"aparelho não pareado ou revogado"}`, http.StatusUnauthorized)
				return
			}
			r.Header.Set("X-Towerman-Device-ID", ap.ID)
			h(w, r)
		})
	}
	mux.Handle("GET /v1/state", autenticado(s.redeEstado))
	mux.Handle("GET /v1/events", autenticado(s.redeEventos))
	mux.Handle("POST /v1/agents/{id}/{acao}", autenticado(s.redeAcao))
	mux.Handle("POST /v1/push-token", autenticado(s.redePushToken))
	mux.Handle("GET /v1/launch/catalog", autenticado(s.redeCatalogo))
	mux.Handle("POST /v1/launch", autenticado(s.redeLancar))
	mux.Handle("GET /v1/metrics", autenticado(s.redeMetricas))
	return mux
}

func (s *Servico) redeCatalogo(w http.ResponseWriter, r *http.Request) {
	c, err := s.catalogoLancamento(r.Context())
	if err != nil {
		http.Error(w, `{"error":"inventário indisponível"}`, http.StatusInternalServerError)
		return
	}
	escreverJSON(w, c)
}

// redeLancar abre uma sessão nova a pedido de um aparelho (iPhone, relógio pelo iPhone, Mac).
func (s *Servico) redeLancar(w http.ResponseWriter, r *http.Request) {
	var p pedidoLancamento
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&p); err != nil {
		http.Error(w, `{"error":"pedido inválido"}`, http.StatusBadRequest)
		return
	}
	p.lancadaPor = r.Header.Get("X-Towerman-Device-ID")
	res := s.lancarSessao(r.Context(), p)
	w.Header().Set("Content-Type", "application/json")
	if res.Erro != "" {
		corpo := map[string]string{"error": res.Erro}
		if res.Worktree != "" {
			corpo["worktree"] = res.Worktree
		}
		w.WriteHeader(res.Codigo)
		json.NewEncoder(w).Encode(corpo)
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "session_id": res.ID, "name": res.Nome,
		"worktree": res.Worktree, "branch": res.Branch})
}

func (s *Servico) redePushToken(w http.ResponseWriter, r *http.Request) {
	var pedido struct {
		PushToken string `json:"push_token"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&pedido) != nil || pedido.PushToken == "" {
		http.Error(w, `{"error":"token inválido"}`, http.StatusBadRequest)
		return
	}
	s.aparelhos.atualizarPushToken(r.Header.Get("X-Towerman-Device-ID"), pedido.PushToken)
	escreverJSON(w, map[string]bool{"ok": true})
}

func (s *Servico) redeParear(w http.ResponseWriter, r *http.Request) {
	var pedido struct {
		Codigo    string `json:"code"`
		Aparelho  string `json:"device"`
		DevicePK  string `json:"device_pk"`
		PushToken string `json:"push_token"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&pedido) != nil {
		http.Error(w, `{"error":"pedido inválido"}`, http.StatusBadRequest)
		return
	}
	// O pareamento só se faz na rede local: pelo túnel (aberto à internet) só passam aparelhos
	// que já têm token.
	if r.Header.Get("Cf-Connecting-Ip") != "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "pareie na mesma rede Wi-Fi do computador"})
		return
	}
	token, ap, err := s.aparelhos.parear(pedido.Codigo, pedido.Aparelho, pedido.DevicePK, pedido.PushToken, time.Now())
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	log.Printf("towerman: aparelho pareado: %s (%s)", ap.Nome, ap.ID)
	escreverJSON(w, map[string]any{"token": token, "device": ap.ID,
		"server": map[string]string{"id": s.aparelhos.servidor, "name": nomeMaquina(), "pk": s.aparelhos.chavePub}})
}

// local executa uma rota do serviço local por dentro, com o token do painel: a rede reutiliza
// a mesma lógica (fila de escrita, regras, extensão) sem a duplicar. Os cabeçalhos extra levam
// quem pediu (aparelho e origem) até às rotas que o gravam.
func (s *Servico) local(metodo, caminho string, corpo any, extra ...http.Header) (int, []byte) {
	var b bytes.Buffer
	if corpo != nil {
		json.NewEncoder(&b).Encode(corpo)
	}
	req := httptest.NewRequest(metodo, "http://127.0.0.1"+caminho, &b)
	for _, h := range extra {
		for k, v := range h {
			req.Header[k] = v
		}
	}
	req.Header.Set("Authorization", "Bearer "+s.op.Token)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

type sessaoVisao struct {
	ID       string `json:"id"`
	Nome     string `json:"nome"`
	Agente   string `json:"agente"`
	Branch   string `json:"branch"`
	Estado   string `json:"estado"`
	Resumo   string `json:"resumo"`
	Cwd      string `json:"cwd"`
	Iniciada string `json:"iniciada_em"`
	Visto    string `json:"visto_em"`
}

type permissaoVisao struct {
	ID         int64           `json:"id"`
	SessionID  string          `json:"session_id"`
	Ferramenta string          `json:"ferramenta"`
	Resumo     string          `json:"resumo"`
	PedidaEm   string          `json:"pedida_em"`
	Entrada    json.RawMessage `json:"entrada"`
}

type perguntas struct {
	Questions []struct {
		Question string `json:"question"`
		Header   string `json:"header"`
		Options  []struct {
			Label string `json:"label"`
		} `json:"options"`
	} `json:"questions"`
}

var estadosV1 = map[string]string{"ativa": "running", "a_espera": "waiting", "parada": "idle", "terminada": "exited"}

// estadoV1 traduz sessões e pedidos para o State do protocolo v1 do Agents Deck.
func (s *Servico) estadoV1() map[string]any {
	host := nomeMaquina()
	var repos []struct {
		Worktrees []struct {
			Sessoes []sessaoVisao `json:"sessoes"`
		} `json:"worktrees"`
	}
	_, b := s.local("GET", "/v1/sessoes", nil)
	json.Unmarshal(b, &repos)
	var painel struct {
		Permissoes []permissaoVisao `json:"permissoes"`
	}
	_, b = s.local("GET", "/v1/painel", nil)
	json.Unmarshal(b, &painel)
	pendentes := map[string]permissaoVisao{}
	for _, p := range painel.Permissoes {
		if _, ja := pendentes[p.SessionID]; !ja {
			pendentes[p.SessionID] = p
		}
	}
	agentes := []map[string]any{}
	for _, r := range repos {
		for _, wt := range r.Worktrees {
			for _, se := range wt.Sessoes {
				a := agenteV1(se, host, pendentes)
				if q, ok, err := s.quadro.ObterSessao(se.ID); err == nil && ok {
					if ctx := contextoDe(q.TranscriptPath); ctx.Pedido != "" || ctx.Resposta != "" {
						a["lastPrompt"], a["lastReply"] = nulo(ctx.Pedido), nulo(ctx.Resposta)
					}
					// Uma permissão pendente vem primeiro; sem ela, a decisão em texto livre ocupa o «waiting».
					if a["waiting"] == nil {
						if d, aberta, err := s.quadro.DecisaoAberta(se.ID); err == nil && aberta {
							entrega, motivo := s.entregavel(q)
							a["waiting"], a["status"], a["statusSince"] = decisaoV1(d, entrega, motivo), "waiting", d.CriadaEm
						}
					}
					if q.LancadaPor != "" {
						nome := "outro aparelho"
						if ap, ok := s.aparelhos.obterPorID(q.LancadaPor); ok {
							nome = ap.Nome
						}
						a["launchedBy"] = nome
					}
				}
				// O último aviso «terminou» (pessoa longe): o iPhone e o relógio avisam quando muda.
				if d := s.fimDe(se.ID); d != "" {
					a["doneAt"] = d
				}
				// Aberta pelo Towerman e ainda a correr: o aparelho pode terminá-la.
				if s.obterHeadless(se.ID) != nil {
					a["capabilities"] = append(a["capabilities"].([]string), "terminate")
				}
				agentes = append(agentes, a)
			}
		}
	}
	sort.SliceStable(agentes, func(i, j int) bool { return agentes[i]["startedAt"].(string) < agentes[j]["startedAt"].(string) })
	for i := range agentes {
		agentes[i]["slot"] = i
	}
	return map[string]any{
		"server": map[string]any{"name": host, "id": s.aparelhos.servidor, "version": "towerman",
			"time": time.Now().UTC().Format(time.RFC3339), "hosts": enderecos()},
		"agents":       agentes,
		"capabilities": []string{"launch", "metrics"},
	}
}

func nulo(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func agenteV1(se sessaoVisao, host string, pendentes map[string]permissaoVisao) map[string]any {
	pasta := filepath.Base(strings.TrimRight(se.Cwd, "/"))
	titulo := se.Nome
	if titulo == "" {
		titulo = pasta
	}
	estado := estadosV1[se.Estado]
	if estado == "" {
		estado = "idle"
	}
	provedor := se.Agente
	if provedor == "" {
		provedor = "claude"
	}
	desde := se.Visto
	var espera any
	if p, ok := pendentes[se.ID]; ok {
		estado = "waiting"
		desde = p.PedidaEm
		espera = pedidoV1(p)
	}
	// Todos respondem daqui: Claude e Codex pelo PermissionRequest; o agy e o IDE quando a
	// pessoa está longe do computador (o hook passa o run_command a pedido de permissão).
	capacidades := []string{"answer", "focus", "input"}
	return map[string]any{
		"id": se.ID, "slot": 0, "title": titulo, "aiTitle": se.Resumo, "tool": provedor, "status": estado,
		"statusSince": desde, "cwd": se.Cwd, "folder": pasta, "branch": se.Branch, "model": "", "modelLabel": "",
		"activity": nil, "lastPrompt": nil, "waiting": espera, "host": host, "provider": provedor,
		"capabilities": capacidades, "startedAt": se.Iniciada, "updatedAt": se.Visto,
	}
}

func pedidoV1(p permissaoVisao) map[string]any {
	var e perguntas
	json.Unmarshal(p.Entrada, &e)
	if p.Ferramenta == "AskUserQuestion" && len(e.Questions) > 0 {
		q := e.Questions[0]
		opcoes := []map[string]any{}
		for i, o := range q.Options {
			opcoes = append(opcoes, map[string]any{"key": prefixoOpcao + strconv.Itoa(i), "label": o.Label, "primary": i == 0})
		}
		titulo := q.Header
		if titulo == "" {
			titulo = "Pergunta"
		}
		return map[string]any{"id": strconv.FormatInt(p.ID, 10), "kind": "question", "title": titulo,
			"detail": q.Question, "context": "", "options": opcoes}
	}
	return map[string]any{"id": strconv.FormatInt(p.ID, 10), "kind": "permission", "title": p.Ferramenta + "?",
		"detail": p.Resumo, "context": "", "options": []map[string]any{
			{"key": "allow", "label": "Permitir", "primary": true}, {"key": "deny", "label": "Negar"}}}
}

// redeEstado devolve o estado. Com ?espera=<versão> é um long-poll: se o estado ainda é essa
// versão, segura o pedido até ele mudar (ou 25 s). Serve o túnel da Cloudflare, que retém o
// stream de eventos até ao fim da resposta.
func (s *Servico) redeEstado(w http.ResponseWriter, r *http.Request) {
	espera, longPoll := r.URL.Query()["espera"]
	var mudou <-chan struct{}
	if longPoll {
		m, sair := s.painel.Difusor.SubscreverRemoto(s.urgenciaV1)
		defer sair()
		mudou = m
	}
	estado := s.estadoV1()
	v := versaoEstado(estado)
	// Nem toda a mudança do quadro muda o estado /v1: espera até a versão mudar de facto.
	limite := time.After(25 * time.Second)
	for longPoll && len(espera) > 0 && espera[0] == v {
		select {
		case <-mudou:
			estado = s.estadoV1()
			v = versaoEstado(estado)
			continue
		case <-limite:
		case <-r.Context().Done():
			return
		case <-s.fechado:
		}
		break
	}
	w.Header().Set("X-Towerman-Versao", v)
	w.Header().Set("Cache-Control", "no-store")
	escreverJSON(w, estado)
}

// versaoEstado resume o estado sem a hora (que muda sempre): agentes e endereços.
func versaoEstado(estado map[string]any) string {
	srv, _ := estado["server"].(map[string]any)
	b, _ := json.Marshal([]any{estado["agents"], srv["hosts"]})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:8])
}

// urgenciaV1 é a assinatura do que acorda logo os aparelhos: agentes que surgem ou saem,
// estado, pedido em espera e aviso «terminou». Ferramenta, atividade, contexto e custo esperam pelo difusor.
func (s *Servico) urgenciaV1() string { return assinaturaUrgente(s.estadoV1()) }

func assinaturaUrgente(estado map[string]any) string {
	agentes, _ := estado["agents"].([]map[string]any)
	resumo := make([]any, 0, len(agentes))
	for _, a := range agentes {
		var espera any
		if w, ok := a["waiting"].(map[string]any); ok {
			espera = w["id"]
		}
		resumo = append(resumo, []any{a["id"], a["status"], espera, a["doneAt"]})
	}
	srv, _ := estado["server"].(map[string]any)
	b, _ := json.Marshal([]any{resumo, srv["hosts"]})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:8])
}

func (s *Servico) redeEventos(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, `{"error":"sem streaming"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	mudou, sair := s.painel.Difusor.SubscreverRemoto(s.urgenciaV1)
	defer sair()
	enviar := func() {
		b, _ := json.Marshal(s.estadoV1())
		fmt.Fprintf(w, "event: state\ndata: %s\n\n", b)
		fl.Flush()
	}
	enviar()
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.fechado:
			return
		case <-mudou:
			enviar()
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		}
	}
}

var idSessao = regexp.MustCompile(`^[A-Za-z0-9-]{1,80}$`)

func (s *Servico) redeAcao(w http.ResponseWriter, r *http.Request) {
	id, acao := r.PathValue("id"), r.PathValue("acao")
	if !idSessao.MatchString(id) {
		http.Error(w, `{"error":"agente inválido"}`, http.StatusBadRequest)
		return
	}
	var corpo struct {
		PromptID string `json:"promptId"`
		Key      string `json:"key"`
		Text     string `json:"text"`
		// Origin: iphone, watch ou mac; só alimenta as métricas.
		Origin string `json:"origin"`
	}
	json.NewDecoder(http.MaxBytesReader(w, r.Body, 70*1024)).Decode(&corpo)
	r.Header.Set(cabecalhoOrigem, origemDoCliente(corpo.Origin))
	quem := http.Header{}
	quem.Set(cabecalhoAparelho, r.Header.Get(cabecalhoAparelho))
	quem.Set(cabecalhoOrigem, r.Header.Get(cabecalhoOrigem))
	responder := func(codigo int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(codigo)
		json.NewEncoder(w).Encode(v)
	}
	erro := func(b []byte, padrao string) string {
		var e struct {
			Erro string `json:"erro"`
		}
		if json.Unmarshal(b, &e) == nil && e.Erro != "" {
			return e.Erro
		}
		return padrao
	}
	switch acao {
	case "terminate":
		if !s.terminarHeadless(id) {
			responder(409, map[string]string{"error": "esta sessão não foi aberta pelo Towerman ou já terminou"})
			return
		}
		responder(200, map[string]bool{"ok": true})
	case "focus":
		codigo, b := s.local("POST", "/v1/sessoes/"+id+"/focar", nil)
		var f struct {
			OK   bool   `json:"ok"`
			Como string `json:"como"`
			Erro string `json:"erro"`
		}
		json.Unmarshal(b, &f)
		if codigo == 200 && f.OK {
			responder(200, map[string]any{"ok": true, "via": f.Como})
			return
		}
		if f.Erro == "" {
			f.Erro = "não encontrei o terminal desta sessão"
		}
		responder(409, map[string]string{"error": f.Erro})
	case "input":
		texto := strings.TrimSpace(corpo.Text)
		if texto == "" {
			responder(400, map[string]string{"error": "texto vazio"})
			return
		}
		codigo, b := s.local("POST", "/v1/sessoes/"+id+"/mensagens", map[string]string{"texto": texto}, quem)
		if codigo != 200 {
			responder(409, map[string]string{"error": erro(b, "não foi possível enviar")})
			return
		}
		var m struct {
			Estado  string `json:"estado"`
			Caminho string `json:"caminho"`
			Motivo  string `json:"motivo"`
		}
		json.Unmarshal(b, &m)
		responder(200, map[string]string{"state": m.Estado, "via": m.Caminho, "reason": m.Motivo})
	case "answer":
		if strings.HasPrefix(corpo.PromptID, prefixoDecisao) {
			s.responderDecisao(w, r, id, corpo.PromptID, corpo.Key, responder)
			return
		}
		s.redeResponder(w, id, corpo.PromptID, corpo.Key, quem, responder, erro)
	case "interrupt":
		responder(501, map[string]string{"error": "o towerman ainda não interrompe sessões"})
	default:
		responder(404, map[string]string{"error": "ação desconhecida"})
	}
}

func (s *Servico) redeResponder(w http.ResponseWriter, sessao, promptID, chave string, quem http.Header,
	responder func(int, any), erro func([]byte, string) string) {
	var painel struct {
		Permissoes []permissaoVisao `json:"permissoes"`
	}
	_, b := s.local("GET", "/v1/painel", nil)
	json.Unmarshal(b, &painel)
	var p *permissaoVisao
	for i := range painel.Permissoes {
		if strconv.FormatInt(painel.Permissoes[i].ID, 10) == promptID && painel.Permissoes[i].SessionID == sessao {
			p = &painel.Permissoes[i]
		}
	}
	if p == nil {
		responder(409, map[string]string{"error": "o pedido já não é o atual"})
		return
	}
	var decisao map[string]any
	switch {
	case chave == "allow" || chave == "deny":
		decisao = map[string]any{"decisao": chave}
	case strings.HasPrefix(chave, prefixoOpcao):
		var e perguntas
		json.Unmarshal(p.Entrada, &e)
		i, err := strconv.Atoi(strings.TrimPrefix(chave, prefixoOpcao))
		if err != nil || len(e.Questions) == 0 || i < 0 || i >= len(e.Questions[0].Options) {
			responder(400, map[string]string{"error": "opção desconhecida"})
			return
		}
		q := e.Questions[0]
		decisao = map[string]any{"decisao": "allow", "respostas": map[string]string{q.Question: q.Options[i].Label}}
	default:
		responder(400, map[string]string{"error": "opção desconhecida"})
		return
	}
	codigo, b := s.local("POST", "/v1/permissoes/"+promptID, decisao, quem)
	if codigo == 200 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if codigo == 410 {
		codigo = 409
	}
	responder(codigo, map[string]string{"error": erro(b, "falhou")})
}

// MARK: - Pareamento (rotas locais, protegidas pelo token do painel)

// urlPareamento é o conteúdo do QR: o app abre-o pela câmara ou pelo leitor interno.
func (s *Servico) urlPareamento(codigo string) string {
	v := url.Values{}
	v.Set("v", "1")
	v.Set("id", s.aparelhos.servidor)
	v.Set("nome", nomeMaquina())
	v.Set("porta", strconv.Itoa(s.PortaRede()))
	v.Set("codigo", codigo)
	v.Set("pk", s.aparelhos.chavePub)
	v.Set("h", strings.Join(enderecos(), ","))
	return "towerman://pair?" + v.Encode()
}

func (s *Servico) criarPareamento(w http.ResponseWriter, r *http.Request) {
	if s.PortaRede() == 0 {
		http.Error(w, `{"erro":"a porta de rede está desligada (TOWERMAN_PORTA_REDE=0)"}`, http.StatusConflict)
		return
	}
	codigo, exp := s.aparelhos.novoCodigo(time.Now())
	escreverJSON(w, map[string]any{"codigo": codigo, "url": s.urlPareamento(codigo), "expira_em": exp.UTC().Format(time.RFC3339),
		"nome": nomeMaquina(), "porta": s.PortaRede(), "enderecos": enderecos()})
}

func (s *Servico) listarAparelhos(w http.ResponseWriter, r *http.Request) {
	escreverJSON(w, s.aparelhos.copia())
}

func (s *Servico) revogarAparelho(w http.ResponseWriter, r *http.Request) {
	if !s.aparelhos.revogar(r.PathValue("id")) {
		http.Error(w, `{"erro":"aparelho desconhecido"}`, http.StatusNotFound)
		return
	}
	escreverJSON(w, map[string]bool{"ok": true})
}

// MARK: - Arranque da porta de rede

// PortaRede devolve a porta de rede efetiva, ou 0 quando está desligada.
func (s *Servico) PortaRede() int { return int(s.portaRede.Load()) }

func (s *Servico) iniciarRede(ctx context.Context) (*http.Server, error) {
	if s.op.PortaRede == 0 {
		return nil, nil
	}
	porta := s.op.PortaRede
	if porta < 0 {
		porta = 0 // testes: porta livre qualquer
	}
	ln, err := net.Listen("tcp", net.JoinHostPort("", strconv.Itoa(porta)))
	if err != nil {
		return nil, fmt.Errorf("porta de rede %d: %w", porta, err)
	}
	s.portaRede.Store(int32(ln.Addr().(*net.TCPAddr).Port))
	srv := &http.Server{Handler: s.rotasRede(), ReadHeaderTimeout: 5 * time.Second}
	go srv.Serve(ln)
	if s.op.PortaRede > 0 {
		anunciarBonjour(ctx, nomeMaquina(), s.PortaRede(), s.aparelhos.servidor)
	}
	s.iniciarTunel(ctx)
	return srv, nil
}

// anunciarBonjour publica _towerman._tcp com o id do servidor, pelo dns-sd (macOS, e Windows com o
// Bonjour da Apple instalado) ou pelo avahi-publish (Linux). Sem nenhum deles, o app usa os
// endereços guardados no pareamento.
func anunciarBonjour(ctx context.Context, nome string, porta int, id string) {
	var cmd *exec.Cmd
	switch {
	case runtime.GOOS == "darwin":
		cmd = exec.CommandContext(ctx, "dns-sd", "-R", "Towerman "+nome, nomeServicoBonjour, "local", strconv.Itoa(porta), "id="+id)
	case runtime.GOOS == "windows":
		if _, err := exec.LookPath("dns-sd"); err != nil {
			return
		}
		cmd = exec.CommandContext(ctx, "dns-sd", "-R", "Towerman "+nome, nomeServicoBonjour, "local", strconv.Itoa(porta), "id="+id)
	default:
		if _, err := exec.LookPath("avahi-publish-service"); err != nil {
			return
		}
		cmd = exec.CommandContext(ctx, "avahi-publish-service", "Towerman "+nome, nomeServicoBonjour, strconv.Itoa(porta), "id="+id)
	}
	if err := cmd.Start(); err != nil {
		log.Printf("towerman: sem anúncio Bonjour: %v", err)
		return
	}
	go cmd.Wait()
}
