package servico

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/GeorgeMyller/towerman/internal/painel"
)

// rede faz um pedido à porta de rede (o que o iPhone usa), sem abrir socket.
func (a *ambiente) rede(metodo, caminho, token string, corpo any) (int, map[string]any) {
	a.t.Helper()
	var b bytes.Buffer
	if corpo != nil {
		json.NewEncoder(&b).Encode(corpo)
	}
	req := httptest.NewRequest(metodo, caminho, &b)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	a.srv.rotasRede().ServeHTTP(rec, req)
	var r map[string]any
	json.Unmarshal(rec.Body.Bytes(), &r)
	return rec.Code, r
}

func TestPareamentoPorCodigoDeUsoUnico(t *testing.T) {
	a := novoAmbiente(t, nil)
	codigo, _ := a.srv.aparelhos.novoCodigo(time.Now())

	if c, _ := a.rede("GET", "/v1/state", "", nil); c != http.StatusUnauthorized {
		t.Fatalf("sem token o estado devia dar 401, deu %d", c)
	}
	if c, _ := a.rede("POST", "/v1/pair", "", map[string]string{"code": "ERRADO00", "device": "x"}); c != http.StatusForbidden {
		t.Fatalf("código errado devia dar 403, deu %d", c)
	}
	c, r := a.rede("POST", "/v1/pair", "", map[string]string{"code": strings.ToLower(codigo), "device": "iPhone do George"})
	token, _ := r["token"].(string)
	if c != 200 || token == "" {
		t.Fatalf("pareamento devia dar um token: %d %v", c, r)
	}
	if c, _ := a.rede("POST", "/v1/pair", "", map[string]string{"code": codigo, "device": "outro"}); c != http.StatusForbidden {
		t.Fatalf("o código só serve uma vez, deu %d", c)
	}
	c, estado := a.rede("GET", "/v1/state", token, nil)
	if c != 200 || estado["agents"] == nil {
		t.Fatalf("com token o estado devia vir: %d %v", c, estado)
	}
	// Os endereços atuais vão no estado: o app aprende-os (ex.: Tailscale ligado depois).
	if srv, _ := estado["server"].(map[string]any); srv == nil || srv["hosts"] == nil {
		t.Fatalf("o estado devia trazer os endereços do computador: %v", estado["server"])
	}
	lista := a.srv.aparelhos.copia()
	if len(lista) != 1 || lista[0].Nome != "iPhone do George" || lista[0].Hash != "" {
		t.Fatalf("aparelho mal registado (e o hash nunca sai): %+v", lista)
	}
	if !a.srv.aparelhos.revogar(lista[0].ID) {
		t.Fatal("revogar devia funcionar")
	}
	if c, _ := a.rede("GET", "/v1/state", token, nil); c != http.StatusUnauthorized {
		t.Fatalf("revogado devia dar 401, deu %d", c)
	}
}

func TestCodigoExpira(t *testing.T) {
	a := novoAmbiente(t, nil)
	codigo, _ := a.srv.aparelhos.novoCodigo(time.Now().Add(-3 * time.Minute))
	if c, _ := a.rede("POST", "/v1/pair", "", map[string]string{"code": codigo}); c != http.StatusForbidden {
		t.Fatalf("código expirado devia dar 403, deu %d", c)
	}
}

