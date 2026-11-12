package instalar

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func existeSo(nomes ...string) func(string) bool {
	return func(n string) bool {
		for _, x := range nomes {
			if x == n {
				return true
			}
		}
		return false
	}
}

func TestComandoTmux(t *testing.T) {
	casos := []struct {
		existe []string
		root   bool
		want   []string
	}{
		{[]string{"apt-get", "sudo"}, false, []string{"sudo", "apt-get", "install", "-y", "tmux"}},
		{[]string{"dnf"}, true, []string{"dnf", "install", "-y", "tmux"}},
		{[]string{"pacman"}, false, []string{"sudo", "pacman", "-S", "--needed", "--noconfirm", "tmux"}},
		{[]string{"zypper"}, false, []string{"sudo", "zypper", "--non-interactive", "install", "tmux"}},
		{nil, false, nil},
	}
	for _, c := range casos {
		if got := comandoTmux(existeSo(c.existe...), c.root); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%v root=%v: got %v, want %v", c.existe, c.root, got, c.want)
		}
	}
}

func TestGarantirTmux(t *testing.T) {
	nunca := func([]string) error { t.Fatal("não devia correr nada"); return nil }

	if msg := garantirTmux(existeSo("tmux", "apt-get"), false, true, nunca); msg != "" {
		t.Fatalf("com tmux não há nada a fazer: %q", msg)
	}
	// Sem terminal: só o aviso com o comando.
	msg := garantirTmux(existeSo("apt-get", "sudo"), false, false, nunca)
	if !strings.HasPrefix(msg, "aviso:") || !strings.Contains(msg, "sudo apt-get install -y tmux") {
		t.Fatalf("sem tty: %q", msg)
	}
	// Sem sudo e sem ser root: também só o aviso.
	if msg := garantirTmux(existeSo("apt-get"), false, true, nunca); !strings.Contains(msg, "sudo apt-get install -y tmux") {
		t.Fatalf("sem sudo: %q", msg)
	}
	if msg := garantirTmux(existeSo(), false, true, nunca); !strings.HasPrefix(msg, "aviso:") {
		t.Fatalf("sem gestor de pacotes: %q", msg)
	}

	// Com terminal: corre o gestor e confirma que o tmux ficou.
	instalado := false
	existe := func(n string) bool { return n == "apt-get" || n == "sudo" || (n == "tmux" && instalado) }
	var corrido []string
	msg = garantirTmux(existe, false, true, func(c []string) error { corrido, instalado = c, true; return nil })
	if !reflect.DeepEqual(corrido, []string{"sudo", "apt-get", "install", "-y", "tmux"}) || !strings.HasPrefix(msg, "tmux instalado") {
		t.Fatalf("instalação: %v %q", corrido, msg)
	}
	// O gestor falha (palavra-passe errada, sem rede): aviso, nunca erro.
	msg = garantirTmux(existeSo("apt-get", "sudo"), false, true, func([]string) error { return errors.New("exit status 1") })
	if !strings.HasPrefix(msg, "aviso: tmux não instalado") || !strings.Contains(msg, "sudo apt-get install -y tmux") {
		t.Fatalf("falha: %q", msg)
	}
}
