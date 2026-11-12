package servico

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/GeorgeMyller/towerman/internal/inventario"
	"github.com/GeorgeMyller/towerman/internal/papeis"
)

const (
	validadeLancamento = 10 * time.Minute
	maxLancamentos     = 200
)

// origensLancamento: quem pode pedir uma sessão. O relógio só lança no modo default.
var origensLancamento = map[string]bool{"painel": true, "phone": true, "watch": true, "mac": true}

// pedidoLancamento é o pedido comum ao painel, à porta de rede e ao relay.
type pedidoLancamento struct {
	RequestID string `json:"request_id"`
	Origin    string `json:"origin"`
	Agent     string `json:"agent"`
	Root      string `json:"root"`
	Base      string `json:"base"`
	Role      string `json:"role"`
	Task      string `json:"task"`
	Branch    string `json:"branch"`
	Mode      string `json:"mode"`
	// lancadaPor é o id do aparelho autenticado; nunca vem do corpo do pedido.
	lancadaPor string
}

type resultadoLancamento struct {
	Codigo   int
	Erro     string
	ID       string
	Nome     string
	Worktree string
	Branch   string
}

func falhaLancamento(codigo int, formato string, args ...any) resultadoLancamento {
	return resultadoLancamento{Codigo: codigo, Erro: fmt.Sprintf(formato, args...)}
}

// entradaLancamento guarda o resultado de um request_id; pronto fecha quando o lançamento acaba.
type entradaLancamento struct {
	pronto chan struct{}
	r      resultadoLancamento
	em     time.Time
}

// cacheLancamentos torna o lançamento idempotente: o relay pode reenviar um pedido cuja resposta se perdeu.
type cacheLancamentos struct {
	mu sync.Mutex
	m  map[string]*entradaLancamento
}

// reservar devolve a entrada da chave e se é nova (quem a recebe nova faz o lançamento).
func (c *cacheLancamentos) reservar(chave string, agora time.Time) (*entradaLancamento, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]*entradaLancamento{}
	}
	if e, ok := c.m[chave]; ok {
		return e, false
	}
	var maisVelha string
	for k, e := range c.m {
		select {
		case <-e.pronto:
		default:
			continue // em curso: nunca sai
		}
		if agora.Sub(e.em) > validadeLancamento {
			delete(c.m, k)
		} else if maisVelha == "" || e.em.Before(c.m[maisVelha].em) {
			maisVelha = k
		}
	}
	if len(c.m) >= maxLancamentos && maisVelha != "" {
		delete(c.m, maisVelha)
	}
	e := &entradaLancamento{pronto: make(chan struct{}), em: agora}
	c.m[chave] = e
	return e, true
}

// lancarSessao valida o pedido, cria a worktree e arranca a sessão. Com request_id, a repetição
// do mesmo pedido pelo mesmo aparelho devolve o mesmo resultado sem lançar outra vez.
func (s *Servico) lancarSessao(ctx context.Context, p pedidoLancamento) resultadoLancamento {
	if p.RequestID == "" {
		return s.lancarSemCache(ctx, p)
	}
	e, nova := s.lancamentos.reservar(p.lancadaPor+"\x00"+p.RequestID, time.Now())
	if !nova {
		select {
		case <-e.pronto:
			return e.r
		case <-ctx.Done():
			return falhaLancamento(http.StatusServiceUnavailable, "o lançamento ainda está a decorrer")
		}
	}
	// O lançamento não depende do pedido HTTP que o trouxe: se o relay cortar, a resposta fica guardada.
	e.r = s.lancarSemCache(context.WithoutCancel(ctx), p)
	e.em = time.Now()
	close(e.pronto)
	return e.r
}

