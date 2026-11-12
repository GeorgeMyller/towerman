//go:build windows

package hook

import "testing"

func TestInatividadeWindows(t *testing.T) {
	d, ok := Inatividade()
	if !ok {
		t.Skip("GetLastInputInfo indisponível (sessão sem ambiente interativo)")
	}
	if d < 0 {
		t.Fatalf("inatividade negativa: %s", d)
	}
}
