package servico

import (
	"encoding/json"
	"testing"

	"github.com/GeorgeMyller/towerman/internal/hook"
)

func TestStopAusenteAvisaQueTerminou(t *testing.T) {
	a := novoAmbiente(t, nil)
	stop := func(id, em string, ausente bool) {
		ev, _ := json.Marshal(map[string]any{"session_id": id, "hook_event_name": "Stop", "cwd": "/x/projeto"})
		resp, err := a.srv.proc.Processar(hook.Envelope{Agente: "claude", RecebidoEm: em, Evento: ev, Ausente: ausente})
		if err != nil {
			t.Fatal(err)
		}
		a.srv.posProcessar(resp)
	}
	stop("presente", "2026-10-28T10:00:00Z", false)
	stop("longe", "2026-10-28T10:00:01Z", true)
	fins := a.srv.tirarFins()
	if len(fins) != 1 || fins[0].SessionID != "longe" || fins[0].Titulo != "projeto" {
		t.Fatalf("fins = %+v", fins)
	}
	if len(a.srv.tirarFins()) != 0 {
		t.Fatal("tirarFins devia esvaziar a fila")
	}
}
