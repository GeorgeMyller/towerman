package servico

import (
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strings"

	"github.com/GeorgeMyller/towerman/internal/processo"
	"github.com/GeorgeMyller/towerman/internal/quadro"
)

// Quando o focar não encontra a janela da sessão, o Towerman abre uma:
//   - sessão num tmux sem janela à vista: um terminal novo ligado a esse tmux, no painel certo;
//   - sessão terminada (o processo já não existe): um terminal novo que a retoma
//     (claude --resume, codex resume).
// Uma sessão viva fora do tmux não é duplicada: retomá-la ao lado da original misturava as duas.

// comandoAbrir diz o que correr para voltar a ver a sessão, ou "" quando não há forma segura.
func comandoAbrir(se quadro.Sessao, t terminal, tmuxSessao func(painel string) string, vivo func(int) bool) string {
	if t.TmuxPane != "" {
		if nome := tmuxSessao(t.TmuxPane); nome != "" {
			return "tmux attach-session -t " + aspas("="+nome)
		}
	}
	if vivo(se.Pid) {
		return ""
	}
	switch se.Agente {
	case "", "claude":
		return "claude --resume " + aspas(se.SessionID)
	case "codex":
		return "codex resume " + aspas(se.SessionID)
	}
	return ""
}

// tmuxSessaoDe devolve a sessão tmux do painel e deixa esse painel ativo (o attach mostra-o).
func tmuxSessaoDe(painel string) string {
	out, err := exec.Command("tmux", "display-message", "-p", "-t", painel, "#{session_name}").Output()
	if err != nil {
		return ""
	}
	exec.Command("tmux", "select-window", "-t", painel).Run()
	exec.Command("tmux", "select-pane", "-t", painel).Run()
	return strings.TrimSpace(string(out))
}

// abrirJanela abre um terminal novo para a sessão: pelo IDE (extensão ligada) ou pelo terminal
// do sistema. Devolve como abriu.
func (s *Servico) abrirJanela(se quadro.Sessao, t terminal) (string, error) {
	cmd := comandoAbrir(se, t, tmuxSessaoDe, processo.Vivo)
	if cmd == "" {
		return "", fmt.Errorf("a sessão está a correr num terminal sem janela que o Towerman consiga abrir")
	}
	if t.TmuxPane == "" && s.ext.ligadas() > 0 {
		if _, ok := s.ext.pedirAbrir(se.Cwd, cmd, esperaExtensao); ok {
			return "terminal novo no IDE", nil
		}
	}
	nome := ""
	if runtime.GOOS == "linux" && t.TmuxPane == "" && temTmux() {
		nome = nomeSessaoTmux(se.SessionID)
	}
	if err := abrirTerminal(se.Cwd, cmd, nome); err != nil {
		return "", err
	}
	return "terminal novo", nil
}

