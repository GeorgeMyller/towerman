package instalar

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstalarCodex(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hooks.json")
	os.WriteFile(p, []byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"notify-send feito"}]}]}}`), 0o600)

	r, err := InstalarCodex(p)
	if err != nil || !r.Escrito || len(r.Acrescentados) != len(eventosCodex) {
		t.Fatalf("devia acrescentar %d eventos: %+v %v", len(eventosCodex), r, err)
	}
	if r2, _ := InstalarCodex(p); r2.Escrito {
		t.Fatal("segunda instalação não devia reescrever")
	}
	dados, _ := os.ReadFile(p)
	var v struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string
				Timeout json.Number
			}
		}
	}
	json.Unmarshal(dados, &v)
	if len(v.Hooks["Stop"]) != 2 || v.Hooks["Stop"][0].Hooks[0].Command != "notify-send feito" {
		t.Fatalf("o hook do utilizador devia ficar: %s", dados)
	}
	pr := v.Hooks["PermissionRequest"][0].Hooks[0]
	if pr.Command != ComandoHookCodex || pr.Timeout != "610" {
		t.Fatalf("PermissionRequest do Codex errado: %+v", pr)
	}
	if _, tem := v.Hooks["Notification"]; tem {
		t.Fatal("o Codex não tem Notification")
	}
	rd, err := Desinstalar(p)
	if err != nil || len(rd.Removidos) != len(eventosCodex) {
		t.Fatalf("o uninstall devia reconhecer «towerman hook --agente codex»: %+v %v", rd, err)
	}
	dados, _ = os.ReadFile(p)
	if strings.Contains(string(dados), "towerman") || !strings.Contains(string(dados), "notify-send") {
		t.Fatalf("só as entradas do towerman saem: %s", dados)
	}
}
