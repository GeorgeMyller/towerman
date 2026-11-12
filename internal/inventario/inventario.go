// Package inventario lista worktrees e branches dos repos conhecidos e propõe limpezas.
// Só lê o git; remover é sempre um passo separado e confirmado (Limpar).
package inventario

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/GeorgeMyller/towerman/internal/caminhos"
)

const (
	EstadoBase        = "base"
	EstadoAtiva       = "ativa"
	EstadoSuja        = "suja"
	EstadoIntegrada   = "integrada"
	EstadoParada      = "parada"
	EstadoPorIntegrar = "por integrar"
	EstadoOrfa        = "órfã"
	EstadoErro        = "erro"

	diasParada    = 14
	criadaRecente = 24 * time.Hour
	timeoutGit    = 10 * time.Second
	reposParalelo = 4
)

var basesConhecidas = []string{"devel", "dev", "main", "master"}

type Repo struct {
	Raiz string
	Base string
}

type Linha struct {
	Repo      string `json:"repo"`
	Raiz      string `json:"raiz"`
	Worktree  string `json:"worktree"`
	Branch    string `json:"branch"`
	Base      string `json:"base"`
	Frente    int    `json:"frente"`
	Atras     int    `json:"atras"`
	Integrada bool   `json:"integrada"`
	Suja      bool   `json:"suja"`
	Ultimo    string `json:"ultimo_commit"`
	Sessao    string `json:"sessao"`
	Estado    string `json:"estado"`
	Principal bool   `json:"principal"`
	Propria   bool   `json:"propria"`
	Recente   bool   `json:"recente"`
	Erro      string `json:"erro,omitempty"`
}

type Relatorio struct {
	Linhas          []Linha             `json:"linhas"`
	RemotasProprias map[string][]string `json:"remotas_proprias"`
}

// ReposConhecidos junta os repos vistos no quadro (git-common-dir) com os de repos.txt
// («caminho» ou «caminho<TAB>base», # comenta).
func ReposConhecidos(commonDirs []string, reposTxt string) []Repo {
	vistos := map[string]int{}
	var r []Repo
	juntar := func(raiz, base string) {
		raiz = filepath.Clean(raiz)
		if i, ok := vistos[raiz]; ok {
			if base != "" {
				r[i].Base = base
			}
			return
		}
		vistos[raiz] = len(r)
		r = append(r, Repo{Raiz: raiz, Base: base})
	}
	for _, c := range commonDirs {
		if c == "" {
			continue
		}
		if filepath.Base(c) == ".git" {
			c = filepath.Dir(c)
		}
		juntar(c, "")
	}
	if f, err := os.Open(reposTxt); err == nil {
		home, _ := os.UserHomeDir()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			l := strings.TrimSpace(sc.Text())
			if l == "" || strings.HasPrefix(l, "#") {
				continue
			}
			partes := strings.SplitN(l, "\t", 2)
			p := strings.TrimSpace(partes[0])
			if strings.HasPrefix(p, "~/") {
				p = filepath.Join(home, p[2:])
			}
			base := ""
			if len(partes) == 2 {
				base = strings.TrimSpace(partes[1])
			}
			juntar(p, base)
		}
		f.Close()
	}
	sort.Slice(r, func(i, j int) bool { return r[i].Raiz < r[j].Raiz })
	return r
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	c, cancel := context.WithTimeout(ctx, timeoutGit)
	defer cancel()
	out, err := exec.CommandContext(c, "git", append([]string{"-C", dir}, args...)...).Output()
	return strings.TrimRight(string(out), "\n"), err
}

func existeRef(ctx context.Context, dir, ref string) bool {
	_, err := git(ctx, dir, "rev-parse", "--verify", "-q", ref)
	return err == nil
}

func baseDe(ctx context.Context, r Repo) string {
	if r.Base != "" {
		return r.Base
	}
	for _, b := range basesConhecidas {
		if existeRef(ctx, r.Raiz, "refs/heads/"+b) {
			return b
		}
	}
	return ""
}

type worktreeGit struct {
	caminho, branch string
	prunable, bare  bool
}