// abrirTerminal corre cmd numa janela nova do terminal do sistema, na pasta cwd. No Linux, com
// nomeTmux, o comando corre dentro dessa sessão tmux (ver linhaLinux).
func abrirTerminal(cwd, cmd, nomeTmux string) error {
	if runtime.GOOS == "windows" {
		return abrirTerminalWindows(cwd, cmd)
	}
	linha := cmd
	if cwd != "" {
		linha = "cd " + aspas(cwd) + " && " + cmd
	}
	if runtime.GOOS == "darwin" {
		script := fmt.Sprintf("tell application \"Terminal\"\n do script %q\n activate\nend tell", linha)
		if out, err := exec.Command("osascript", "-e", script).CombinedOutput(); err != nil {
			return fmt.Errorf("Terminal: %v: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	// Linux: o primeiro emulador de terminal que existir.
	garantirAmbienteGrafico()
	sh := linhaLinux(cwd, cmd, nomeTmux)
	for _, c := range [][]string{
		{"x-terminal-emulator", "-e", "sh", "-c", sh},
		{"gnome-terminal", "--", "sh", "-c", sh},
		{"konsole", "-e", "sh", "-c", sh},
		{"xfce4-terminal", "-x", "sh", "-c", sh},
		{"kitty", "sh", "-c", sh},
		{"alacritty", "-e", "sh", "-c", sh},
		{"xterm", "-e", "sh", "-c", sh},
	} {
		if _, err := exec.LookPath(c[0]); err != nil {
			continue
		}
		if err := exec.Command(c[0], c[1:]...).Start(); err == nil {
			return nil
		}
	}
	return fmt.Errorf("não encontrei um terminal para abrir (instale gnome-terminal, konsole ou xterm)")
}

// O PATH do systemd não tem ~/.local/bin nem ~/go/bin, onde ficam o claude, o agy e o towerman.
const pathLinux = `PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"; `

// linhaLinux é o script de sh que o terminal do Linux corre. O shell fica aberto quando o
// comando acaba. Com nomeTmux, o comando corre numa sessão tmux com esse nome (ou liga-se a
// ela, se já existir): o hook vê o TMUX_PANE e o Towerman passa a poder escrever na sessão
// parada. O PATH repete-se dentro do tmux, porque um servidor tmux já aberto tem o seu ambiente.
func linhaLinux(cwd, cmd, nomeTmux string) string {
	if nomeTmux == "" {
		linha := cmd
		if cwd != "" {
			linha = "cd " + aspasPara("linux", cwd) + " && " + cmd
		}
		return pathLinux + linha + `; exec "${SHELL:-sh}"`
	}
	dentro := pathLinux + cmd + `; exec "${SHELL:-sh}"`
	tmux := "tmux new-session -A -s " + aspasPara("linux", nomeTmux)
	if cwd != "" {
		tmux += " -c " + aspasPara("linux", cwd)
	}
	// Sem o TMUX do serviço, o tmux não recusa abrir uma sessão «dentro de outra».
	return pathLinux + "unset TMUX; " + tmux + " " + aspasPara("linux", dentro) + ` || exec "${SHELL:-sh}"`
}

var foraNomeTmux = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// nomeSessaoTmux dá à sessão tmux um nome curto e estável: towerman-<8 primeiros do id>.
// O tmux não aceita «.» nem «:» nos nomes.
func nomeSessaoTmux(id string) string {
	id = foraNomeTmux.ReplaceAllString(id, "")
	if len(id) > 8 {
		id = id[:8]
	}
	if id == "" {
		return "towerman"
	}
	return "towerman-" + id
}

// temTmux diz se o tmux está instalado (o install do Linux tenta instalá-lo).
var temTmux = func() bool { _, err := exec.LookPath("tmux"); return err == nil }

// abrirTerminalWindows abre o Windows Terminal (wt.exe) na pasta cwd, ou uma janela do cmd
// quando ele não existe. «cmd /k» deixa a janela aberta quando o comando acaba.
func abrirTerminalWindows(cwd, cmd string) error {
	if wt, err := exec.LookPath("wt"); err == nil {
		args := []string{"cmd", "/k", cmd}
		if cwd != "" {
			args = append([]string{"-d", cwd}, args...)
		}
		return exec.Command(wt, args...).Start()
	}
	// O primeiro argumento entre aspas do «start» é o título; com um espaço, o Go põe-lhe aspas.
	args := []string{"/c", "start", "Towerman - sessão"}
	if cwd != "" {
		args = append(args, "/D", cwd)
	}
	return exec.Command("cmd", append(args, "cmd", "/k", cmd)...).Start()
}

// aspas protege um argumento para a shell do sistema.
func aspas(s string) string { return aspasPara(runtime.GOOS, s) }

var semAspas = regexp.MustCompile(`^[A-Za-z0-9._:=/\\-]+$`)

// aspasPara: aspas simples para sh; no Windows (cmd), aspas duplas, e só quando fazem falta.
func aspasPara(goos, s string) string {
	if goos == "windows" {
		if semAspas.MatchString(s) {
			return s
		}
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
