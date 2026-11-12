package servico

import (
	"strings"
	"testing"

	"github.com/GeorgeMyller/towerman/internal/quadro"
	"github.com/GeorgeMyller/towerman/internal/transcricao"
)

func TestExtrairContexto(t *testing.T) {
	ms := []transcricao.Mensagem{
		{Papel: "utilizador", Texto: "pedido antigo"},
		{Papel: "assistente", Texto: "resposta antiga"},
		{Papel: "utilizador", Texto: "corrige o teste"},
		{Papel: "ferramenta", Texto: "Bash: go test"},
		{Papel: "assistente", Texto: "a ver"},
		{Papel: "assistente", Texto: "Corrigido. Faço commit?"},
	}
	c := extrairContexto(ms)
	if c.Pedido != "corrige o teste" || c.Resposta != "Corrigido. Faço commit?" {
		t.Fatalf("contexto errado: %+v", c)
	}
	// Pedido novo ainda sem resposta: a resposta antiga não pode aparecer como se fosse dele.
	c = extrairContexto(append(ms, transcricao.Mensagem{Papel: "utilizador", Texto: "e agora?"}))
	if c.Pedido != "e agora?" || c.Resposta != "" {
		t.Fatalf("resposta de outro pedido: %+v", c)
	}
	if r := fimDe(strings.Repeat("a", 1000)+"FIM", 10); !strings.HasPrefix(r, "…") || !strings.HasSuffix(r, "FIM") {
		t.Fatalf("devia guardar o fim: %q", r)
	}
}

func TestComandoAbrir(t *testing.T) {
	semTmux := func(string) string { return "" }
	comTmux := func(string) string { return "trabalho" }
	morto := func(int) bool { return false }
	vivo := func(int) bool { return true }
	se := quadro.Sessao{SessionID: "abc-1", Agente: "claude", Pid: 42}
	if c := comandoAbrir(se, terminal{TmuxPane: "%3"}, comTmux, vivo); c != "tmux attach-session -t "+aspas("=trabalho") {
		t.Fatalf("tmux: %q", c)
	}
	if c := comandoAbrir(se, terminal{}, semTmux, vivo); c != "" {
		t.Fatalf("sessão viva fora do tmux não se duplica: %q", c)
	}
	if c := comandoAbrir(se, terminal{}, semTmux, morto); c != "claude --resume "+aspas("abc-1") {
		t.Fatalf("retomar claude: %q", c)
	}
	se.Agente = "codex"
	if c := comandoAbrir(se, terminal{}, semTmux, morto); c != "codex resume "+aspas("abc-1") {
		t.Fatalf("retomar codex: %q", c)
	}
	se.Agente = "agy"
	if c := comandoAbrir(se, terminal{}, semTmux, morto); c != "" {
		t.Fatalf("agy não se retoma: %q", c)
	}
}

func TestAspasPara(t *testing.T) {
	casos := []struct{ goos, entrada, saida string }{
		{"linux", "abc-1", "'abc-1'"},
		{"linux", "it's", `'it'\''s'`},
		{"windows", "abc-1", "abc-1"},
		{"windows", `C:\Repos\towerman`, `C:\Repos\towerman`},
		{"windows", `C:\Meus Repos`, `"C:\Meus Repos"`},
		{"windows", `a"b c`, `"a\"b c"`},
	}
	for _, c := range casos {
		if s := aspasPara(c.goos, c.entrada); s != c.saida {
			t.Errorf("aspasPara(%s, %q) = %q, esperava %q", c.goos, c.entrada, s, c.saida)
		}
	}
}
