package servico

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/GeorgeMyller/towerman/internal/hook"
	"github.com/GeorgeMyller/towerman/internal/relay"
)

func TestPushNoFimDoPassoDeSessaoLancada(t *testing.T) {
	a := novoAmbiente(t, nil)
	priv, pub, _ := relay.GerarParX25519()
	codigo, _ := a.srv.aparelhos.novoCodigo(time.Now())
	_, par := a.rede("POST", "/v1/pair", "", map[string]string{"code": codigo, "device": "iPhone", "device_pk": pub, "push_token": "tk-1"})
	devID := fmt.Sprint(par["device"])

	// Sessão local: o Stop não gera aviso.
	a.enviar(map[string]any{"session_id": "local", "hook_event_name": "UserPromptSubmit", "prompt": "x"})
	a.esperar()
	if r := a.srv.proc.mustProcessar(t, "local", "Stop", "feito"); r.Fim != nil {
		t.Fatalf("sessão local, com a pessoa presente, não devia gerar push: %+v", r.Fim)
	}

	a.enviar(map[string]any{"session_id": "remota", "hook_event_name": "UserPromptSubmit", "prompt": "x"})
	a.esperar()
	a.srv.quadro.MarcarLancamento("remota", "claude", devID)
	resposta := strings.Repeat("é", 200)
	r := a.srv.proc.mustProcessar(t, "remota", "Stop", resposta)
	if r.Fim == nil || r.Fim.SessionID != "remota" || r.Fim.Resposta != resposta {
		t.Fatalf("sessão lançada devia gerar o aviso «terminou» com a resposta: %+v", r)
	}

	// Sem push, o aviso chega pelo estado: doneAt só na sessão que terminou com aviso.
	a.srv.anotarFim(*r.Fim)
	fins := map[string]any{}
	for _, ag := range a.srv.estadoV1()["agents"].([]map[string]any) {
		fins[ag["id"].(string)] = ag["doneAt"]
	}
	if d, _ := fins["remota"].(string); d == "" || fins["local"] != nil {
		t.Fatalf("doneAt devia estar só na sessão remota: %v", fins)
	}

	ps := a.srv.pushesFim(*r.Fim, "mac")
	if len(ps) != 1 || ps[0].Token != "tk-1" || ps[0].Categoria != "session-done" || ps[0].Enc == "" ||
		ps[0].Device != devID || ps[0].Server != a.srv.aparelhos.servidor {
		t.Fatalf("push mal montado: %+v", ps)
	}
	if strings.Contains(ps[0].Corpo, "é") {
		t.Fatal("o texto da resposta não pode ir em claro")
	}
	chave, _ := relay.DerivarChaveSimetrica(priv, a.srv.aparelhos.chavePub, a.srv.aparelhos.servidor)
	claro, err := relay.Abrir(chave, ps[0].Enc, []byte(devID))
	if err != nil {
		t.Fatalf("o enc devia abrir com a chave do aparelho: %v", err)
	}
	var c struct{ Kind, Title, Body string }
	json.Unmarshal(claro, &c)
	if c.Kind != "done" || !strings.HasSuffix(c.Title, " · mac") || len([]rune(c.Body)) != 140 {
		t.Fatalf("o enc devia ter o título e 140 letras: %q (%d)", c.Title, len([]rune(c.Body)))
	}
}

// mustProcessar passa um evento pelo processador e devolve a resposta.
func (p *Processador) mustProcessar(t *testing.T, sessao, evento, ultima string) Resposta {
	t.Helper()
	ev, _ := json.Marshal(map[string]any{"session_id": sessao, "hook_event_name": evento, "last_assistant_message": ultima})
	r, err := p.Processar(envelopeDe(ev))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func envelopeDe(ev []byte) hook.Envelope {
	return hook.Envelope{Agente: "claude", RecebidoEm: time.Now().UTC().Format(time.RFC3339Nano), Evento: ev}
}
