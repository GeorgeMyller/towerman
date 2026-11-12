package servico

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/GeorgeMyller/towerman/internal/config"
	"github.com/GeorgeMyller/towerman/internal/gitinfo"
	"github.com/GeorgeMyller/towerman/internal/hook"
	"github.com/GeorgeMyller/towerman/internal/painel"
	"github.com/GeorgeMyller/towerman/internal/processo"
	"github.com/GeorgeMyller/towerman/internal/quadro"
	"github.com/GeorgeMyller/towerman/internal/sessoesclaude"
)

const capacidadeFila = 10000

type Opcoes struct {
	Porta     int
	Token     string
	Home      string
	ClaudeDir string

	IntervaloDiario   time.Duration
	IntervaloVigia    time.Duration
	IntervaloRetencao time.Duration
	Retencao          time.Duration
	EsperaDiario      time.Duration
	// Tetos para onde o diário e a vigia abrandam quando não há trabalho.
	IntervaloDiarioMax time.Duration
	IntervaloVigiaMax  time.Duration
	// PresencaAntigravity é a pasta das marcas de presença do Antigravity; vazio usa
	// ~/.gemini/antigravity/presence (os testes apontam para uma pasta temporária).
	PresencaAntigravity string
	// PortaRede é a porta para o iPhone e o relógio (todas as interfaces). 0 desliga; <0 escolhe
	// uma porta livre (testes).
	PortaRede int
}

type Servico struct {
	op        Opcoes
	quadro    *quadro.Quadro
	proc      *Processador
	painel    *painel.Painel
	fila      chan func()
	inicio    time.Time
	ingerindo atomic.Bool
	porta     atomic.Int32
	ext       *hubExtensao
	esperas   sync.Map
	fechado   chan struct{}
	fecharUma sync.Once
	despertar chan struct{}

	permissoesNoPainel atomic.Bool
	aparelhos          *aparelhos
	portaRede          atomic.Int32
	mux                http.Handler
	headless           sync.Map
	lancamentos        cacheLancamentos
	inventario         cacheInventario
	avisosMu           sync.Mutex
	avisos             []string
	diarioMu           sync.Mutex
	finsMu             sync.Mutex
	fins               []fimSessao
	// fimEm guarda, por sessão, a hora do último aviso «terminou»; vai no estado como doneAt.
	fimEm map[string]string
}

func Novo(op Opcoes) (*Servico, error) {
	if op.IntervaloDiario == 0 {
		op.IntervaloDiario = 30 * time.Second
	}
	if op.IntervaloVigia == 0 {
		op.IntervaloVigia = 60 * time.Second
	}
	if op.IntervaloDiarioMax == 0 {
		op.IntervaloDiarioMax = 5 * time.Minute
	}
	if op.IntervaloVigiaMax == 0 {
		op.IntervaloVigiaMax = 5 * time.Minute
	}
	if op.IntervaloRetencao == 0 {
		op.IntervaloRetencao = 24 * time.Hour
	}
	if op.Retencao == 0 {
		op.Retencao = 30 * 24 * time.Hour
	}
	if op.EsperaDiario == 0 {
		op.EsperaDiario = 100 * time.Millisecond
	}
	q, err := quadro.Abrir(filepath.Join(op.Home, "quadro.db"))
	if err != nil {
		return nil, err
	}
	s := &Servico{
		op:      op,
		quadro:  q,
		fila:    make(chan func(), capacidadeFila),
		inicio:  time.Now(),
		ext:     novoHub(),
		fechado: make(chan struct{}),

		despertar: make(chan struct{}, 1),
	}
	s.proc = &Processador{Quadro: q, Git: gitinfo.Novo(), ClaudeDir: op.ClaudeDir, Agora: time.Now,
		// Com um aparelho pareado, o pedido espera sempre: o iPhone pode estar a dormir agora e
		// receber o pedido daqui a pouco. O terminal mostra o mesmo pedido em paralelo.
		PermissoesNoPainel: func() bool {
			return (s.permissoesNoPainel.Load() && s.painel.Difusor.Subscritores() > 0) || s.aparelhos.quantos() > 0
		},
		ModoRegra2:      func() string { return config.LerPreferenciasEm(op.Home).Regra2 },
		DecisoesLigadas: func() bool { return config.LerPreferenciasEm(op.Home).DecisoesLigadas() }}
	s.painel = painel.Novo(q, op.Token, s.Porta)
	s.aparelhos = novosAparelhos(op.Home)
	s.mux = s.rotas()
	return s, nil
}

// Porta devolve a porta efetiva; útil quando Opcoes.Porta é 0.
func (s *Servico) Porta() int { return int(s.porta.Load()) }

