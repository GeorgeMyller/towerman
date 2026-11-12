//go:build windows

package instalar

import (
	"errors"
	"strings"
	"testing"
)

func TestRegistarServicoWindows(t *testing.T) {
	t.Setenv("TOWERMAN_HOME", "")
	t.Setenv("TOWERMAN_PORTA", "")
	t.Setenv("TOWERMAN_PORTA_REDE", "7352")
	var chamadas []string
	semAdmin := false
	original := executar
	executar = func(nome string, args ...string) error {
		linha := nome + " " + strings.Join(args, " ")
		chamadas = append(chamadas, linha)
		if semAdmin && strings.Contains(linha, "add rule") {
			return errors.New("A operação requer elevação")
		}
		return nil
	}
	t.Cleanup(func() { executar = original })

	msg, err := RegistarServico(`C:\go\bin\towerman.exe`)
	if err != nil {
		t.Fatal(err)
	}
	tudo := strings.Join(chamadas, "\n")
	for _, esperado := range []string{`serve --oculto`, "name=Towerman dir=in action=allow protocol=TCP localport=7352 profile=private"} {
		if !strings.Contains(tudo, esperado) {
			t.Errorf("faltou %q nas chamadas:\n%s", esperado, tudo)
		}
	}
	if !strings.Contains(msg, "firewall: porta 7352") {
		t.Errorf("mensagem sem a firewall: %q", msg)
	}

	semAdmin = true
	msg, err = RegistarServico(`C:\go\bin\towerman.exe`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "administrador") || !strings.Contains(msg, "netsh advfirewall") {
		t.Errorf("sem elevação, esperava o comando netsh: %q", msg)
	}
}
