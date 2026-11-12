package gitinfo

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Info struct {
	Repo     string
	Worktree string
	Branch   string
}

type entrada struct {
	info Info
	em   time.Time
}

type Resolvedor struct {
	Validade time.Duration
	Timeout  time.Duration
	mu       sync.Mutex
	cache    map[string]entrada
	agora    func() time.Time
}

func Novo() *Resolvedor {
	return &Resolvedor{Validade: 30 * time.Second, Timeout: 2 * time.Second, cache: map[string]entrada{}, agora: time.Now}
}

// Resolver devolve repo (git-common-dir absoluto), worktree e branch do cwd.
// Fora de um repo, ou com o git a falhar, devolve uma Info vazia.
func (r *Resolvedor) Resolver(cwd string, ignorarCache bool) Info {
	if cwd == "" {
		return Info{}
	}
	r.mu.Lock()
	if e, ok := r.cache[cwd]; ok && !ignorarCache && r.agora().Sub(e.em) < r.Validade {
		r.mu.Unlock()
		return e.info
	}
	r.mu.Unlock()

	info := r.consultar(cwd)
	r.mu.Lock()
	r.cache[cwd] = entrada{info, r.agora()}
	r.mu.Unlock()
	return info
}

func (r *Resolvedor) consultar(cwd string) Info {
	ctx, cancel := context.WithTimeout(context.Background(), r.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", cwd, "rev-parse",
		"--path-format=absolute", "--git-common-dir", "--show-toplevel", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	linhas := strings.Split(strings.TrimSpace(string(out)), "\n")
	if err != nil && len(linhas) < 2 {
		return Info{}
	}
	info := Info{}
	if len(linhas) > 0 {
		info.Repo = filepath.Clean(strings.TrimSpace(linhas[0]))
	}
	if len(linhas) > 1 {
		info.Worktree = filepath.Clean(strings.TrimSpace(linhas[1]))
	}
	if len(linhas) > 2 {
		info.Branch = strings.TrimSpace(linhas[2])
	}
	if info.Branch == "HEAD" || info.Branch == "" {
		info.Branch = commitAtual(ctx, cwd)
	}
	return info
}

// commitAtual cobre HEAD destacado e repositórios ainda sem commits.
func commitAtual(ctx context.Context, cwd string) string {
	out, err := exec.CommandContext(ctx, "git", "-C", cwd, "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		out, _ = exec.CommandContext(ctx, "git", "-C", cwd, "symbolic-ref", "--short", "HEAD").Output()
	}
	return strings.TrimSpace(string(out))
}
