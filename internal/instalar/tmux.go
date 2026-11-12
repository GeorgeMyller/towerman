package instalar

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// No Linux, o Towerman só escreve no terminal de uma sessão parada (a resposta vinda do
// telemóvel) quando ela corre no tmux. O focar abre as sessões dentro do tmux, por isso o
// install tenta instalá-lo. Falhar aqui nunca falha o install: fica um aviso com o comando.

// gestoresTmux: o primeiro gestor de pacotes que existir instala o tmux.
var gestoresTmux = [][]string{
	{"apt-get", "install", "-y", "tmux"},
	{"dnf", "install", "-y", "tmux"},
	{"pacman", "-S", "--needed", "--noconfirm", "tmux"},
	{"zypper", "--non-interactive", "install", "tmux"},
}

// comandoTmux devolve o comando que instala o tmux nesta distribuição (com sudo quando não é
// root), ou nil quando não reconhece o gestor de pacotes.
func comandoTmux(existe func(string) bool, root bool) []string {
	for _, g := range gestoresTmux {
		if !existe(g[0]) {
			continue
		}
		if root {
			return g
		}
		return append([]string{"sudo"}, g...)
	}
	return nil
}

// GarantirTmux instala o tmux no Linux quando falta. Com um terminal (tty), corre o gestor de
// pacotes com sudo, que pede a palavra-passe; sem tty, ou se falhar, devolve o aviso com o
// comando a correr à mão. Devolve "" quando o tmux já existe ou fora do Linux.
func GarantirTmux(tty bool) string {
	if runtime.GOOS != "linux" {
		return ""
	}
	existe := func(n string) bool { _, err := exec.LookPath(n); return err == nil }
	return garantirTmux(existe, os.Geteuid() == 0, tty, correrInterativo)
}

func garantirTmux(existe func(string) bool, root, tty bool, correr func([]string) error) string {
	if existe("tmux") {
		return ""
	}
	const porque = "sem tmux, as respostas do telemóvel a uma sessão parada ficam na fila"
	cmd := comandoTmux(existe, root)
	if cmd == nil {
		return "aviso: " + porque + "; instale o tmux com o gestor de pacotes da distribuição"
	}
	linha := strings.Join(cmd, " ")
	if !tty || (cmd[0] == "sudo" && !existe("sudo")) {
		return "aviso: " + porque + "; instale-o com: " + linha
	}
	if err := correr(cmd); err != nil || !existe("tmux") {
		return fmt.Sprintf("aviso: tmux não instalado (%v); %s; instale-o com: %s", err, porque, linha)
	}
	return "tmux instalado (" + linha + ")"
}

// correrInterativo corre o comando ligado ao terminal: o sudo pede a palavra-passe e o
// gestor de pacotes mostra o progresso.
func correrInterativo(cmd []string) error {
	c := exec.Command(cmd[0], cmd[1:]...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}

// TemTerminal diz se a entrada padrão é um terminal (há alguém para escrever a palavra-passe).
func TemTerminal() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