func (s *Servico) Quadro() *quadro.Quadro { return s.quadro }

// Executar corre até ctx terminar; pronto recebe a porta quando o serviço já aceita pedidos.
func (s *Servico) Executar(ctx context.Context, pronto func(porta int)) error {
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(s.op.Porta)))
	if err != nil {
		s.quadro.Fechar()
		return fmt.Errorf("porta %d ocupada ou indisponível: %w", s.op.Porta, err)
	}
	s.porta.Store(int32(ln.Addr().(*net.TCPAddr).Port))

	escritorFeito := make(chan struct{})
	go func() {
		defer close(escritorFeito)
		for tarefa := range s.fila {
			tarefa()
			s.painel.Difusor.Mudou()
		}
	}()

	srv := &http.Server{Handler: s.mux, ReadHeaderTimeout: 5 * time.Second}
	srv.RegisterOnShutdown(s.painel.Fechar)
	srv.RegisterOnShutdown(func() { s.fecharUma.Do(func() { close(s.fechado) }) })
	servidorFeito := make(chan error, 1)
	go func() { servidorFeito <- srv.Serve(ln) }()

	var fundo sync.WaitGroup
	ctxFundo, pararFundo := context.WithCancel(ctx)
	fundo.Add(1)
	go func() {
		defer fundo.Done()
		s.correrFundo(ctxFundo)
	}()

	// A porta de rede é opcional: sem ela, o painel e os hooks continuam a funcionar.
	srvRede, err := s.iniciarRede(ctxFundo)
	if err != nil {
		log.Printf("towerman: %v (o iPhone não vai conseguir ligar)", err)
	}

	if pronto != nil {
		pronto(s.Porta())
	}

	var errServidor error
	select {
	case <-ctx.Done():
	case errServidor = <-servidorFeito:
	}
	pararFundo()
	fundo.Wait()
	desligar, cancelar := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelar()
	if srvRede != nil {
		srvRede.Shutdown(desligar)
	}
	srv.Shutdown(desligar)
	close(s.fila)
	<-escritorFeito
	s.quadro.Fechar()
	if errServidor != nil && !errors.Is(errServidor, http.ErrServerClosed) {
		return errServidor
	}
	return nil
}

func (s *Servico) enfileirar(ctx context.Context, tarefa func()) {
	select {
	case s.fila <- tarefa:
	case <-ctx.Done():
	}
}

func (s *Servico) rotas() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/v1/eventos", s.proteger(http.HandlerFunc(s.receberEvento)))
	mux.Handle("/estado", s.proteger(http.HandlerFunc(s.estado)))
	s.painel.Registar(mux, s.proteger)
	rotas := map[string]http.HandlerFunc{
		"GET /v1/painel":                      s.obterPainel,
		"POST /v1/painel":                     s.alterarPainel,
		"POST /v1/permissoes/{id}":            s.decidirPermissao,
		"GET /v1/papeis":                      s.listarPapeis,
		"POST /v1/sessoes/nova":               s.novaSessao,
		"POST /v1/sessoes/{id}/terminar":      s.terminarSessao,
		"POST /v1/sessoes/{id}/promover":      s.promover,
		"GET /v1/fluxos":                      s.listarFluxos,
		"POST /v1/fluxos/arestas":             s.criarAresta,
		"POST /v1/fluxos/arestas/{id}/apagar": s.apagarAresta,
		"POST /v1/fluxos/{id}/estado":         s.estadoFluxo,
		"GET /v1/mapa":                        s.mapa,
		"GET /v1/worktrees":                   s.listarWorktrees,
		"GET /v1/avisos":                      s.listarAvisos,
		"GET /v1/metricas":                    s.obterMetricas,
		"POST /v1/avisos/{id}/falso-alarme":   s.marcarFalsoAlarme,
		"GET /v1/sessoes/{id}/mensagens":      s.listarMensagens,
		"POST /v1/sessoes/{id}/mensagens":     s.enviarMensagem,
		"POST /v1/sessoes/{id}/focar":         s.focar,
		"POST /v1/sessoes/{id}/decisao":       s.responderDecisaoPainel,
		"POST /v1/sessoes/{id}/confirmar":     s.confirmar,
		"GET /v1/extensao/eventos":            s.eventosExtensao,
		"POST /v1/extensao/resposta":          s.respostaExtensao,
		"POST /v1/presenca":                   s.presenca,
		"POST /v1/pareamento":                 s.criarPareamento,
		"GET /v1/pareamento/qr.png":           s.qrPareamento,
		"GET /parear":                         s.paginaParear,
		"GET /v1/aparelhos":                   s.listarAparelhos,
		"POST /v1/aparelhos/{id}/revogar":     s.revogarAparelho,
	}
	for padrao, h := range rotas {
		mux.Handle(padrao, s.proteger(h))
	}
	return mux
}

