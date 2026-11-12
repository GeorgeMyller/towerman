package instalar

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInstalarAgyPreservaOutrosHooks(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config", "hooks.json")
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte(`{"meu-hook":{"Stop":[{"type":"command","command":"say feito"}]}}`), 0o600)

	escrito, err := InstalarAgy(p)
	if err != nil || !escrito {
		t.Fatalf("devia instalar: %v", err)
	}
	if escrito, _ := InstalarAgy(p); escrito {
		t.Fatal("segunda instalação não devia reescrever")
	}
	var v map[string]map[string]any
	dados, _ := os.ReadFile(p)
	if err := json.Unmarshal(dados, &v); err != nil {
		t.Fatal(err)
	}
	if v["meu-hook"] == nil || v["towerman"]["PreToolUse"] == nil || v["towerman"]["Stop"] == nil {
		t.Fatalf("hooks errados: %s", dados)
	}
	if removido, _ := DesinstalarAgy(p); !removido {
		t.Fatal("devia remover")
	}
	dados, _ = os.ReadFile(p)
	v = nil // Unmarshal num mapa já cheio manteria a chave antiga
	json.Unmarshal(dados, &v)
	if _, ainda := v["towerman"]; ainda || v["meu-hook"] == nil {
		t.Fatalf("só a entrada do towerman sai: %s", dados)
	}
}
