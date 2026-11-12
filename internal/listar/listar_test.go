package listar

import (
	"strings"
	"testing"
	"time"

	"github.com/GeorgeMyller/towerman/internal/quadro"
	"github.com/GeorgeMyller/towerman/internal/visao"
)

func TestEscrever(t *testing.T) {
	agora := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	ha := func(d time.Duration) string { return agora.Add(-d).Format(quadro.FormatoData) }
	repos := []visao.Repo{{Nome: "loja", Worktrees: []visao.Worktree{{Caminho: "/x/loja", Sessoes: []visao.Sessao{
		{ID: "abcdef123456", Branch: "main", Estado: quadro.AEspera, Visto: ha(2 * time.Minute), Resumo: "corrige o IVA",
			Toques: []visao.Toque{{Caminho: "carrinho.py", Tipo: "edit", Em: ha(3 * time.Hour)}}},
	}}}}}
	var b strings.Builder
	Escrever(&b, repos, agora, false)
	saida := b.String()
	for _, esperado := range []string{"loja\n", "/x/loja", "◆ abcdef12", "À TUA ESPERA", "há 2 min", "«corrige o IVA»", "carrinho.py edit · há 3 h"} {
		if !strings.Contains(saida, esperado) {
			t.Errorf("falta %q em:\n%s", esperado, saida)
		}
	}
	b.Reset()
	Escrever(&b, nil, agora, false)
	if !strings.Contains(b.String(), "Nenhuma sessão viva") {
		t.Fatalf("estado vazio inesperado: %q", b.String())
	}
}

func TestNomeRepo(t *testing.T) {
	casos := map[string]string{"/a/loja/.git": "loja", "/a/nu.git": "nu", "": "fora de git"}
	for repo, esperado := range casos {
		if r := visao.NomeRepo(repo); r != esperado {
			t.Errorf("NomeRepo(%q) = %q, esperado %q", repo, r, esperado)
		}
	}
}