// proteger exige o token (Bearer ou cookie do painel) e recusa pedidos vindos de outra origem do browser.
func (s *Servico) proteger(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.painel.OrigemValida(r) {
			http.Error(w, `{"erro":"origem recusada"}`, http.StatusForbidden)
			return
		}
		if !s.painel.Autenticado(r) {
			http.Error(w, `{"erro":"token inválido"}`, http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func validarEnvelope(dados []byte) (hook.Envelope, error) {
	var env hook.Envelope
	if err := json.Unmarshal(dados, &env); err != nil {
		return env, err
	}
	var ev struct {
		SessionID     string `json:"session_id"`
		HookEventName string `json:"hook_event_name"`
	}
	if err := json.Unmarshal(env.Evento, &ev); err != nil {
		return env, err
	}
	if ev.SessionID == "" || ev.HookEventName == "" || env.RecebidoEm == "" {
		return env, errors.New("envelope incompleto")
	}
	return env, nil
}

func (s *Servico) receberEvento(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, `{"erro":"método"}`, http.StatusMethodNotAllowed)
		return
	}
	dados, err := io.ReadAll(http.MaxBytesReader(w, r.Body, hook.TamanhoMaximo+64*1024))
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			http.Error(w, `{"erro":"corpo grande"}`, http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, `{"erro":"leitura"}`, http.StatusBadRequest)
		return
	}
	env, err := validarEnvelope(dados)
	if err != nil {
		http.Error(w, `{"erro":"envelope inválido"}`, http.StatusBadRequest)
		return
	}
	s.libertarEsperas(env)
	s.acordarFundo()
	if r.URL.Query().Get("responder") == "1" {
		s.responderEvento(w, r, env)
		return
	}
	select {
	case s.fila <- func() { s.processar(env) }:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte("{}"))
	default:
		http.Error(w, `{"erro":"fila cheia"}`, http.StatusServiceUnavailable)
	}
}

func (s *Servico) processar(env hook.Envelope) {
	resp, err := s.proc.Processar(env)
	s.posProcessar(resp)
	if err != nil {
		log.Printf("towerman: evento descartado: %v", err)
	}
}

type Estado struct {
	Versao          string         `json:"versao"`
	Porta           int            `json:"porta"`
	Inicio          string         `json:"inicio"`
	RSSBytes        uint64         `json:"rss_bytes"`
	Sessoes         map[string]int `json:"sessoes"`
	Toques          int            `json:"toques"`
	Eventos         int            `json:"eventos"`
	DiarioPendente  int            `json:"diario_pendente"`
	IngerindoDiario bool           `json:"ingerindo_diario"`
	Avisos          []string       `json:"avisos"`
}

func (s *Servico) estado(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, `{"erro":"método"}`, http.StatusMethodNotAllowed)
		return
	}
	c, err := s.quadro.Contar()
	if err != nil {
		http.Error(w, `{"erro":"quadro"}`, http.StatusInternalServerError)
		return
	}
	s.avisosMu.Lock()
	avisos := append([]string{}, s.avisos...)
	s.avisosMu.Unlock()
	e := Estado{
		Versao:          config.Versao,
		Porta:           s.Porta(),
		Inicio:          s.inicio.UTC().Format(time.RFC3339),
		RSSBytes:        rss(),
		Sessoes:         c.Sessoes,
		Toques:          c.Toques,
		Eventos:         c.Eventos,
		DiarioPendente:  contarLinhas(filepath.Join(s.op.Home, "diario.jsonl")),
		IngerindoDiario: s.ingerindo.Load(),
		Avisos:          avisos,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(e)
}

func (s *Servico) avisar(msg string) {
	s.avisosMu.Lock()
	defer s.avisosMu.Unlock()
	for _, a := range s.avisos {
		if a == msg {
			return
		}
	}
	s.avisos = append(s.avisos, msg)
}

