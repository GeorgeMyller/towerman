package servico

import (
	"reflect"
	"testing"
)

func TestLerAmbienteGrafico(t *testing.T) {
	saida := "DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1000/bus\nDISPLAY=:0\nHOME=/home/user\nXAUTHORITY=/home/user/.Xauthority\nWAYLAND_DISPLAY=\n"
	got := lerAmbienteGrafico(saida)
	want := map[string]string{"DISPLAY": ":0", "XAUTHORITY": "/home/user/.Xauthority"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got := lerAmbienteGrafico(""); len(got) != 0 {
		t.Fatalf("saída vazia devia dar mapa vazio: %v", got)
	}
}

func TestPadroesJanela(t *testing.T) {
	casos := map[string][]string{
		"/home/user/projects/sample-client": {"^sample-client - ", "."},
		"/home/user/app.v2":                 {`^app\.v2 - `, "."},
		"":                                  {"."},
		"/":                                 {"."},
	}
	for cwd, want := range casos {
		if got := padroesJanela(cwd); !reflect.DeepEqual(got, want) {
			t.Errorf("padroesJanela(%q) = %q, want %q", cwd, got, want)
		}
	}
}