func TestRedeRespondePermissaoComAparelhoPareado(t *testing.T) {
	a := novoAmbiente(t, nil)
	codigo, _ := a.srv.aparelhos.novoCodigo(time.Now())
	_, r := a.rede("POST", "/v1/pair", "", map[string]string{"code": codigo, "device": "iPhone"})
	token := r["token"].(string)

	// Sem painel aberto nem «responder permissões aqui»: o aparelho pareado basta para esperar.
	resultado := make(chan map[string]any, 1)
	go func() {
		resultado <- a.responder(map[string]any{"session_id": "r", "hook_event_name": "PermissionRequest", "tool_name": "Bash",
			"tool_input": map[string]any{"command": "make test"}})
	}()
	var agente, pedido string
	for prazo := time.Now().Add(3 * time.Second); pedido == "" && time.Now().Before(prazo); time.Sleep(20 * time.Millisecond) {
		_, st := a.rede("GET", "/v1/state", token, nil)
		for _, x := range st["agents"].([]any) {
			ag := x.(map[string]any)
			if w, ok := ag["waiting"].(map[string]any); ok {
				agente, pedido = ag["id"].(string), w["id"].(string)
				if w["detail"] != "make test" || w["kind"] != "permission" {
					t.Fatalf("pedido mal traduzido: %v", w)
				}
			}
		}
	}
	if pedido == "" {
		t.Fatal("o pedido não chegou ao estado da rede")
	}
	if c, _ := a.rede("POST", fmt.Sprintf("/v1/agents/%s/answer", agente), token, map[string]string{"promptId": pedido, "key": "allow"}); c != http.StatusNoContent {
		t.Fatalf("responder devia dar 204, deu %d", c)
	}
	saida := <-resultado
	if saida["hookSpecificOutput"].(map[string]any)["decision"].(map[string]any)["behavior"] != "allow" {
		t.Fatalf("decisão não chegou ao hook: %v", saida)
	}
	if c, _ := a.rede("POST", fmt.Sprintf("/v1/agents/%s/answer", agente), token, map[string]string{"promptId": pedido, "key": "allow"}); c != http.StatusConflict {
		t.Fatalf("resposta repetida devia dar 409, deu %d", c)
	}
}

func TestQRNoTerminal(t *testing.T) {
	desenho, err := QR("towerman://pair?v=1&codigo=ABCD2345")
	if err != nil || !strings.Contains(desenho, "█") {
		t.Fatalf("QR vazio: %v", err)
	}
}

// Pelo túnel (aberto à internet), um código válido não chega para parear.
func TestPareamentoRecusadoPeloTunel(t *testing.T) {
	a := novoAmbiente(t, nil)
	codigo, _ := a.srv.aparelhos.novoCodigo(time.Now())
	b, _ := json.Marshal(map[string]string{"code": codigo, "device": "intruso"})
	req := httptest.NewRequest("POST", "/v1/pair", bytes.NewReader(b))
	req.Header.Set("Cf-Connecting-Ip", "203.0.113.7")
	rec := httptest.NewRecorder()
	a.srv.rotasRede().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("pareamento pelo túnel devia dar 403, deu %d", rec.Code)
	}
	if c, _ := a.rede("POST", "/v1/pair", "", map[string]string{"code": codigo, "device": "iPhone"}); c != 200 {
		t.Fatalf("o código continua válido na rede local, deu %d", c)
	}
}

func TestEnderecosIncluemOTunel(t *testing.T) {
	urlTunel.Store("https://abc-def.trycloudflare.com")
	esquecerEnderecos()
	defer func() { urlTunel.Store(""); esquecerEnderecos() }()
	l := enderecos()
	if len(l) == 0 || l[len(l)-1] != "https://abc-def.trycloudflare.com" {
		t.Fatalf("o túnel devia ir no fim da lista: %v", l)
	}
}

// Long-poll do túnel: com a versão atual o pedido fica à espera; com outra responde logo.
func TestEstadoLongPoll(t *testing.T) {
	a := novoAmbiente(t, nil)
	codigo, _ := a.srv.aparelhos.novoCodigo(time.Now())
	_, r := a.rede("POST", "/v1/pair", "", map[string]string{"code": codigo, "device": "iPhone"})
	token, _ := r["token"].(string)
	pedir := func(espera string, prazo time.Duration) (*httptest.ResponseRecorder, time.Duration) {
		ctx, cancelar := context.WithTimeout(context.Background(), prazo)
		defer cancelar()
		req := httptest.NewRequest("GET", "/v1/state?espera="+espera, nil).WithContext(ctx)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		inicio := time.Now()
		a.srv.rotasRede().ServeHTTP(rec, req)
		return rec, time.Since(inicio)
	}
	rec, dur := pedir("outra", 5*time.Second)
	v := rec.Header().Get("X-Towerman-Versao")
	if rec.Code != 200 || v == "" || dur > time.Second {
		t.Fatalf("versão diferente devia responder logo com a versão: %d %q %v", rec.Code, v, dur)
	}
	if rec2, _ := pedir("outra", 5*time.Second); rec2.Header().Get("X-Towerman-Versao") != v {
		t.Fatal("a versão não pode mudar sem o estado mudar (senão o long-poll gira em vazio)")
	}
	if _, dur := pedir(v, 400*time.Millisecond); dur < 350*time.Millisecond {
		t.Fatalf("com a versão atual devia esperar, voltou em %v", dur)
	}
}

