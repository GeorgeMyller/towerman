// Package visao agrupa as sessões do quadro por repo e worktree, a forma que o ls e o painel mostram.
package visao

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/GeorgeMyller/towerman/internal/quadro"
)

type Toque struct {
	Caminho string `json:"caminho"`
	Tipo    string `json:"tipo"`
	Em      string `json:"em"`
}

type Sessao struct {
	ID       string    `json:"id"`
	Nome     string    `json:"nome"`
	Agente   string    `json:"agente"`
	Pid      int       `json:"pid"`
	Branch   string    `json:"branch"`
	Estado   string    `json:"estado"`
	Resumo   string    `json:"resumo"`
	Cwd      string    `json:"cwd"`
	Iniciada string    `json:"iniciada_em"`
	Visto    string    `json:"visto_em"`
	Toques   []Toque   `json:"toques"`
	Colisoes []Colisao `json:"colisoes"`
	Papel    string    `json:"papel"`
	Headless bool      `json:"headless"`
}

type Colisao struct {
	Com         string `json:"com"`
	NomeCom     string `json:"nome_com"`
	Caminho     string `json:"caminho"`
	MesmaBranch bool   `json:"mesma_branch"`
}

type Worktree struct {
	Caminho string   `json:"caminho"`
	Sessoes []Sessao `json:"sessoes"`
}

type Repo struct {
	Nome      string     `json:"nome"`
	Caminho   string     `json:"caminho"`
	Worktrees []Worktree `json:"worktrees"`
}

// NomeRepo tira o nome legível do git-common-dir (".../loja/.git" → "loja").
func NomeRepo(repo string) string {
	if repo == "" {
		return "fora de git"
	}
	r := filepath.Clean(repo)
	if filepath.Base(r) == ".git" {
		r = filepath.Dir(r)
	}
	return strings.TrimSuffix(filepath.Base(r), ".git")
}

func Construir(q *quadro.Quadro, todas bool, toquesPorSessao int) ([]Repo, error) {
	sessoes, err := q.Sessoes(todas)
	if err != nil {
		return nil, err
	}
	colisoes := map[string][]Colisao{}
	if cs, err := q.ColisoesVivas(time.Now().Add(-24 * time.Hour)); err == nil {
		for _, c := range cs {
			colisoes[c.SessionID] = append(colisoes[c.SessionID], Colisao{Com: c.Com, NomeCom: c.NomeCom, Caminho: c.Caminho, MesmaBranch: c.MesmaBranch})
		}
	}
	repos := map[string]*Repo{}
	var ordem []string
	for _, s := range sessoes {
		chave := s.Repo
		r, ok := repos[chave]
		if !ok {
			r = &Repo{Nome: NomeRepo(s.Repo), Caminho: s.Repo}
			repos[chave] = r
			ordem = append(ordem, chave)
		}
		wt := s.Worktree
		if wt == "" {
			wt = s.Cwd
		}
		i := -1
		for j := range r.Worktrees {
			if r.Worktrees[j].Caminho == wt {
				i = j
			}
		}
		if i < 0 {
			r.Worktrees = append(r.Worktrees, Worktree{Caminho: wt})
			i = len(r.Worktrees) - 1
		}
		vs := Sessao{ID: s.SessionID, Nome: s.Nome, Agente: s.Agente, Pid: s.Pid, Branch: s.Branch, Estado: s.Estado,
			Resumo: s.Resumo, Cwd: s.Cwd, Iniciada: s.IniciadaEm, Visto: s.VistoEm, Toques: []Toque{}, Colisoes: colisoes[s.SessionID], Papel: s.Papel, Headless: s.Headless}
		if vs.Colisoes == nil {
			vs.Colisoes = []Colisao{}
		}
		if toquesPorSessao > 0 {
			ts, err := q.ToquesRecentes(s.SessionID, toquesPorSessao)
			if err != nil {
				return nil, err
			}
			for _, t := range ts {
				vs.Toques = append(vs.Toques, Toque{Caminho: t.Caminho, Tipo: t.Tipo, Em: t.Em})
			}
		}
		r.Worktrees[i].Sessoes = append(r.Worktrees[i].Sessoes, vs)
	}
	out := make([]Repo, 0, len(ordem))
	for _, k := range ordem {
		out = append(out, *repos[k])
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Nome < out[j].Nome })
	return out, nil
}

// Ha devolve uma duração legível em português ("agora", "há 5 min", "há 2 h", "há 3 d").
func Ha(quando string, agora time.Time) string {
	t, err := time.Parse(quadro.FormatoData, quando)
	if err != nil {
		return ""
	}
	d := agora.Sub(t)
	switch {
	case d < time.Minute:
		return "agora"
	case d < time.Hour:
		return "há " + strconv.Itoa(int(d.Minutes())) + " min"
	case d < 48*time.Hour:
		return "há " + strconv.Itoa(int(d.Hours())) + " h"
	}
	return "há " + strconv.Itoa(int(d.Hours()/24)) + " d"
}