func listarWorktrees(ctx context.Context, raiz string) ([]worktreeGit, error) {
	out, err := git(ctx, raiz, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var r []worktreeGit
	var atual *worktreeGit
	for _, l := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(l, "worktree "):
			r = append(r, worktreeGit{caminho: strings.TrimPrefix(l, "worktree ")})
			atual = &r[len(r)-1]
		case atual == nil:
		case strings.HasPrefix(l, "branch refs/heads/"):
			atual.branch = strings.TrimPrefix(l, "branch refs/heads/")
		case strings.HasPrefix(l, "prunable"):
			atual.prunable = true
		case l == "bare":
			atual.bare = true
		}
	}
	return r, nil
}

// Integrada diz se todos os commits de branch já estão em base, incluindo squash-merges (git cherry).
func Integrada(ctx context.Context, dir, base, branch string) bool {
	out, err := git(ctx, dir, "cherry", base, branch)
	if err != nil {
		return false
	}
	return !strings.Contains("\n"+out, "\n+")
}

func inventariarRepo(ctx context.Context, r Repo, sessoes map[string]string, agora time.Time) []Linha {
	nome := filepath.Base(r.Raiz)
	wts, err := listarWorktrees(ctx, r.Raiz)
	if err != nil {
		return []Linha{{Repo: nome, Raiz: r.Raiz, Worktree: r.Raiz, Estado: EstadoErro, Erro: "não é um repositório git acessível"}}
	}
	base := baseDe(ctx, r)
	email, _ := git(ctx, r.Raiz, "config", "user.email")
	var linhas []Linha
	for i, w := range wts {
		if w.bare {
			continue
		}
		l := Linha{Repo: nome, Raiz: r.Raiz, Worktree: w.caminho, Branch: w.branch, Base: base, Principal: i == 0, Sessao: sessoes[chave(w.caminho)]}
		if w.prunable {
			l.Estado = EstadoOrfa
			linhas = append(linhas, l)
			continue
		}
		ref := w.branch
		if ref == "" {
			ref = "HEAD"
		}
		if base != "" && w.branch != base {
			if out, err := git(ctx, w.caminho, "rev-list", "--left-right", "--count", base+"..."+ref); err == nil {
				if c := strings.Fields(out); len(c) == 2 {
					l.Atras, _ = strconv.Atoi(c[0])
					l.Frente, _ = strconv.Atoi(c[1])
				}
			}
			l.Integrada = w.branch != "" && (l.Frente == 0 || Integrada(ctx, w.caminho, base, w.branch))
		}
		if out, err := git(ctx, w.caminho, "status", "--porcelain"); err == nil && strings.TrimSpace(out) != "" {
			l.Suja = true
		}
		var ultimo time.Time
		if out, err := git(ctx, w.caminho, "log", "-1", "--format=%ct%n%ae", ref); err == nil {
			partes := strings.SplitN(out, "\n", 2)
			if ts, err := strconv.ParseInt(partes[0], 10, 64); err == nil {
				ultimo = time.Unix(ts, 0)
				l.Ultimo = Ha(ultimo, agora)
			}
			l.Propria = len(partes) == 2 && email != "" && strings.EqualFold(partes[1], email)
		}
		if w.branch != "" {
			if out, err := git(ctx, r.Raiz, "reflog", "show", "--format=%ct", "refs/heads/"+w.branch); err == nil {
				linhas := strings.Split(out, "\n")
				if ts, err := strconv.ParseInt(linhas[len(linhas)-1], 10, 64); err == nil {
					l.Recente = agora.Sub(time.Unix(ts, 0)) < criadaRecente
				}
			}
		}
		switch {
		case w.branch != "" && w.branch == base:
			l.Estado = EstadoBase
		case l.Sessao != "":
			l.Estado = EstadoAtiva
		case l.Suja:
			l.Estado = EstadoSuja
		case l.Integrada:
			l.Estado = EstadoIntegrada
		case !ultimo.IsZero() && agora.Sub(ultimo) > diasParada*24*time.Hour:
			l.Estado = EstadoParada
		default:
			l.Estado = EstadoPorIntegrar
		}
		linhas = append(linhas, l)
	}
	return linhas
}

