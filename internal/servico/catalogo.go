package servico

import (
	"context"

	"github.com/GeorgeMyller/towerman/internal/papeis"
)

type agenteCatalogo struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Modes []string `json:"modes"`
	Note  string   `json:"note,omitempty"`
}

type papelCatalogo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type repoCatalogo struct {
	Name  string          `json:"name"`
	Root  string          `json:"root"`
	Base  string          `json:"base"`
	Roles []papelCatalogo `json:"roles"`
}

type catalogo struct {
	Agents   []agenteCatalogo `json:"agents"`
	Repos    []repoCatalogo   `json:"repos"`
	Headless struct {
		Open int `json:"open"`
		Max  int `json:"max"`
	} `json:"headless"`
}

// notasAgente avisa o aparelho do que cada agente não consegue fazer sem terminal.
var notasAgente = map[string]string{
	"antigravity": "sem terminal, o agy só corre os comandos de permissions.allow; os outros são negados",
	"codex":       "sem terminal, o Codex não pede aprovações: corre no sandbox (Pedir tudo: edita a worktree, sem rede; Só planear: só leitura)",
}

// catalogoLancamento diz ao aparelho o que este computador pode lançar: agentes instalados,
// repos conhecidos (um por raiz) e papéis de cada um, sem o prompt.
func (s *Servico) catalogoLancamento(ctx context.Context) (catalogo, error) {
	var c catalogo
	c.Agents, c.Repos = []agenteCatalogo{}, []repoCatalogo{}
	for _, ag := range arrancadores {
		if _, err := ag.Localizar(); err == nil {
			c.Agents = append(c.Agents, agenteCatalogo{ID: ag.ID(), Name: ag.Nome(), Modes: ag.Modos(), Note: notasAgente[ag.ID()]})
		}
	}
	rel, _, err := s.inventarioRecente(ctx, false)
	if err != nil {
		return c, err
	}
	vistos := map[string]bool{}
	for _, l := range rel.Linhas {
		if l.Raiz == "" || vistos[l.Raiz] {
			continue
		}
		vistos[l.Raiz] = true
		r := repoCatalogo{Name: l.Repo, Root: l.Raiz, Base: l.Base, Roles: []papelCatalogo{}}
		for _, p := range papeis.Listar(l.Raiz, s.op.ClaudeDir) {
			r.Roles = append(r.Roles, papelCatalogo{Name: p.Nome, Description: p.Descricao})
		}
		c.Repos = append(c.Repos, r)
	}
	c.Headless.Open, c.Headless.Max = s.contarHeadless(), maxHeadless
	return c, nil
}
