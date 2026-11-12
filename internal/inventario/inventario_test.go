package inventario

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func gitT(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=eu", "GIT_AUTHOR_EMAIL=eu@x", "GIT_COMMITTER_NAME=eu", "GIT_COMMITTER_EMAIL=eu@x")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func commit(t *testing.T, dir, ficheiro string) {
	os.WriteFile(filepath.Join(dir, ficheiro), []byte(ficheiro), 0o644)
	gitT(t, dir, "add", ".")
	gitT(t, dir, "commit", "-q", "-m", ficheiro)
}

func porWorktree(rel Relatorio) map[string]Linha {
	m := map[string]Linha{}
	for _, l := range rel.Linhas {
		m[filepath.Base(l.Worktree)] = l
	}
	return m
}

func TestInventarioELimpar(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "loja")
	os.MkdirAll(repo, 0o755)
	gitT(t, repo, "init", "-q", "-b", "main")
	gitT(t, repo, "config", "user.email", "eu@x")
	commit(t, repo, "a")
	gitT(t, repo, "branch", "devel")

	gitT(t, repo, "worktree", "add", "-q", "-b", "feat/feita", filepath.Join(base, "feita"), "devel")
	commit(t, filepath.Join(base, "feita"), "b")
	gitT(t, repo, "worktree", "add", "-q", "-b", "feat/aberta", filepath.Join(base, "aberta"), "devel")
	commit(t, filepath.Join(base, "aberta"), "c")
	gitT(t, repo, "worktree", "add", "-q", "-b", "feat/suja", filepath.Join(base, "suja"), "devel")
	os.WriteFile(filepath.Join(base, "suja", "x"), []byte("x"), 0o644)
	gitT(t, repo, "worktree", "add", "-q", "-b", "feat/nova", filepath.Join(base, "nova"), "devel")
	gitT(t, repo, "worktree", "add", "-q", "-b", "feat/ocupada", filepath.Join(base, "ocupada"), "devel")
	gitT(t, repo, "checkout", "-q", "devel")
	gitT(t, repo, "merge", "-q", "--squash", "feat/feita")
	gitT(t, repo, "commit", "-q", "-m", "squash feita")

	repos := ReposConhecidos([]string{filepath.Join(repo, ".git")}, filepath.Join(base, "nao-existe.txt"))
	if len(repos) != 1 || repos[0].Raiz != filepath.Clean(repo) {
		t.Fatalf("repos inesperados: %+v", repos)
	}
	sessoes := map[string]string{filepath.Join(base, "ocupada"): "sessao-x"}
	rel := Inventariar(context.Background(), repos, sessoes)
	w := porWorktree(rel)
	esperado := map[string]string{"loja": EstadoBase, "feita": EstadoIntegrada, "aberta": EstadoPorIntegrar,
		"suja": EstadoSuja, "nova": EstadoIntegrada, "ocupada": EstadoAtiva}
	for wt, e := range esperado {
		if w[wt].Estado != e {
			t.Errorf("%s: estado %q, esperado %q (%+v)", wt, w[wt].Estado, e, w[wt])
		}
	}
	if w["aberta"].Frente != 1 || w["loja"].Base != "devel" || !w["loja"].Principal || w["ocupada"].Sessao != "sessao-x" {
		t.Fatalf("campos inesperados: %+v", w)
	}

	// Branches criadas agora não são candidatas; simula-se uma criada há dias apagando o reflog.
	if c := Candidatas(rel); len(c) != 0 {
		t.Fatalf("branches recentes não deviam ser candidatas: %+v", c)
	}
	os.Remove(filepath.Join(repo, ".git", "logs", "refs", "heads", "feat", "feita"))
	rel = Inventariar(context.Background(), repos, sessoes)
	c := Candidatas(rel)
	if len(c) != 1 || c[0].Branch != "feat/feita" {
		t.Fatalf("candidata inesperada: %+v", c)
	}
	if err := Remover(context.Background(), c[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(base, "feita")); !os.IsNotExist(err) {
		t.Fatal("a worktree devia ter sido removida")
	}
	if exec.Command("git", "-C", repo, "rev-parse", "--verify", "-q", "refs/heads/feat/feita").Run() == nil {
		t.Fatal("a branch squash-merged devia ter sido apagada")
	}
}

func TestReposTxt(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "repos.txt")
	os.WriteFile(p, []byte("# comentário\n/x/a\n/x/b\tdev\n\n"), 0o600)
	r := ReposConhecidos([]string{"/x/a/.git"}, p)
	if len(r) != 2 || r[0].Raiz != filepath.Clean("/x/a") || r[1].Base != "dev" {
		t.Fatalf("repos inesperados: %+v", r)
	}
}