func remotasProprias(ctx context.Context, r Repo) []string {
	email, _ := git(ctx, r.Raiz, "config", "user.email")
	if email == "" {
		return nil
	}
	out, err := git(ctx, r.Raiz, "for-each-ref", "--format=%(refname:short)\t%(authoremail)", "refs/remotes/origin")
	if err != nil {
		return nil
	}
	var r2 []string
	for _, l := range strings.Split(out, "\n") {
		partes := strings.SplitN(l, "\t", 2)
		if len(partes) != 2 {
			continue
		}
		nome := strings.TrimPrefix(partes[0], "origin/")
		switch nome {
		case "origin", "HEAD", "main", "master", "devel", "dev":
			continue
		}
		if strings.EqualFold(strings.Trim(partes[1], "<>"), email) {
			r2 = append(r2, nome)
		}
	}
	return r2
}

// chave compara caminhos de worktree vindos de fontes diferentes (git, hooks): resolve links e
// nomes curtos do Windows e ignora maiúsculas onde o sistema de ficheiros também as ignora.
func Chave(p string) string { return chave(p) }

func chave(p string) string {
	return caminhos.Normalizar(caminhos.Real(p, ""), "", "")
}

// Inventariar corre os repos em paralelo; sessoes mapeia caminho da worktree → nome da sessão viva.
func Inventariar(ctx context.Context, repos []Repo, sessoesPorCaminho map[string]string) Relatorio {
	agora := time.Now()
	sessoes := make(map[string]string, len(sessoesPorCaminho))
	for k, v := range sessoesPorCaminho {
		sessoes[chave(k)] = v
	}
	res := make([][]Linha, len(repos))
	remotas := make([][]string, len(repos))
	var wg sync.WaitGroup
	sem := make(chan struct{}, reposParalelo)
	for i, r := range repos {
		wg.Add(1)
		go func(i int, r Repo) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			res[i] = inventariarRepo(ctx, r, sessoes, agora)
			remotas[i] = remotasProprias(ctx, r)
		}(i, r)
	}
	wg.Wait()
	rel := Relatorio{RemotasProprias: map[string][]string{}}
	for i := range repos {
		rel.Linhas = append(rel.Linhas, res[i]...)
		if len(remotas[i]) > 0 {
			rel.RemotasProprias[filepath.Base(repos[i].Raiz)] = remotas[i]
		}
	}
	if rel.Linhas == nil {
		rel.Linhas = []Linha{}
	}
	return rel
}

// Candidatas são as worktrees que o limpar pode propor: não principais, integradas, limpas,
// sem sessão viva, com o último commit do próprio utilizador e criadas há mais de 24 h
// (uma branch nova sem commits também conta como «integrada»).
func Candidatas(rel Relatorio) []Linha {
	var r []Linha
	for _, l := range rel.Linhas {
		if l.Principal || l.Branch == "" || l.Branch == l.Base {
			continue
		}
		switch l.Branch {
		case "main", "master", "devel", "dev":
			continue
		}
		if l.Estado == EstadoIntegrada && l.Propria && !l.Recente {
			r = append(r, l)
		}
	}
	return r
}

// Remover apaga a worktree (sem --force: falha se houver alterações) e a branch.
// git branch -d recusa squash-merges; nesse caso, só com cherry a confirmar a integração, usa -D.
func Remover(ctx context.Context, l Linha) error {
	if _, err := git(ctx, l.Raiz, "worktree", "remove", l.Worktree); err != nil {
		return err
	}
	if _, err := git(ctx, l.Raiz, "branch", "-d", l.Branch); err != nil {
		if !Integrada(ctx, l.Raiz, l.Base, l.Branch) {
			return err
		}
		if _, err := git(ctx, l.Raiz, "branch", "-D", l.Branch); err != nil {
			return err
		}
	}
	return nil
}

func Ha(t, agora time.Time) string {
	d := agora.Sub(t)
	switch {
	case d < time.Hour:
		return "há " + strconv.Itoa(int(d.Minutes())) + " min"
	case d < 48*time.Hour:
		return "há " + strconv.Itoa(int(d.Hours())) + " h"
	}
	return "há " + strconv.Itoa(int(d.Hours()/24)) + " d"
}
