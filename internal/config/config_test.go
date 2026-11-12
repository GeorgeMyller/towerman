package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Sem config.json, o acesso fora de casa fica desligado: não há relay por omissão.
func TestPreferenciasSemRelayPorOmissao(t *testing.T) {
	p := LerPreferenciasEm(t.TempDir())
	if p.RelayURL != "" || p.RelayToken != "" {
		t.Fatalf("esperava relay desligado, obteve %+v", p)
	}
	if p.Regra2 != Regra2Pedir {
		t.Fatalf("regra2 padrão: %q", p.Regra2)
	}
}

func TestPreferenciasRelayEToken(t *testing.T) {
	h := t.TempDir()
	conteudo := `{"regra2":"deny","relay_url":" https://relay.example.com ","relay_token":" tk1 "}`
	if err := os.WriteFile(filepath.Join(h, "config.json"), []byte(conteudo), 0o600); err != nil {
		t.Fatal(err)
	}
	p := LerPreferenciasEm(h)
	if p.RelayURL != "https://relay.example.com" || p.RelayToken != "tk1" {
		t.Fatalf("obteve %+v", p)
	}
	// Guardar outra preferência não perde o relay nem o token.
	p.Regra2 = Regra2Off
	if err := GuardarPreferenciasEm(h, p); err != nil {
		t.Fatal(err)
	}
	if q := LerPreferenciasEm(h); q.RelayURL != p.RelayURL || q.RelayToken != "tk1" || q.Regra2 != Regra2Off {
		t.Fatalf("depois de guardar: %+v", q)
	}
}

// Um config.json sem relay não ganha relay_url ao guardar outra preferência.
func TestGuardarSemRelayNaoEscreveURL(t *testing.T) {
	h := t.TempDir()
	if err := GuardarPreferenciasEm(h, LerPreferenciasEm(h)); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(h, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "relay") {
		t.Fatalf("config.json não devia ter relay: %s", b)
	}
}
