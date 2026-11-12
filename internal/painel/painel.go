package painel

import (
	"crypto/subtle"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/GeorgeMyller/towerman/internal/quadro"
	"github.com/GeorgeMyller/towerman/internal/transcricao"
	"github.com/GeorgeMyller/towerman/internal/visao"
)

//go:embed index.html
var pagina []byte

//go:embed torre.html
var paginaTorre []byte

//go:embed deck.html
var paginaDeck []byte

const (
	NomeCookie           = "towerman_token"
	toquesPorSessao      = 5
	mensagensPorConversa = 30
	intervaloVivo        = 25 * time.Second
)

const paginaSemSessao = `<!doctype html><meta charset="utf-8"><title>Towerman</title>
<body style="font:16px system-ui;max-width:36rem;margin:4rem auto;padding:0 1rem">
<h1>Towerman</h1><p>Sem sessão no painel. Abre-o a partir do terminal:</p>
<pre style="background:#8882;padding:.75rem;border-radius:6px">towerman painel</pre></body>`

type Painel struct {
	Quadro  *quadro.Quadro
	Difusor *Difusor
	Token   string
	Porta   func() int

	fecharUma sync.Once
	fechado   chan struct{}
}

func Novo(q *quadro.Quadro, token string, porta func() int) *Painel {
	return &Painel{Quadro: q, Difusor: NovoDifusor(), Token: token, Porta: porta, fechado: make(chan struct{})}
}

// Fechar termina as ligações ao vivo, para o encerramento do servidor não ficar à espera delas.
func (p *Painel) Fechar() { p.fecharUma.Do(func() { close(p.fechado) }) }

func (p *Painel) tokenValido(t string) bool {
	return t != "" && subtle.ConstantTimeCompare([]byte(t), []byte(p.Token)) == 1
}

func (p *Painel) Autenticado(r *http.Request) bool {
	if a := r.Header.Get("Authorization"); strings.HasPrefix(a, "Bearer ") && p.tokenValido(strings.TrimPrefix(a, "Bearer ")) {
		return true
	}
	c, err := r.Cookie(NomeCookie)
	return err == nil && p.tokenValido(c.Value)
}

// OrigemValida aceita pedidos sem Origin (curl, hooks) e os da própria página.
func (p *Painel) OrigemValida(r *http.Request) bool {
	o := r.Header.Get("Origin")
	return o == "" || o == fmt.Sprintf("http://127.0.0.1:%d", p.Porta())
}

func (p *Painel) Registar(mux *http.ServeMux, proteger func(http.Handler) http.Handler) {
	mux.Handle("GET /v1/sessoes", proteger(http.HandlerFunc(p.sessoes)))
	mux.Handle("GET /v1/sessoes/{id}/conversa", proteger(http.HandlerFunc(p.conversa)))
	mux.Handle("GET /v1/ao-vivo", proteger(http.HandlerFunc(p.aoVivo)))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) { p.servir(w, r, pagina) })
	mux.HandleFunc("GET /torre", func(w http.ResponseWriter, r *http.Request) { p.servir(w, r, paginaTorre) })
	mux.HandleFunc("GET /deck", func(w http.ResponseWriter, r *http.Request) { p.servir(w, r, paginaDeck) })
}

// servir entrega uma das páginas do painel; ?token= troca-se por cookie e volta à mesma página sem ele.
func (p *Painel) servir(w http.ResponseWriter, r *http.Request, conteudo []byte) {
	if t := r.URL.Query().Get("token"); t != "" {
		if !p.tokenValido(t) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(paginaSemSessao))
			return
		}
		http.SetCookie(w, &http.Cookie{Name: NomeCookie, Value: t, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
		http.Redirect(w, r, r.URL.Path, http.StatusSeeOther)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if !p.Autenticado(r) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(paginaSemSessao))
		return
	}
	w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'unsafe-inline'; script-src 'unsafe-inline'")
	w.Write(conteudo)
}

func escreverJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}

func (p *Painel) sessoes(w http.ResponseWriter, r *http.Request) {
	repos, err := visao.Construir(p.Quadro, r.URL.Query().Get("todas") == "1", toquesPorSessao)
	if err != nil {
		http.Error(w, `{"erro":"quadro"}`, http.StatusInternalServerError)
		return
	}
	escreverJSON(w, repos)
}

type respostaConversa struct {
	Disponivel bool                   `json:"disponivel"`
	Mensagens  []transcricao.Mensagem `json:"mensagens"`
}

func (p *Painel) conversa(w http.ResponseWriter, r *http.Request) {
	s, ok, err := p.Quadro.ObterSessao(r.PathValue("id"))
	if err != nil {
		http.Error(w, `{"erro":"quadro"}`, http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, `{"erro":"sessão desconhecida"}`, http.StatusNotFound)
		return
	}
	resp := respostaConversa{Mensagens: []transcricao.Mensagem{}}
	if s.TranscriptPath != "" {
		if ms, err := transcricao.Ultimas(s.TranscriptPath, mensagensPorConversa); err == nil {
			resp.Disponivel = true
			if ms != nil {
				resp.Mensagens = ms
			}
		}
	}
	escreverJSON(w, resp)
}

func (p *Painel) aoVivo(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "sem streaming", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	mudancas, sair := p.Difusor.Subscrever()
	defer sair()
	fmt.Fprint(w, "retry: 2000\n\n")
	fl.Flush()
	vivo := time.NewTicker(intervaloVivo)
	defer vivo.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-p.fechado:
			return
		case <-mudancas:
			fmt.Fprint(w, "event: mudou\ndata: {}\n\n")
		case <-vivo.C:
			fmt.Fprint(w, ": vivo\n\n")
		}
		fl.Flush()
	}
}
