package servico

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/GeorgeMyller/towerman/internal/quadro"
)

// transcricaoRetomada escreve uma transcrição como a do Claude Code depois de --fork-session: as
// linhas copiadas guardam a sessão antiga em session_id; as novas, a própria.
func transcricaoRetomada(t *testing.T, nova, antiga string) string {
	t.Helper()
	var b strings.Builder
	for _, l := range []map[string]any{
		{"type": "ai-title", "sessionId": nova},
		{"type": "user", "sessionId": nova, "session_id": antiga},
		{"type": "assistant", "sessionId": nova, "session_id": antiga},
		{"type": "user", "sessionId": nova, "session_id": nova},
	} {
		j, _ := json.Marshal(l)
		b.Write(append(j, '\n'))
	}
	p := filepath.Join(t.TempDir(), nova+".jsonl")
	if err := os.WriteFile(p, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRetomadaTerminaACopiaAntiga(t *testing.T) {
	a := novoAmbiente(t, nil)
	p := a.srv.proc
	ev := func(m map[string]any) {
		t.Helper()
		j, _ := json.Marshal(m)
		if _, err := p.Processar(envelopeDe(j)); err != nil {
			t.Fatal(err)
		}
	}
	ev(map[string]any{"session_id": "antiga", "hook_event_name": "Notification", "notification_type": "permission_prompt"})
	if _, err := a.srv.quadro.CriarMensagem("antiga", "Digitando no Watch", "stop", time.Now()); err != nil {
		t.Fatal(err)
	}

	ev(map[string]any{"session_id": "nova", "hook_event_name": "SessionStart",
		"transcript_path": transcricaoRetomada(t, "nova", "antiga")})

	if s := a.sessao("antiga"); s.Estado != quadro.Terminada {
		t.Fatalf("a cópia antiga devia terminar, está %q", s.Estado)
	}
	ms, err := a.srv.quadro.MensagensPendentes("nova")
	if err != nil || len(ms) != 1 || ms[0].Texto != "Digitando no Watch" {
		t.Fatalf("a mensagem da cópia antiga devia passar para a nova: %+v %v", ms, err)
	}
	// A mensagem entra no próximo Stop da conversa nova.
	if r := p.mustProcessar(t, "nova", "Stop", ""); !strings.Contains(r.Bloquear, "Digitando no Watch") {
		t.Fatalf("Stop devia entregar a mensagem, bloquear = %q", r.Bloquear)
	}
}

func TestSemRetomadaNadaMuda(t *testing.T) {
	a := novoAmbiente(t, nil)
	p := a.srv.proc
	p.mustProcessar(t, "outra", "Stop", "")
	tr := transcricaoRetomada(t, "propria", "propria")
	j, _ := json.Marshal(map[string]any{"session_id": "propria", "hook_event_name": "SessionStart", "transcript_path": tr})
	if _, err := p.Processar(envelopeDe(j)); err != nil {
		t.Fatal(err)
	}
	if s := a.sessao("outra"); s.Estado == quadro.Terminada {
		t.Fatal("uma sessão sem relação não devia terminar")
	}
	if origem, decidido := origemRetomada(tr, "propria"); origem != "" || !decidido {
		t.Fatalf("origem = %q decidido = %v", origem, decidido)
	}
	if origem, decidido := origemRetomada(filepath.Join(t.TempDir(), "falta.jsonl"), "x"); origem != "" || decidido {
		t.Fatalf("sem transcrição: origem = %q decidido = %v", origem, decidido)
	}
}
