//go:build linux

package instalar

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/GeorgeMyller/towerman/internal/config"
)

const nomeUnit = "towerman.service"

func caminhoUnit() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(h, ".config")
	}
	return filepath.Join(base, "systemd", "user", nomeUnit), nil
}

func conteudoUnit(executavel string) string {
	var b strings.Builder
	b.WriteString("[Unit]\nDescription=Towerman: quadro das sessões de agentes\n\n[Service]\n")
	for _, kv := range variaveisServico() {
		fmt.Fprintf(&b, "Environment=%q\n", kv[0]+"="+kv[1])
	}
	fmt.Fprintf(&b, "ExecStart=%q serve\nRestart=on-failure\nRestartSec=2\n\n[Install]\nWantedBy=default.target\n", executavel)
	return b.String()
}

func RegistarServico(executavel string) (string, error) {
	p, err := caminhoUnit()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	if err := config.EscreverAtomico(p, []byte(conteudoUnit(executavel)), 0o644); err != nil {
		return "", err
	}
	for _, args := range [][]string{
		{"--user", "daemon-reload"},
		{"--user", "enable", nomeUnit},
		{"--user", "restart", nomeUnit},
	} {
		if err := executar("systemctl", args...); err != nil {
			return "", fmt.Errorf("%w\nunit escrita em %s; arranca à mão com: systemctl --user enable --now %s", err, p, nomeUnit)
		}
	}
	msg := "serviço systemd do utilizador ativo: " + p
	// Sem «linger», o systemd pára o serviço quando a sessão gráfica fecha, e o iPhone deixa
	// de chegar ao computador fora de casa (ou com o ecrã bloqueado na sessão terminada).
	if u := os.Getenv("USER"); u != "" {
		if err := executar("loginctl", "enable-linger", u); err != nil {
			msg += "\naviso: o serviço pára quando sair da sessão; para o manter sempre: sudo loginctl enable-linger " + u
		} else {
			msg += "\nserviço mantido ativo fora da sessão (loginctl enable-linger)"
		}
	}
	return msg, nil
}

func RemoverServico() (string, error) {
	p, err := caminhoUnit()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(p); errors.Is(err, fs.ErrNotExist) {
		return "sem serviço registado", nil
	}
	executar("systemctl", "--user", "disable", "--now", nomeUnit)
	if err := os.Remove(p); err != nil {
		return "", err
	}
	executar("systemctl", "--user", "daemon-reload")
	return "serviço systemd removido: " + p, nil
}
