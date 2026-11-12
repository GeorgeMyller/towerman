package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/GeorgeMyller/towerman/internal/bench"
	"github.com/GeorgeMyller/towerman/internal/config"
	"github.com/GeorgeMyller/towerman/internal/extensao"
	"github.com/GeorgeMyller/towerman/internal/hook"
	"github.com/GeorgeMyller/towerman/internal/instalar"
	"github.com/GeorgeMyller/towerman/internal/inventario"
	"github.com/GeorgeMyller/towerman/internal/listar"
	"github.com/GeorgeMyller/towerman/internal/servico"
)

const ajuda = `uso: towerman <comando> [opções]

comandos:
  hook        recebe um evento de hook no stdin (invocado pelo agente)
  serve       corre o serviço local em 127.0.0.1
  install     acrescenta os hooks ao settings.json e regista o serviço
  uninstall   remove os hooks e o serviço
  painel      abre o painel no browser (--sem-abrir só imprime o URL)
  wt          worktrees e branches de todos os repos (wt limpar propõe remover as integradas)
  regra2      mostra ou muda a regra 2: ask (pede confirmação), deny (recusa) ou off
  ls          lista as sessões por repo e worktree (--todas, -n)
  deck        abre o deck (radar, pedidos, partidas) numa janela própria
  pair        mostra o QR para parear o iPhone e o Apple Watch com este computador
  aparelhos   lista os aparelhos pareados (aparelhos revogar <nome|id>)
  bench       mede a latência do hook
  version     mostra a versão

variáveis: TOWERMAN=off, TOWERMAN_HOME, TOWERMAN_PORTA, TOWERMAN_PORTA_REDE, TOWERMAN_TIMEOUT_MS, TOWERMAN_CLAUDE_DIR
`

func main() {
	if len(os.Args) < 2 {
		// No Windows, abrir o towerman.exe com duplo clique instala-o.
		if instaladorDuploClique() {
			return
		}
		fmt.Fprint(os.Stderr, ajuda)
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	if cmd == "hook" {
		// «towerman hook --agente agy <Evento>»: hooks do Antigravity (agy e IDE).
		if len(args) >= 3 && args[0] == "--agente" && args[1] == "agy" {
			hook.ExecutarAgy(args[2], os.Stdin, os.Stdout)
		} else if len(args) >= 2 && args[0] == "--agente" && args[1] == "codex" {
			hook.ExecutarAgente("codex", os.Stdin, os.Stdout)
		} else {
			hook.Executar(os.Stdin, os.Stdout)
		}
		os.Exit(0)
	}
	var err error
	switch cmd {
	case "serve":
		err = serve(args)
	case "install":
		err = install(args, os.Stdout)
	case "uninstall":
		err = uninstall(args, os.Stdout)
	case "painel":
		err = painel(args, os.Stdout)
	case "deck":
		err = deck(args, os.Stdout)
	case "wt":
		err = inventario.Executar(args, os.Stdin, os.Stdout)
	case "regra2":
		err = regra2(args, os.Stdout)
	case "pair", "parear":
		err = parear(args, os.Stdout)
	case "aparelhos", "devices":
		err = aparelhos(args, os.Stdout)
	case "ls":
		err = listar.Executar(args, os.Stdout)
	case "bench":
		err = bench.Executar(args, os.Stdout)
	case "version", "--version", "-v":
		fmt.Println("towerman", config.Versao)
	case "help", "-h", "--help":
		fmt.Print(ajuda)
	default:
		fmt.Fprintf(os.Stderr, "towerman: comando desconhecido %q\n\n%s", cmd, ajuda)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "towerman:", err)
		os.Exit(1)
	}
}

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	porta := fs.Int("porta", config.Porta(), "porta em 127.0.0.1")
	oculto := fs.Bool("oculto", false, "no Windows, corre sem janela de consola (usado pela tarefa agendada)")
	fs.Parse(args)
	if *oculto && relancarSemConsola(args) {
		return nil
	}
	token, _, err := config.GarantirToken()
	if err != nil {
		return err
	}
	home, err := config.Home()
	if err != nil {
		return err
	}
	claude, err := config.ClaudeDir()
	if err != nil {
		return err
	}
	srv, err := servico.Novo(servico.Opcoes{Porta: *porta, Token: token, Home: home, ClaudeDir: claude,
		PortaRede: config.PortaRede()})
	if err != nil {
		return err
	}
	ctx, parar := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer parar()
	return srv.Executar(ctx, func(p int) {
		fmt.Fprintf(os.Stderr, "towerman: a escutar em 127.0.0.1:%d (quadro em %s)\n", p, filepath.Join(home, "quadro.db"))
	})
}