// ingerirDiario renomeia o diário antes de o ler, para que hooks novos escrevam num ficheiro novo;
// a espera curta deixa terminar as escritas que já tinham o ficheiro antigo aberto.
func (s *Servico) ingerirDiario(ctx context.Context) {
	s.diarioMu.Lock()
	defer s.diarioMu.Unlock()
	s.ingerindo.Store(true)
	defer s.ingerindo.Store(false)

	atual := filepath.Join(s.op.Home, "diario.jsonl")
	if _, err := os.Stat(atual); err == nil {
		destino := filepath.Join(s.op.Home, fmt.Sprintf("diario.%d.jsonl", time.Now().UnixNano()))
		if err := os.Rename(atual, destino); err != nil {
			log.Printf("towerman: renomear diário: %v", err)
			return
		}
		select {
		case <-time.After(s.op.EsperaDiario):
		case <-ctx.Done():
			return
		}
	}
	pendentes, _ := filepath.Glob(filepath.Join(s.op.Home, "diario.[0-9]*.jsonl"))
	sort.Strings(pendentes)
	for _, f := range pendentes {
		if ctx.Err() != nil {
			return
		}
		s.ingerirFicheiro(ctx, f)
	}
}

func (s *Servico) ingerirFicheiro(ctx context.Context, f string) {
	fh, err := os.Open(f)
	if err != nil {
		return
	}
	leitor := bufio.NewReaderSize(fh, 64*1024)
	var rejeitadas [][]byte
	for {
		linha, err := leitor.ReadBytes('\n')
		linha = bytes.TrimSpace(linha)
		if len(linha) > 0 {
			if env, errV := validarEnvelope(linha); errV != nil {
				rejeitadas = append(rejeitadas, append([]byte(nil), linha...))
			} else {
				s.enfileirar(ctx, func() { s.processar(env) })
			}
		}
		if err != nil {
			break
		}
	}
	fh.Close()
	if ctx.Err() != nil {
		return
	}
	if len(rejeitadas) > 0 {
		if rf, err := os.OpenFile(filepath.Join(s.op.Home, "diario.rejeitado.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
			rf.Write(append(bytes.Join(rejeitadas, []byte("\n")), '\n'))
			rf.Close()
		}
	}
	feito := make(chan struct{})
	s.enfileirar(ctx, func() {
		os.Remove(f)
		close(feito)
	})
	select {
	case <-feito:
	case <-ctx.Done():
	}
}

// estadoPresenca é o que a marca de presença do Antigravity diz de uma conversa (presencaViva).
type estadoPresenca int

const (
	marcaDesconhecida estadoPresenca = iota
	marcaViva
	marcaFechada
)

// vigiar termina as sessões cujo processo morreu; diz se ficou alguma viva para vigiar.
func (s *Servico) vigiar() bool {
	sessoes, err := s.quadro.SessoesNaoTerminadas()
	if err != nil {
		return true
	}
	agora := time.Now()
	vivas := false
	for _, x := range sessoes {
		// Antigravity: a marca de presença decide antes do PID (o IDE vive mais do que a conversa).
		marca := marcaDesconhecida
		if x.Agente == "antigravity" {
			marca = presencaViva(s.pastaPresenca(), x.SessionID)
		}
		if x.Pid <= 0 && marca == marcaDesconhecida {
			continue
		}
		terminou := marca == marcaFechada || (marca == marcaDesconhecida && !processo.Vivo(x.Pid))
		if !terminou && x.Agente == "claude" {
			if r, ok := sessoesclaude.DoPid(s.op.ClaudeDir, x.Pid); ok && r.SessionID != x.SessionID {
				terminou = true
			}
		}
		if terminou {
			s.quadro.TerminarSessao(x.SessionID, agora)
			s.quadro.FalharMensagensPendentes(x.SessionID, "a sessão terminou")
		} else {
			vivas = true
		}
	}
	return vivas
}

// pastaPresenca é onde o agy guarda as marcas <conversationId>.lock.
func (s *Servico) pastaPresenca() string {
	if s.op.PresencaAntigravity != "" {
		return s.op.PresencaAntigravity
	}
	casa, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(casa, ".gemini", "antigravity", "presence")
}

func (s *Servico) reter() {
	if err := s.quadro.Reter(time.Now().Add(-s.op.Retencao)); err != nil {
		log.Printf("towerman: retenção: %v", err)
	}
}

func contarLinhas(f string) int {
	b, err := os.ReadFile(f)
	if err != nil {
		return 0
	}
	return bytes.Count(b, []byte("\n"))
}

func rss() uint64 {
	if runtime.GOOS == "linux" {
		if b, err := os.ReadFile("/proc/self/status"); err == nil {
			for _, l := range strings.Split(string(b), "\n") {
				if strings.HasPrefix(l, "VmRSS:") {
					campos := strings.Fields(l)
					if len(campos) >= 2 {
						if kb, err := strconv.ParseUint(campos[1], 10, 64); err == nil {
							return kb * 1024
						}
					}
				}
			}
		}
	}
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.Sys
}
