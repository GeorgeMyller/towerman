package inventario

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/GeorgeMyller/towerman/internal/config"
	"github.com/GeorgeMyller/towerman/internal/quadro"
)

func carregar(ctx context.Context) (Relatorio, error) {
	var commonDirs []string
	sessoes := map[string]string{}
	if p, err := config.CaminhoQuadro(); err == nil {
		if q, err := quadro.AbrirLeitura(p); err == nil {
			commonDirs, sessoes, _ = q.ReposESessoes()
			q.Fechar()
		} else if !errors.Is(err, fs.ErrNotExist) {
			return Relatorio{}, err
		}
	}
	home, err := config.Home()
	if err != nil {
		return Relatorio{}, err
	}
	repos := ReposConhecidos(commonDirs, filepath.Join(home, "repos.txt"))
	return Inventariar(ctx, repos, sessoes), nil
}

func curto(c string) string {
	if h, err := os.UserHomeDir(); err == nil && strings.HasPrefix(c, h) {
		return "~" + c[len(h):]
	}
	return c
}

func Escrever(out io.Writer, rel Relatorio) {
	if len(rel.Linhas) == 0 {
		fmt.Fprintln(out, "Nenhum repositório conhecido. Abre sessões nos teus repos ou lista-os em ~/.claude/towerman/repos.txt.")
		return
	}
	largura := 10
	for _, l := range rel.Linhas {
		if n := len([]rune(valor(l.Branch, "(destacada)"))); n > largura {
			largura = n
		}
	}
	repo := ""
	for _, l := range rel.Linhas {
		if l.Repo != repo {
			if repo != "" {
				fmt.Fprintln(out)
			}
			repo = l.Repo
			fmt.Fprintf(out, "%s  (base %s)\n", l.Repo, valor(l.Base, "?"))
		}
		dif := ""
		if l.Base != "" && l.Branch != l.Base && l.Estado != EstadoOrfa {
			dif = fmt.Sprintf("↑%d ↓%d", l.Frente, l.Atras)
		}
		extra := l.Ultimo
		if l.Sessao != "" {
			extra += " · sessão " + l.Sessao
		}
		if l.Erro != "" {
			extra = l.Erro
		}
		fmt.Fprintf(out, "  %-13s %-*s %-10s %s  %s\n", l.Estado, largura, valor(l.Branch, "(destacada)"), dif, curto(l.Worktree), extra)
	}
	for repo, rs := range rel.RemotasProprias {
		if len(rs) > 1 {
			fmt.Fprintf(out, "\naviso: %s tem %d branches tuas no origin (%s); a tua regra é no máximo uma.\n", repo, len(rs), strings.Join(rs, ", "))
		}
	}
}

func valor(s, padrao string) string {
	if s == "" {
		return padrao
	}
	return s
}

// Executar corre "towerman wt" e "towerman wt limpar".
func Executar(args []string, entrada io.Reader, out io.Writer) error {
	limpar := len(args) > 0 && args[0] == "limpar"
	if limpar {
		args = args[1:]
	}
	fs := flag.NewFlagSet("wt", flag.ExitOnError)
	comoJSON := fs.Bool("json", false, "saída em JSON")
	fs.Parse(args)
	ctx := context.Background()
	rel, err := carregar(ctx)
	if err != nil {
		return err
	}
	if !limpar {
		if *comoJSON {
			return json.NewEncoder(out).Encode(rel)
		}
		Escrever(out, rel)
		return nil
	}
	cs := Candidatas(rel)
	if len(cs) == 0 {
		fmt.Fprintln(out, "Nada a limpar: nenhuma worktree tua integrada, limpa, sem sessão e com mais de um dia.")
		return nil
	}
	fmt.Fprintln(out, "Posso remover estas worktrees e as suas branches locais (já integradas na base):")
	for _, c := range cs {
		fmt.Fprintf(out, "  %s  %s  (%s)\n", c.Branch, curto(c.Worktree), c.Repo)
	}
	fmt.Fprint(out, "Remover? [s/N] ")
	resposta, _ := bufio.NewReader(entrada).ReadString('\n')
	if r := strings.ToLower(strings.TrimSpace(resposta)); r != "s" && r != "sim" {
		fmt.Fprintln(out, "Nada foi removido.")
		return nil
	}
	falhas := 0
	for _, c := range cs {
		if err := Remover(ctx, c); err != nil {
			falhas++
			fmt.Fprintf(out, "  falhou %s: %v\n", c.Branch, err)
			continue
		}
		fmt.Fprintf(out, "  removida %s\n", c.Branch)
	}
	if falhas > 0 {
		return fmt.Errorf("%d remoções falharam", falhas)
	}
	return nil
}