func (s *Servico) lancarSemCache(ctx context.Context, p pedidoLancamento) resultadoLancamento {
	p.Task = strings.TrimSpace(p.Task)
	if p.Task == "" {
		return falhaLancamento(http.StatusBadRequest, "a tarefa é obrigatória")
	}
	if !origensLancamento[p.Origin] {
		return falhaLancamento(http.StatusBadRequest, "origem inválida")
	}
	if p.Agent == "" {
		p.Agent = "claude"
	}
	ag, ok := procurarArrancador(p.Agent)
	if !ok {
		return falhaLancamento(http.StatusBadRequest, "agente desconhecido")
	}
	if p.Mode == "" {
		p.Mode = ag.Modos()[0]
		if p.Origin == "watch" {
			p.Mode = "default"
		}
	}
	if p.Origin == "watch" && p.Mode != "default" {
		return falhaLancamento(http.StatusBadRequest, "o relógio só lança no modo default")
	}
	if !slices.Contains(ag.Modos(), p.Mode) {
		return falhaLancamento(http.StatusBadRequest, "modo de permissões inválido")
	}
	if _, err := ag.Localizar(); err != nil {
		return falhaLancamento(http.StatusBadRequest, "%s não está instalado neste computador", ag.Nome())
	}
	rel, _, err := s.inventarioRecente(ctx, false)
	if err != nil {
		return falhaLancamento(http.StatusInternalServerError, "inventário")
	}
	raiz, base := "", ""
	for _, l := range rel.Linhas {
		if inventario.Chave(l.Raiz) == inventario.Chave(p.Root) {
			raiz, base = l.Raiz, l.Base
			break
		}
	}
	if raiz == "" {
		return falhaLancamento(http.StatusBadRequest, "repositório desconhecido")
	}
	if p.Base != "" {
		base = p.Base
	}
	if p.Branch == "" {
		p.Branch = "feat/" + slug(p.Task, 40)
	}
	if !reBranch.MatchString(p.Branch) || strings.Contains(p.Branch, "..") || !reBranch.MatchString(base) {
		return falhaLancamento(http.StatusBadRequest, "nome de branch inválido")
	}
	var papel *papeis.Papel
	if p.Role != "" {
		pp, ok := papeis.Procurar(raiz, s.op.ClaudeDir, p.Role)
		if !ok {
			return falhaLancamento(http.StatusBadRequest, "papel desconhecido")
		}
		papel = &pp
	}
	if s.contarHeadless() >= maxHeadless {
		return falhaLancamento(http.StatusConflict, "já há %d sessões abertas pelo Towerman; termina ou promove uma", maxHeadless)
	}
	pasta := filepath.Join(filepath.Dir(raiz), filepath.Base(raiz)+"-wt", slug(strings.ReplaceAll(p.Branch, "/", "-"), 60))
	if _, err := os.Stat(pasta); err == nil {
		return falhaLancamento(http.StatusConflict, "já existe uma pasta para esta branch; escolhe outro nome")
	}
	if out, err := exec.CommandContext(ctx, "git", "-C", raiz, "worktree", "add", "-b", p.Branch, pasta, base).CombinedOutput(); err != nil {
		return falhaLancamento(http.StatusConflict, "git worktree add falhou: %s", strings.TrimSpace(string(out)))
	}
	nome := slug(p.Branch[strings.LastIndex(p.Branch, "/")+1:], 24)
	if papel != nil {
		nome = slug(papel.Nome, 16) + "-" + nome
	}
	id, err := s.arrancarHeadless(ag, pasta, nome, papel, p.Task, p.Mode)
	s.inventario.mu.Lock()
	s.inventario.em = time.Time{}
	s.inventario.mu.Unlock()
	if err != nil {
		r := falhaLancamento(http.StatusInternalServerError, "%s", err.Error())
		r.Worktree = pasta
		return r
	}
	agente, por := ag.ID(), p.lancadaPor
	s.enfileirarSimples(func() { s.quadro.MarcarLancamento(id, agente, por) })
	return resultadoLancamento{Codigo: http.StatusOK, ID: id, Nome: nome, Worktree: pasta, Branch: p.Branch}
}
