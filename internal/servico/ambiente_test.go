package servico

import (
	"reflect"
	"testing"
)

func TestLerAmbienteGrafico(t *testing.T) {
	saida := "DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1000/bus\nDISPLAY=:0\nHOME=/home/george\nXAUTHORITY=/home/george/.Xauthority\nWAYLAND_DISPLAY=\n"
	got := lerAmbienteGrafico(saida)
	want := map[string]string{"DISPLAY": ":0", "XAUTHORITY": "/home/george/.Xauthority"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got := lerAmbienteGrafico(""); len(got) != 0 {
		t.Fatalf("saída vazia devia dar mapa vazio: %v", got)
	}
}

func TestPadroesJanela(t *testing.T) {
	casos := map[string][]string{
		"/home/george/Documentos/2026/centralizacao_user_client": {"^centralizacao_user_client - ", "."},
		"/home/george/app.v2": {`^app\.v2 - `, "."},
		"":                    {"."},
		"/":                   {"."},
	}
	for cwd, want := range casos {
		if got := padroesJanela(cwd); !reflect.DeepEqual(got, want) {
			t.Errorf("padroesJanela(%q) = %q, want %q", cwd, got, want)
		}
	}
}
