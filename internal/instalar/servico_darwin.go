//go:build darwin

package instalar

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/GeorgeMyller/towerman/internal/config"
)

const rotuloLaunchd = "io.github.georgemyller.towerman"

// rotuloLaunchdAntigo é o rótulo das versões anteriores; install e uninstall removem-no (migração).
const rotuloLaunchdAntigo = "legado.towerman"

func caminhoPlist() (string, error) { return caminhoPlistDe(rotuloLaunchd) }

func caminhoPlistDe(rotulo string) (string, error) {
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, "Library", "LaunchAgents", rotulo+".plist"), nil
}

// removerServicoAntigo descarrega e apaga o agente com o rótulo antigo, se existir. Devolve o
// caminho apagado ("" quando não havia nada).
func removerServicoAntigo() (string, error) {
	p, err := caminhoPlistDe(rotuloLaunchdAntigo)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(p); errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	executar("launchctl", "unload", "-w", p)
	if err := os.Remove(p); err != nil {
		return "", err
	}
	return p, nil
}

func escapar(s string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(s))
	return b.String()
}

func conteudoPlist(executavel string) string {
	var env strings.Builder
	if vs := variaveisServico(); len(vs) > 0 {
		env.WriteString("  <key>EnvironmentVariables</key>\n  <dict>\n")
		for _, kv := range vs {
			fmt.Fprintf(&env, "    <key>%s</key><string>%s</string>\n", escapar(kv[0]), escapar(kv[1]))
		}
		env.WriteString("  </dict>\n")
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>%s</string>
  <key>ProgramArguments</key>
  <array><string>%s</string><string>serve</string></array>
%s  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
</dict>
</plist>
`, rotuloLaunchd, escapar(executavel), env.String())
}

func RegistarServico(executavel string) (string, error) {
	p, err := caminhoPlist()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	// O agente antigo tem de sair antes: dois agentes a servir a mesma porta disputam-na.
	if _, err := removerServicoAntigo(); err != nil {
		return "", err
	}
	if err := config.EscreverAtomico(p, []byte(conteudoPlist(executavel)), 0o644); err != nil {
		return "", err
	}
	executar("launchctl", "unload", p)
	if err := executar("launchctl", "load", "-w", p); err != nil {
		return "", fmt.Errorf("%w\nplist escrito em %s", err, p)
	}
	return "agente launchd ativo: " + p, nil
}

func RemoverServico() (string, error) {
	p, err := caminhoPlist()
	if err != nil {
		return "", err
	}
	antigo, err := removerServicoAntigo()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(p); errors.Is(err, fs.ErrNotExist) {
		if antigo != "" {
			return "agente launchd removido: " + antigo, nil
		}
		return "sem serviço registado", nil
	}
	executar("launchctl", "unload", "-w", p)
	if err := os.Remove(p); err != nil {
		return "", err
	}
	if antigo != "" {
		return "agentes launchd removidos: " + p + ", " + antigo, nil
	}
	return "agente launchd removido: " + p, nil
}
