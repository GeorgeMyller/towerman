package servico

import (
	"context"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/GeorgeMyller/towerman/internal/inventario"
	"github.com/GeorgeMyller/towerman/internal/quadro"
	"github.com/GeorgeMyller/towerman/internal/visao"
)

const validadeInventario = 30 * time.Second

type cacheInventario struct {
	mu  sync.Mutex
	rel inventario.Relatorio
	em  time.Time
}

// inventarioRecente evita correr git em todos os repos a cada atualização do mapa.
func (s *Servico) inventarioRecente(ctx context.Context, fresco bool) (inventario.Relatorio, time.Time, error) {
	s.inventario.mu.Lock()
	defer s.inventario.mu.Unlock()
	if !fresco && time.Since(s.inventario.em) < validadeInventario {
		return s.inventario.rel, s.inventario.em, nil
	}
	repos, sessoes, err := s.quadro.ReposESessoes()
	if err != nil {
		return inventario.Relatorio{}, time.Time{}, err
	}
	lista := inventario.ReposConhecidos(repos, filepath.Join(s.op.Home, "repos.txt"))
	s.inventario.rel = inventario.Inventariar(ctx, lista, sessoes)
	s.inventario.em = time.Now()
	return s.inventario.rel, s.inventario.em, nil
}

type faixaMapa struct {
	Caminho   string         `json:"caminho"`
	Branch    string         `json:"branch"`
	Estado    string         `json:"estado"`
	Frente    int            `json:"frente"`
	Atras     int            `json:"atras"`
	Ultimo    string         `json:"ultimo_commit"`
	Principal bool           `json:"principal"`
	NoGit     bool           `json:"no_git"`
	Sessoes   []visao.Sessao `json:"sessoes"`
}

type zonaMapa struct {
	Nome    string      `json:"nome"`
	Raiz    string      `json:"raiz"`
	Base    string      `json:"base"`
	Faixas  []faixaMapa `json:"faixas"`
	Remotas []string    `json:"remotas_proprias"`
}

// mapa junta o inventário do git (worktrees com e sem sessão) às sessões do quadro.
// Sessões cuja pasta não aparece no inventário (repo não listado, fora de git) ganham faixa própria.
func (s *Servico) mapa(w http.ResponseWriter, r *http.Request) {
	repos, err := visao.Construir(s.quadro, r.URL.Query().Get("todas") == "1", 5)
	if err != nil {
		http.Error(w, `{"erro":"quadro"}`, http.StatusInternalServerError)
		return
	}
	rel, em, err := s.inventarioRecente(r.Context(), r.URL.Query().Get("fresco") == "1")
	if err != nil {
		http.Error(w, `{"erro":"inventário"}`, http.StatusInternalServerError)
		return
	}
	zonas := []zonaMapa{}
	porRaiz := map[string]int{}
	faixaPorCaminho := map[string][2]int{}
	for _, l := range rel.Linhas {
		i, ok := porRaiz[inventario.Chave(l.Raiz)]
		if !ok {
			i = len(zonas)
			porRaiz[inventario.Chave(l.Raiz)] = i
			zonas = append(zonas, zonaMapa{Nome: l.Repo, Raiz: l.Raiz, Base: l.Base, Remotas: rel.RemotasProprias[l.Repo]})
		}
		zonas[i].Faixas = append(zonas[i].Faixas, faixaMapa{Caminho: l.Worktree, Branch: l.Branch, Estado: l.Estado,
			Frente: l.Frente, Atras: l.Atras, Ultimo: l.Ultimo, Principal: l.Principal, NoGit: true, Sessoes: []visao.Sessao{}})
		faixaPorCaminho[inventario.Chave(l.Worktree)] = [2]int{i, len(zonas[i].Faixas) - 1}
	}
	for _, rv := range repos {
		for _, wt := range rv.Worktrees {
			if pos, ok := faixaPorCaminho[inventario.Chave(wt.Caminho)]; ok {
				f := &zonas[pos[0]].Faixas[pos[1]]
				f.Sessoes = append(f.Sessoes, wt.Sessoes...)
				continue
			}
			raiz := rv.Caminho
			if filepath.Base(raiz) == ".git" {
				raiz = filepath.Dir(raiz)
			}
			i, ok := porRaiz[inventario.Chave(raiz)]
			if !ok {
				i = len(zonas)
				porRaiz[inventario.Chave(raiz)] = i
				zonas = append(zonas, zonaMapa{Nome: rv.Nome, Raiz: raiz})
			}
			branch := ""
			if len(wt.Sessoes) > 0 {
				branch = wt.Sessoes[0].Branch
			}
			zonas[i].Faixas = append(zonas[i].Faixas, faixaMapa{Caminho: wt.Caminho, Branch: branch, Sessoes: wt.Sessoes})
			faixaPorCaminho[inventario.Chave(wt.Caminho)] = [2]int{i, len(zonas[i].Faixas) - 1}
		}
	}
	arestas, err := s.quadro.ArestasVivas()
	if err != nil || arestas == nil {
		arestas = []quadro.Aresta{}
	}
	escreverJSON(w, map[string]any{"zonas": zonas, "arestas": arestas, "inventario_em": em.UTC().Format(time.RFC3339)})
}
