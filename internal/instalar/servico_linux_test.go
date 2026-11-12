//go:build linux

package instalar

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegistarERemoverServicoLinux(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("TOWERMAN_HOME", "/tmp/m h")
	t.Setenv("TOWERMAN_PORTA", "")
	t.Setenv("USER", "teste")
	var chamadas []string
	original := executar
	executar = func(nome string, args ...string) error {
		chamadas = append(chamadas, nome+" "+strings.Join(args, " "))
		return nil
	}
	t.Cleanup(func() { executar = original })

	if _, err := RegistarServico("/opt/x y/towerman"); err != nil {
		t.Fatal(err)
	}
	unit, err := os.ReadFile(filepath.Join(cfg, "systemd", "user", "towerman.service"))
	if err != nil {
		t.Fatal(err)
	}
	for _, esperado := range []string{`ExecStart="/opt/x y/towerman" serve`, `Environment="TOWERMAN_HOME=/tmp/m h"`, "WantedBy=default.target"} {
		if !strings.Contains(string(unit), esperado) {
			t.Errorf("unit sem %q:\n%s", esperado, unit)
		}
	}
	if strings.Join(chamadas, ";") != "systemctl --user daemon-reload;systemctl --user enable towerman.service;systemctl --user restart towerman.service;loginctl enable-linger teste" {
		t.Fatalf("chamadas inesperadas: %v", chamadas)
	}

	chamadas = nil
	if _, err := RemoverServico(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cfg, "systemd", "user", "towerman.service")); !os.IsNotExist(err) {
		t.Fatal("unit devia ter sido removida")
	}
	if len(chamadas) != 2 || !strings.Contains(chamadas[0], "disable --now") {
		t.Fatalf("chamadas inesperadas: %v", chamadas)
	}
}
