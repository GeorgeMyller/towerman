package servico

import (
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

// parearComNome devolve o token de um aparelho novo; chavePub pode ser vazia.
func (a *ambiente) parearComNome(nome, chavePub string) string {
	a.t.Helper()
	codigo, _ := a.srv.aparelhos.novoCodigo(time.Now())
	_, r := a.rede("POST", "/v1/pair", "", map[string]string{"code": codigo, "device": nome, "device_pk": chavePub})
	token, _ := r["token"].(string)
	if token == "" {
		a.t.Fatalf("pareamento falhou: %v", r)
	}
	return token
}

func TestRedeCatalogoELancamento(t *testing.T) {
	a, repo := ambienteLancar(t)
	token := a.parearComNome("iPhone do George", "")

	if c, _ := a.rede("GET", "/v1/launch/catalog", "", nil); c != http.StatusUnauthorized {
		t.Fatalf("catálogo sem token devia dar 401, deu %d", c)
	}
	if c, _ := a.rede("POST", "/v1/launch", "", map[string]any{"root": repo, "task": "x"}); c != http.StatusUnauthorized {
		t.Fatalf("lançar sem token devia dar 401, deu %d", c)
	}

	c, cat := a.rede("GET", "/v1/launch/catalog", token, nil)
	if c != 200 {
		t.Fatalf("catálogo: %d %v", c, cat)
	}
	agentes, _ := cat["agents"].([]any)
	if len(agentes) == 0 || agentes[0].(map[string]any)["id"] != "claude" {
		t.Fatalf("o claude falso devia aparecer nos agentes: %v", cat["agents"])
	}
	repos, _ := cat["repos"].([]any)
	if len(repos) != 1 {
		t.Fatalf("devia haver um repo no catálogo: %v", cat["repos"])
	}
	r0 := repos[0].(map[string]any)
	papeis, _ := r0["roles"].([]any)
	if !mesmaPasta(r0["root"], repo) || len(papeis) != 1 || papeis[0].(map[string]any)["name"] != "revisor" {
		t.Fatalf("repo mal descrito: %v", r0)
	}
	if _, temPrompt := papeis[0].(map[string]any)["prompt"]; temPrompt {
		t.Fatal("o catálogo não leva o prompt do papel")
	}

	c, st := a.rede("GET", "/v1/state", token, nil)
	caps, _ := st["capabilities"].([]any)
	if c != 200 || len(caps) == 0 || caps[0] != "launch" {
		t.Fatalf("o estado devia anunciar a capacidade launch: %v", st["capabilities"])
	}

	c, r := a.rede("POST", "/v1/launch", token, map[string]any{"request_id": "q1", "origin": "phone", "root": repo, "role": "revisor", "task": "Rever o relay"})
	id, _ := r["session_id"].(string)
	if c != 200 || id == "" || r["branch"] != "feat/rever-o-relay" {
		t.Fatalf("lançar: %d %v", c, r)
	}
	a.esperar()
	if s := a.sessao(id); s.LancadaPor == "" {
		t.Fatalf("a sessão devia guardar o aparelho: %+v", s)
	}
	_, st = a.rede("GET", "/v1/state", token, nil)
	achou := false
	for _, x := range st["agents"].([]any) {
		if ag := x.(map[string]any); ag["id"] == id {
			achou = ag["launchedBy"] == "iPhone do George"
		}
	}
	if !achou {
		t.Fatalf("o agente lançado devia trazer launchedBy: %v", st["agents"])
	}

	c, e := a.rede("POST", "/v1/launch", token, map[string]any{"request_id": "q2", "origin": "watch", "root": repo, "task": "x", "mode": "plan"})
	if c != http.StatusBadRequest || e["error"] == "" {
		t.Fatalf("relógio em plan devia dar 400 com error: %d %v", c, e)
	}

	if c, _ := a.rede("POST", "/v1/agents/"+id+"/terminate", token, nil); c != 200 {
		t.Fatalf("terminar: %d", c)
	}
	if c, _ := a.rede("POST", "/v1/agents/"+id+"/terminate", token, nil); c != http.StatusConflict {
		t.Fatalf("terminar de novo devia dar 409, deu %d", c)
	}
}

// mesmaPasta compara caminhos resolvendo ligações (no macOS, /var é /private/var).
func mesmaPasta(a any, b string) bool {
	x, _ := filepath.EvalSymlinks(fmt.Sprint(a))
	y, _ := filepath.EvalSymlinks(b)
	return x != "" && x == y
}
