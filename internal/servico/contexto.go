package servico

import (
	"os"
	"sync"
	"time"

	"github.com/GeorgeMyller/towerman/internal/transcricao"
)

// Contexto de uma sessão para os apps: o último pedido da pessoa e o fim da última resposta do
// agente, lidos da transcrição. Ajuda a decidir o que responder sem ir ao computador.

const maxResposta = 900

type contextoSessao struct {
	Pedido   string
	Resposta string
}

var cacheContexto struct {
	sync.Mutex
	porCaminho map[string]entradaContexto
}

type entradaContexto struct {
	tamanho int64
	mudou   time.Time
	ctx     contextoSessao
}

// contextoDe lê a transcrição só quando ela mudou (tamanho ou hora): o estado /v1 é calculado
// a cada evento.
func contextoDe(caminho string) contextoSessao {
	if caminho == "" {
		return contextoSessao{}
	}
	st, err := os.Stat(caminho)
	if err != nil {
		return contextoSessao{}
	}
	c := &cacheContexto
	c.Lock()
	if c.porCaminho == nil {
		c.porCaminho = map[string]entradaContexto{}
	}
	if e, ok := c.porCaminho[caminho]; ok && e.tamanho == st.Size() && e.mudou.Equal(st.ModTime()) {
		c.Unlock()
		return e.ctx
	}
	c.Unlock()
	ms, err := transcricao.Ultimas(caminho, 60)
	if err != nil {
		return contextoSessao{}
	}
	ctx := extrairContexto(ms)
	c.Lock()
	c.porCaminho[caminho] = entradaContexto{tamanho: st.Size(), mudou: st.ModTime(), ctx: ctx}
	c.Unlock()
	return ctx
}

// extrairContexto: o último pedido da pessoa e o texto do agente que veio depois dele (o fim,
// que é onde ficam a conclusão e a pergunta).
func extrairContexto(ms []transcricao.Mensagem) contextoSessao {
	var ctx contextoSessao
	ultimoPedido := -1
	for i := len(ms) - 1; i >= 0; i-- {
		if ms[i].Papel == "utilizador" {
			ctx.Pedido = ms[i].Texto
			ultimoPedido = i
			break
		}
	}
	for i := len(ms) - 1; i > ultimoPedido; i-- {
		if ms[i].Papel == "assistente" {
			ctx.Resposta = fimDe(ms[i].Texto, maxResposta)
			break
		}
	}
	return ctx
}

func fimDe(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return "…" + string(r[len(r)-n:])
}