func caminhoSettings(valor string) (string, error) {
	if valor != "" {
		return valor, nil
	}
	return instalar.CaminhoSettingsPadrao()
}

func install(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	settings := fs.String("settings", "", "settings.json a alterar (padrão ~/.claude/settings.json)")
	semServico := fs.Bool("sem-servico", false, "não registar o serviço no arranque da sessão")
	semExtensao := fs.Bool("sem-extensao", false, "não instalar a extensão nos IDEs (VS Code, Antigravity IDE…)")
	fs.Parse(args)

	p, err := caminhoSettings(*settings)
	if err != nil {
		return err
	}
	_, gerado, err := config.GarantirToken()
	if err != nil {
		return err
	}
	if gerado {
		fmt.Fprintln(out, "token gerado")
	}
	r, err := instalar.Instalar(p)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "%s: %d eventos acrescentados", p, len(r.Acrescentados))
	if len(r.Atualizados) > 0 {
		fmt.Fprintf(out, ", %d atualizados", len(r.Atualizados))
	}
	if len(r.Existentes) > 0 {
		fmt.Fprintf(out, ", %d já existiam", len(r.Existentes))
	}
	fmt.Fprintln(out)
	if pc := instalar.CaminhoHooksCodex(); pc != "" {
		if rc, err := instalar.InstalarCodex(pc); err != nil {
			fmt.Fprintf(out, "aviso: hooks do Codex não instalados em %s: %v\n", pc, err)
		} else {
			fmt.Fprintf(out, "%s: hooks do Codex: %d acrescentados, %d atualizados, %d já existiam\n", pc,
				len(rc.Acrescentados), len(rc.Atualizados), len(rc.Existentes))
			if rc.Escrito {
				fmt.Fprintln(out, "  no Codex, abra /hooks uma vez e confie nos hooks do towerman (o Codex só corre hooks revistos)")
			}
		}
	}
	if pa := instalar.CaminhoHooksAgy(); pa != "" {
		if escrito, err := instalar.InstalarAgy(pa); err != nil {
			fmt.Fprintf(out, "aviso: hooks do Antigravity não instalados em %s: %v\n", pa, err)
		} else if escrito {
			fmt.Fprintf(out, "%s: hooks do Antigravity (agy e IDE) instalados\n", pa)
		} else {
			fmt.Fprintf(out, "%s: hooks do Antigravity já existiam\n", pa)
		}
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return err
	}
	if _, err := exec.LookPath("towerman"); err != nil {
		fmt.Fprintf(out, "aviso: \"towerman\" não está no PATH; os hooks só funcionam quando %s estiver acessível como \"towerman\"\n", filepath.Dir(exe))
	}
	if msg := instalar.GarantirPathSistema(exe); msg != "" {
		fmt.Fprintln(out, msg)
	}
	if msg := instalar.GarantirTmux(instalar.TemTerminal()); msg != "" {
		fmt.Fprintln(out, msg)
	}
	if !*semExtensao {
		instalarExtensao(out)
	}
	if msg, err := instalar.InstalarAtalho(exe); err != nil {
		fmt.Fprintln(out, "aviso: atalho de aplicação não criado:", err)
	} else if msg != "" {
		fmt.Fprintln(out, msg)
	}
	if home, err := config.Home(); err == nil {
		if bin, err := servico.PrepararTunel(home); err != nil {
			fmt.Fprintln(out, "aviso: túnel para fora de casa indisponível:", err)
		} else if bin != "" {
			fmt.Fprintln(out, "acesso fora de casa: túnel da Cloudflare com", bin)
		}
	}
	if *semServico {
		return nil
	}
	msg, err := instalar.RegistarServico(exe)
	if err != nil {
		fmt.Fprintln(out, "aviso: serviço não registado:", strings.TrimSpace(err.Error()))
		return nil
	}
	fmt.Fprintln(out, msg)
	return nil
}

