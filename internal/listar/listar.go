package listar

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/GeorgeMyller/towerman/internal/config"
	"github.com/GeorgeMyller/towerman/internal/quadro"
	"github.com/GeorgeMyller/towerman/internal/visao"
)

var simbolos = map[string]string{
	quadro.Ativa:     "●",
	quadro.AEspera:   "◆",
	quadro.Parada:    "○",
	quadro.Terminada: "×",
}

var rotulos = map[string]string{
	quadro.Ativa:     "a trabalhar",
	quadro.AEspera:   "À TUA ESPERA",
	quadro.Parada:    "parada",
	quadro.Terminada: "terminada",
}

func Executar(args []string, out io.Writer) error {
	opcoes := flag.NewFlagSet("ls", flag.ExitOnError)
	todas := opcoes.Bool("todas", false, "incluir sessões terminadas")
	n := opcoes.Int("n", 3, "ficheiros tocados a mostrar por sessão")
	opcoes.Parse(args)

	p, err := config.CaminhoQuadro()
	if err != nil {
		return err
	}
	q, err := quadro.AbrirLeitura(p)
	if errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintln(out, "Ainda não há quadro. Corre \"towerman install\" e abre uma sessão.")
		return nil
	}
	if err != nil {
		return err
	}
	defer q.Fechar()
	repos, err := visao.Construir(q, *todas, *n)
	if err != nil {
		return err
	}
	Escrever(out, repos, time.Now(), *todas)
	return nil
}

func Escrever(out io.Writer, repos []visao.Repo, agora time.Time, todas bool) {
	if len(repos) == 0 {
		if todas {
			fmt.Fprintln(out, "Nenhuma sessão registada.")
		} else {
			fmt.Fprintln(out, "Nenhuma sessão viva. Usa --todas para ver as terminadas.")
		}
		return
	}
	home, _ := os.UserHomeDir()
	curto := func(c string) string {
		if home != "" && strings.HasPrefix(c, home) {
			return "~" + c[len(home):]
		}
		return c
	}
	for i, r := range repos {
		if i > 0 {
			fmt.Fprintln(out)
		}
		fmt.Fprintf(out, "%s\n", r.Nome)
		for _, w := range r.Worktrees {
			fmt.Fprintf(out, "  %s\n", curto(w.Caminho))
			for _, s := range w.Sessoes {
				nome := s.Nome
				if nome == "" {
					nome = s.ID[:min(8, len(s.ID))]
				}
				fmt.Fprintf(out, "    %s %-22s %-24s %-13s %-10s", simbolos[s.Estado], nome, s.Branch, rotulos[s.Estado], visao.Ha(s.Visto, agora))
				if s.Resumo != "" {
					fmt.Fprintf(out, " «%s»", corte(s.Resumo, 60))
				}
				fmt.Fprintln(out)
				for _, t := range s.Toques {
					fmt.Fprintf(out, "        %s %s · %s\n", corte(t.Caminho, 60), t.Tipo, visao.Ha(t.Em, agora))
				}
			}
		}
	}
}

func corte(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
