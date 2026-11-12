package hook

import "testing"

func TestLerIdleGdbus(t *testing.T) {
	casos := []struct {
		saida string
		n     uint64
		ok    bool
	}{
		{"(uint64 1234,)\n", 1234, true}, // Mutter: ms
		{"(uint32 5,)", 5, true},         // ScreenSaver: s
		{"(uint64 0,)", 0, true},
		{"", 0, false},
		{"Error: GDBus.Error:org.freedesktop.DBus.Error.ServiceUnknown", 0, false},
		{"(int32 -1,)", 0, false},
		{"(uint64 12a,)", 0, false},
	}
	for _, c := range casos {
		if n, ok := lerIdleGdbus(c.saida); n != c.n || ok != c.ok {
			t.Errorf("lerIdleGdbus(%q) = %d, %v; queria %d, %v", c.saida, n, ok, c.n, c.ok)
		}
	}
}

func TestJanelaDaSessao(t *testing.T) {
	ancestrais := []int{4321, 4300, 1200, 1}
	if !janelaDaSessao(1200, ancestrais) {
		t.Fatal("a janela do emulador de terminal que corre a sessão está à frente")
	}
	if janelaDaSessao(999, ancestrais) {
		t.Fatal("a janela de outro programa não é a da sessão")
	}
	if janelaDaSessao(0, nil) {
		t.Fatal("sem ancestrais, nenhuma janela é a da sessão")
	}
}