// Os remotos recebem logo o urgente; o acessório chega adiado, uma vez, com o último estado.
func TestDifusorRemotoEmDoisNiveis(t *testing.T) {
	d := painel.NovoDifusor()
	d.Agregacao, d.Secundaria = 10*time.Millisecond, 300*time.Millisecond
	var urgente atomic.Value
	urgente.Store("a")
	local, sairLocal := d.Subscrever()
	defer sairLocal()
	remoto, sairRemoto := d.SubscreverRemoto(func() string { return urgente.Load().(string) })
	defer sairRemoto()
	chegou := func(c <-chan struct{}, prazo time.Duration) bool {
		select {
		case <-c:
			return true
		case <-time.After(prazo):
			return false
		}
	}
	d.Mudou()
	if !chegou(remoto, 200*time.Millisecond) || !chegou(local, 200*time.Millisecond) {
		t.Fatal("a primeira mudança devia chegar logo a todos")
	}
	// Rajada de acessório: o painel local vê cada uma, o remoto só uma no fim.
	for i := 0; i < 3; i++ {
		d.Mudou()
		if !chegou(local, 200*time.Millisecond) {
			t.Fatal("o painel local não pode ser adiado")
		}
	}
	if chegou(remoto, 150*time.Millisecond) {
		t.Fatal("o acessório não devia acordar logo o remoto")
	}
	if !chegou(remoto, 400*time.Millisecond) {
		t.Fatal("o acessório tem de chegar ao remoto no fim do intervalo")
	}
	if chegou(remoto, 400*time.Millisecond) {
		t.Fatal("a rajada devia dar um único aviso adiado")
	}
	// Um acessório adiado e logo um urgente: o urgente sai já e leva o adiado consigo.
	d.Mudou()
	time.Sleep(50 * time.Millisecond)
	urgente.Store("b")
	d.Mudou()
	if !chegou(remoto, 100*time.Millisecond) {
		t.Fatal("a mudança urgente devia chegar logo")
	}
	if chegou(remoto, 400*time.Millisecond) {
		t.Fatal("o urgente já levou o acessório; não devia haver outro aviso")
	}
}

func TestAssinaturaUrgente(t *testing.T) {
	estado := func(status string, espera any, atividade string) map[string]any {
		return map[string]any{"server": map[string]any{"hosts": []string{"h"}}, "agents": []map[string]any{
			{"id": "s1", "status": status, "waiting": espera, "activity": atividade, "tool": "claude"}}}
	}
	base := assinaturaUrgente(estado("running", nil, "Edit"))
	if assinaturaUrgente(estado("running", nil, "Bash")) != base {
		t.Fatal("a atividade não é urgente")
	}
	if assinaturaUrgente(estado("idle", nil, "Edit")) == base {
		t.Fatal("o estado é urgente")
	}
	if assinaturaUrgente(estado("running", map[string]any{"id": "7"}, "Edit")) == base {
		t.Fatal("um pedido em espera é urgente")
	}
	comFim := estado("running", nil, "Edit")
	comFim["agents"].([]map[string]any)[0]["doneAt"] = "2026-11-02T18:00:00Z"
	if assinaturaUrgente(comFim) == base {
		t.Fatal("um aviso «terminou» novo é urgente")
	}
	if assinaturaUrgente(map[string]any{"server": map[string]any{"hosts": []string{"h"}}, "agents": []map[string]any{}}) == base {
		t.Fatal("um agente que sai é urgente")
	}
}
