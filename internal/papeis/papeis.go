// Package papeis lê os papéis de .claude/agents/*.md (do repo e do utilizador), o mesmo formato
// dos subagentes do Claude Code: frontmatter com name, description, tools e model, e o prompt no corpo.
package papeis

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Papel struct {
	Nome        string   `json:"nome"`
	Descricao   string   `json:"descricao"`
	Ferramentas []string `json:"ferramentas"`
	Modelo      string   `json:"modelo"`
	Origem      string   `json:"origem"`
	Caminho     string   `json:"caminho"`
	Prompt      string   `json:"-"`
}

// Listar devolve os papéis do repo (que têm prioridade) e os do utilizador.
func Listar(raizRepo, claudeDir string) []Papel {
	vistos := map[string]bool{}
	var r []Papel
	for _, fonte := range []struct{ dir, origem string }{
		{filepath.Join(raizRepo, ".claude", "agents"), "repo"},
		{filepath.Join(claudeDir, "agents"), "utilizador"},
	} {
		if fonte.dir == "" {
			continue
		}
		ficheiros, _ := filepath.Glob(filepath.Join(fonte.dir, "*.md"))
		sort.Strings(ficheiros)
		for _, f := range ficheiros {
			p, ok := Ler(f)
			if !ok || vistos[p.Nome] {
				continue
			}
			p.Origem = fonte.origem
			vistos[p.Nome] = true
			r = append(r, p)
		}
	}
	return r
}

func Procurar(raizRepo, claudeDir, nome string) (Papel, bool) {
	for _, p := range Listar(raizRepo, claudeDir) {
		if p.Nome == nome {
			return p, true
		}
	}
	return Papel{}, false
}

func Ler(caminho string) (Papel, bool) {
	f, err := os.Open(caminho)
	if err != nil {
		return Papel{}, false
	}
	defer f.Close()
	p := Papel{Caminho: caminho, Nome: strings.TrimSuffix(filepath.Base(caminho), ".md")}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	var corpo []string
	estado := 0
	for sc.Scan() {
		l := sc.Text()
		switch {
		case estado == 0 && strings.TrimSpace(l) == "---":
			estado = 1
		case estado == 0:
			estado = 2
			corpo = append(corpo, l)
		case estado == 1 && strings.TrimSpace(l) == "---":
			estado = 2
		case estado == 1:
			k, v, ok := strings.Cut(l, ":")
			if !ok {
				continue
			}
			v = strings.Trim(strings.TrimSpace(v), `"'`)
			switch strings.TrimSpace(k) {
			case "name":
				if v != "" {
					p.Nome = v
				}
			case "description":
				p.Descricao = v
			case "model":
				p.Modelo = v
			case "tools":
				for _, t := range strings.Split(strings.Trim(v, "[]"), ",") {
					if t = strings.Trim(strings.TrimSpace(t), `"'`); t != "" {
						p.Ferramentas = append(p.Ferramentas, t)
					}
				}
			}
		default:
			corpo = append(corpo, l)
		}
	}
	p.Prompt = strings.TrimSpace(strings.Join(corpo, "\n"))
	if p.Ferramentas == nil {
		p.Ferramentas = []string{}
	}
	return p, p.Nome != ""
}
