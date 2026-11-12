package servico

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMapaJuntaInventarioESessoes(t *testing.T) {
	repo, wt := novoRepo(t)
	a := novoAmbiente(t, nil)
	apagada := filepath.Join(filepath.Dir(repo), "apagada")
	git(t, repo, "worktree", "add", "-q", "-b", "feat/apagada", apagada)
	a.enviar(map[string]any{"session_id": "w", "hook_event_name": "SessionStart", "cwd": wt})
	a.enviar(map[string]any{"session_id": "z", "hook_event_name": "SessionStart", "cwd": apagada})
	a.esperar()
	git(t, repo, "worktree", "remove", "--force", apagada)
	os.RemoveAll(apagada)

	var m struct {
		Zonas []struct {
			Nome   string
			Faixas []struct {
				Branch  string
				Estado  string
				NoGit   bool `json:"no_git"`
				Sessoes []struct{ ID string }
			}
		}
	}
	a.obter("/v1/mapa?todas=1&fresco=1", &m)
	if len(m.Zonas) != 1 {
		t.Fatalf("o repo devia aparecer numa só zona: %+v", m.Zonas)
	}
	sessoesPorBranch := map[string]int{}
	for _, f := range m.Zonas[0].Faixas {
		sessoesPorBranch[f.Branch] += len(f.Sessoes)
	}
	if sessoesPorBranch["main"] != 0 || sessoesPorBranch["feat/x"] != 1 || sessoesPorBranch["feat/apagada"] != 1 || len(m.Zonas[0].Faixas) != 3 {
		t.Fatalf("faixas inesperadas: %+v", m.Zonas[0].Faixas)
	}
}
