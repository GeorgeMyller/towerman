package servico

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/GeorgeMyller/towerman/internal/relay"
)

// envelopeRelay sela um pedido como o iPhone faz e devolve a resposta aberta.
func (a *ambiente) envelopeRelay(claro any) (int, map[string]any) {
	a.t.Helper()
	priv, pub, _ := relay.GerarParX25519()
	codigo, _ := a.srv.aparelhos.novoCodigo(time.Now())
	_, par := a.rede("POST", "/v1/pair", "", map[string]string{"code": codigo, "device": "iPhone", "device_pk": pub})
	ap, ok := a.srv.aparelhos.obterPorID(fmt.Sprint(par["device"]))
	if !ok {
		a.t.Fatalf("pareamento falhou: %v", par)
	}
	chave, err := relay.DerivarChaveSimetrica(priv, a.srv.aparelhos.chavePub, a.srv.aparelhos.servidor)
	if err != nil {
		a.t.Fatal(err)
	}
	b, _ := json.Marshal(claro)
	selado, _ := relay.Selar(chave, b, []byte(ap.ID))
	resp := a.srv.responderAcaoRelay(relay.Mensagem{Tipo: "action", ReqID: "1", Device: ap.ID, Payload: selado})
	if resp.Tipo != "action_reply" || resp.ReqID != "1" {
		a.t.Fatalf("resposta do relay inesperada: %+v", resp)
	}
	var r map[string]any
	if resp.Payload != "" {
		aberto, err := relay.Abrir(chave, resp.Payload, []byte(ap.ID))
		if err != nil {
			a.t.Fatalf("a resposta devia vir selada: %v", err)
		}
		json.Unmarshal(aberto, &r)
	}
	return resp.Status, r
}

func TestRelayCatalogoELancamento(t *testing.T) {
	a, repo := ambienteLancar(t)

	c, cat := a.envelopeRelay(map[string]any{"op": "catalog"})
	if c != 200 || cat["repos"] == nil {
		t.Fatalf("catálogo pelo relay: %d %v", c, cat)
	}
	c, r := a.envelopeRelay(map[string]any{"op": "launch", "launch": map[string]any{
		"request_id": "rl1", "origin": "phone", "root": repo, "task": "Pelo relay"}})
	if c != 200 || r["session_id"] == nil {
		t.Fatalf("lançar pelo relay: %d %v", c, r)
	}
	if c, _ := a.envelopeRelay(map[string]any{"op": "apagar-tudo"}); c != http.StatusBadRequest {
		t.Fatalf("op desconhecido devia dar 400, deu %d", c)
	}
	// Sem op, o envelope continua a exigir um agente válido.
	if c, _ := a.envelopeRelay(map[string]any{"action": "input", "text": "x"}); c != http.StatusBadRequest {
		t.Fatalf("ação sem agente devia dar 400, deu %d", c)
	}
}
