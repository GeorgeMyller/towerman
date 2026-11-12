//go:build darwin

package instalar

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func espiarExecutar(t *testing.T) *[]string {
	t.Helper()
	var chamadas []string
	original := executar
	executar = func(nome string, args ...string) error {
		chamadas = append(chamadas, nome+" "+strings.Join(args, " "))
		return nil
	}
	t.Cleanup(func() { executar = original })
	return &chamadas
}

// O install troca o agente antigo (legado.towerman) pelo novo rótulo.
func TestRegistarServicoDarwinMigraRotuloAntigo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("TOWERMAN_HOME", "")
	t.Setenv("TOWERMAN_PORTA", "")
	t.Setenv("TOWERMAN_CLAUDE_DIR", "")
	t.Setenv("TOWERMAN_PORTA_REDE", "")
	t.Setenv("TOWERMAN_TUNEL", "")
	agentes := filepath.Join(home, "Library", "LaunchAgents")
	os.MkdirAll(agentes, 0o755)
	antigo := filepath.Join(agentes, "legado.towerman.plist")
	os.WriteFile(antigo, []byte("<plist/>"), 0o644)
	chamadas := espiarExecutar(t)

	if _, err := RegistarServico("/opt/towerman"); err != nil {
		t.Fatal(err)
	}
	novo := filepath.Join(agentes, "io.github.georgemyller.towerman.plist")
	b, err := os.ReadFile(novo)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "<string>io.github.georgemyller.towerman</string>") {
		t.Fatalf("plist sem o rótulo novo:\n%s", b)
	}
	if _, err := os.Stat(antigo); !os.IsNotExist(err) {
		t.Fatal("o plist antigo devia ter sido apagado")
	}
	esperado := "launchctl unload -w " + antigo + ";launchctl unload " + novo + ";launchctl load -w " + novo
	if strings.Join(*chamadas, ";") != esperado {
		t.Fatalf("chamadas inesperadas: %v", *chamadas)
	}
}

// O uninstall remove o agente novo e o antigo; sem nenhum, não chama o launchctl.
func TestRemoverServicoDarwinRemoveAntigo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	agentes := filepath.Join(home, "Library", "LaunchAgents")
	os.MkdirAll(agentes, 0o755)
	antigo := filepath.Join(agentes, "legado.towerman.plist")
	novo := filepath.Join(agentes, "io.github.georgemyller.towerman.plist")
	os.WriteFile(antigo, []byte("<plist/>"), 0o644)
	os.WriteFile(novo, []byte("<plist/>"), 0o644)
	chamadas := espiarExecutar(t)

	if _, err := RemoverServico(); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{antigo, novo} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s devia ter sido apagado", p)
		}
	}
	if len(*chamadas) != 2 {
		t.Fatalf("chamadas inesperadas: %v", *chamadas)
	}

	*chamadas = nil
	msg, err := RemoverServico()
	if err != nil || msg != "sem serviço registado" || len(*chamadas) != 0 {
		t.Fatalf("sem serviço: %q %v %v", msg, err, *chamadas)
	}

	// Só o antigo presente: o uninstall também o apaga.
	os.WriteFile(antigo, []byte("<plist/>"), 0o644)
	if msg, err := RemoverServico(); err != nil || !strings.Contains(msg, antigo) {
		t.Fatalf("só antigo: %q %v", msg, err)
	}
	if _, err := os.Stat(antigo); !os.IsNotExist(err) {
		t.Fatal("o plist antigo devia ter sido apagado")
	}
}
