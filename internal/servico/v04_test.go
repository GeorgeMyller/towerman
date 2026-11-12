package servico

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/GeorgeMyller/towerman/internal/inventario"
	"github.com/GeorgeMyller/towerman/internal/visao"
)

func decisao(saida map[string]any) (string, string) {
	h, _ := saida["hookSpecificOutput"].(map[string]any)
	d, _ := h["permissionDecision"].(string)
	m, _ := h["permissionDecisionReason"].(string)
	return d, m
}

func (a *ambiente) obter(caminho string, v any) {
	a.t.Helper()
	req, _ := http.NewRequest("GET", a.url+caminho, nil)
	req.Header.Set("Authorization", "Bearer "+a.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		a.t.Fatalf("%s: %d", caminho, resp.StatusCode)
	}
	json.NewDecoder(resp.Body).Decode(v)
}

func TestRegra2MesmaBranch(t *testing.T) {
	_, wt := novoRepo(t)
	a := novoAmbiente(t, nil)
	a.enviar(map[string]any{"session_id": "dona", "hook_event_name": "PostToolUse", "cwd": wt, "tool_name": "Edit",
		"tool_input": map[string]any{"file_path": wt + "/a.txt"}})
	tentar := func() (string, string) {
		return decisao(a.responder(map[string]any{"session_id": "intrusa", "hook_event_name": "PreToolUse", "cwd": wt, "tool_name": "Edit",
			"tool_input": map[string]any{"file_path": wt + "/a.txt"}}))
	}
	d, m := tentar()
	if d != "ask" || !strings.Contains(m, "«dona»") || !strings.Contains(m, "feat/x") {
		t.Fatalf("modo ask esperado: %q %q", d, m)
	}
	if d, _ := tentar(); d != "ask" {
		t.Fatalf("cada tentativa deve voltar a ser travada, obtive %q", d)
	}
	if d, _ := decisao(a.responder(map[string]any{"session_id": "intrusa", "hook_event_name": "PreToolUse", "cwd": wt, "tool_name": "Edit",
		"tool_input": map[string]any{"file_path": wt + "/outro.txt"}})); d != "" {
		t.Fatalf("outro ficheiro não devia ser travado: %q", d)
	}

	if c, _ := a.postJSON("/v1/painel", map[string]string{"regra2": "deny"}); c != 200 {
		t.Fatalf("mudar para deny: %d", c)
	}
	if d, _ := tentar(); d != "deny" {
		t.Fatalf("modo deny esperado, obtive %q", d)
	}
	if c, _ := a.postJSON("/v1/painel", map[string]string{"regra2": "talvez"}); c != http.StatusBadRequest {
		t.Fatalf("modo inválido devia dar 400, deu %d", c)
	}
	a.postJSON("/v1/painel", map[string]string{"regra2": "off"})
	if d, _ := tentar(); d != "" {
		t.Fatalf("modo off não devia decidir, obtive %q", d)
	}
	a.postJSON("/v1/painel", map[string]string{"regra2": "ask"})
	a.enviar(map[string]any{"session_id": "dona", "hook_event_name": "SessionEnd"})
	a.esperar()
	if d, _ := tentar(); d != "" {
		t.Fatalf("com a outra sessão terminada não há bloqueio, obtive %q", d)
	}

	var avisos []map[string]any
	a.obter("/v1/avisos", &avisos)
	if len(avisos) == 0 || avisos[0]["regra"].(float64) != 2 || avisos[0]["nome_para"] != "" && avisos[0]["caminho"] != "a.txt" {
		t.Fatalf("avisos inesperados: %v", avisos)
	}
	id := int(avisos[0]["id"].(float64))
	if c, _ := a.postJSON(fmt.Sprintf("/v1/avisos/%d/falso-alarme", id), nil); c != 200 {
		t.Fatalf("marcar falso alarme: %d", c)
	}
	if c, _ := a.postJSON("/v1/avisos/999999/falso-alarme", nil); c != http.StatusNotFound {
		t.Fatalf("aviso desconhecido devia dar 404, deu %d", c)
	}
	a.obter("/v1/avisos", &avisos)
	if avisos[0]["falso_alarme"] != true {
		t.Fatalf("falso alarme não ficou marcado: %v", avisos[0])
	}
}

func TestColisoesNoPainelEWorktrees(t *testing.T) {
	repo, wt := novoRepo(t)
	a := novoAmbiente(t, nil)
	for _, s := range []struct{ id, cwd string }{{"p", repo}, {"w", wt}} {
		a.enviar(map[string]any{"session_id": s.id, "hook_event_name": "PostToolUse", "cwd": s.cwd, "tool_name": "Edit",
			"tool_input": map[string]any{"file_path": s.cwd + "/a.txt"}})
	}
	a.esperar()
	var repos []visao.Repo
	a.obter("/v1/sessoes", &repos)
	n := 0
	for _, r := range repos {
		for _, w := range r.Worktrees {
			for _, s := range w.Sessoes {
				if len(s.Colisoes) != 1 || s.Colisoes[0].Caminho != "a.txt" || s.Colisoes[0].MesmaBranch {
					t.Fatalf("colisão inesperada em %s: %+v", s.ID, s.Colisoes)
				}
				n++
			}
		}
	}
	if n != 2 {
		t.Fatalf("esperava 2 sessões com colisão, vi %d", n)
	}

	var rel inventario.Relatorio
	a.obter("/v1/worktrees", &rel)
	if len(rel.Linhas) != 2 {
		t.Fatalf("esperava 2 worktrees, obtive %+v", rel.Linhas)
	}
	for _, l := range rel.Linhas {
		if l.Sessao == "" {
			t.Fatalf("cada worktree tem uma sessão viva: %+v", l)
		}
	}
}