func uninstall(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("uninstall", flag.ExitOnError)
	settings := fs.String("settings", "", "settings.json a alterar (padrão ~/.claude/settings.json)")
	semServico := fs.Bool("sem-servico", false, "não mexer no serviço")
	semExtensao := fs.Bool("sem-extensao", false, "não remover a extensão dos IDEs")
	purgar := fs.Bool("purgar", false, "apagar também o quadro, o diário e o token")
	fs.Parse(args)

	p, err := caminhoSettings(*settings)
	if err != nil {
		return err
	}
	r, err := instalar.Desinstalar(p)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "%s: %d eventos limpos\n", p, len(r.Removidos))
	if pc := instalar.CaminhoHooksCodex(); pc != "" {
		if rc, err := instalar.Desinstalar(pc); err == nil && len(rc.Removidos) > 0 {
			fmt.Fprintf(out, "%s: hooks do Codex removidos\n", pc)
		}
	}
	if pa := instalar.CaminhoHooksAgy(); pa != "" {
		if removido, err := instalar.DesinstalarAgy(pa); err == nil && removido {
			fmt.Fprintf(out, "%s: hooks do Antigravity removidos\n", pa)
		}
	}
	if !*semExtensao {
		for _, x := range extensao.Remover() {
			if x.Erro != nil {
				fmt.Fprintf(out, "aviso: extensão não removida de %s: %v\n", x.IDE, x.Erro)
			} else {
				fmt.Fprintf(out, "extensão removida de %s\n", x.IDE)
			}
		}
	}
	instalar.RemoverAtalho()
	if !*semServico {
		msg, err := instalar.RemoverServico()
		if err != nil {
			return err
		}
		fmt.Fprintln(out, msg)
	}
	if *purgar {
		home, err := config.Home()
		if err != nil {
			return err
		}
		if err := os.RemoveAll(home); err != nil {
			return err
		}
		fmt.Fprintln(out, "apagado:", home)
	}
	return nil
}

func painel(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("painel", flag.ExitOnError)
	semAbrir := fs.Bool("sem-abrir", false, "só imprimir o URL")
	fs.Parse(args)
	token, err := config.LerToken()
	if err != nil {
		return fmt.Errorf("sem token (%w); corre primeiro \"towerman install\"", err)
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/?token=%s", config.Porta(), token)
	fmt.Fprintln(out, url)
	if *semAbrir {
		return nil
	}
	var abrir *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		abrir = exec.Command("open", url)
	case "windows":
		abrir = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		abrir = exec.Command("xdg-open", url)
	}
	if err := abrir.Start(); err != nil {
		fmt.Fprintln(out, "não consegui abrir o browser; abre o URL acima à mão")
		return nil
	}
	go abrir.Wait()
	return nil
}

func instalarExtensao(out io.Writer) {
	tmp, err := os.MkdirTemp("", "towerman-vsix")
	if err != nil {
		fmt.Fprintln(out, "aviso: extensão não instalada:", err)
		return
	}
	defer os.RemoveAll(tmp)
	rs, err := extensao.Instalar(tmp)
	if err != nil {
		fmt.Fprintln(out, "aviso: extensão não instalada:", err)
		return
	}
	if len(rs) == 0 {
		fmt.Fprintln(out, "nenhum IDE da família VS Code encontrado; «ir para a janela» fica limitado")
	}
	for _, x := range rs {
		if x.Erro != nil {
			fmt.Fprintf(out, "aviso: extensão não instalada em %s: %v\n", x.IDE, x.Erro)
			continue
		}
		fmt.Fprintf(out, "extensão %s instalada em %s (as janelas já abertas apanham-na ao recarregar)\n", extensao.Versao(), x.IDE)
	}
}

func regra2(args []string, out io.Writer) error {
	p := config.LerPreferencias()
	if len(args) == 0 {
		fmt.Fprintln(out, "regra2:", p.Regra2)
		return nil
	}
	switch args[0] {
	case config.Regra2Pedir, config.Regra2Recusar, config.Regra2Off:
	default:
		return fmt.Errorf("modo desconhecido %q: usa ask, deny ou off", args[0])
	}
	p.Regra2 = args[0]
	home, err := config.Home()
	if err != nil {
		return err
	}
	if err := config.GuardarPreferenciasEm(home, p); err != nil {
		return err
	}
	fmt.Fprintln(out, "regra2:", p.Regra2)
	return nil
}
