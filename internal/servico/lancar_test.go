package servico

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// ambienteLancar prepara um claude falso, um repo conhecido e o papel «revisor».
func ambienteLancar(t *testing.T) (*ambiente, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("claude falso em sh")
	}
	falso := t.TempDir()
	os.WriteFile(filepath.Join(falso, "claude"), []byte(claudeFalso), 0o755)
	t.Setenv("PATH", falso+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FALSO_DIR", falso)
	repo, _ := novoRepo(t)
	claude := t.TempDir()
	os.MkdirAll(filepath.Join(claude, "agents"), 0o755)
	os.WriteFile(filepath.Join(claude, "agents", "revisor.md"), []byte("---\nname: revisor\ndescription: Revê código\ntools: Read\n---\nÉs um revisor.\n"), 0o644)
	a := novoAmbiente(t, func(op *Opcoes) { op.ClaudeDir = claude })
	a.enviar(map[string]any{"session_id": "x", "hook_event_name": "SessionStart", "cwd": repo})
	a.esperar()
	t.Cleanup(func() {
		s := a.srv
		s.headless.Range(func(k, _ any) bool { s.terminarHeadless(k.(string)); return true })
	})
	return a, repo
}

func TestLancarSessaoValida(t *testing.T) {
	a, repo := ambienteLancar(t)
	ctx := context.Background()
	casos := []struct {
		nome   string
		p      pedidoLancamento
		codigo int
	}{
		{"repo desconhecido", pedidoLancamento{RequestID: "r1", Origin: "phone", Root: "/não/existe", Task: "x"}, http.StatusBadRequest},
		{"papel desconhecido", pedidoLancamento{RequestID: "r2", Origin: "phone", Root: repo, Role: "nenhum", Task: "x"}, http.StatusBadRequest},
		{"tarefa vazia", pedidoLancamento{RequestID: "r3", Origin: "phone", Root: repo, Task: "  "}, http.StatusBadRequest},
		{"relógio fora do modo default", pedidoLancamento{RequestID: "r4", Origin: "watch", Root: repo, Task: "x", Mode: "acceptEdits"}, http.StatusBadRequest},
		{"agente desconhecido", pedidoLancamento{RequestID: "r5", Origin: "phone", Agent: "outro", Root: repo, Task: "x"}, http.StatusBadRequest},
		{"origem inválida", pedidoLancamento{RequestID: "r6", Origin: "fax", Root: repo, Task: "x"}, http.StatusBadRequest},
	}
	for _, c := range casos {
		if r := a.srv.lancarSessao(ctx, c.p); r.Codigo != c.codigo || r.Erro == "" {
			t.Errorf("%s: esperava %d com erro, veio %+v", c.nome, c.codigo, r)
		}
	}
	if a.srv.contarHeadless() != 0 {
		t.Fatal("nenhum pedido inválido devia arrancar sessões")
	}
}

func TestLancarSessaoRelogioUsaDefaultERegistaAparelho(t *testing.T) {
	a, repo := ambienteLancar(t)
	r := a.srv.lancarSessao(context.Background(), pedidoLancamento{RequestID: "w1", Origin: "watch", Root: repo, Role: "revisor",
		Task: "Rever o relay", lancadaPor: "ap-1"})
	if r.Codigo != http.StatusOK || r.ID == "" {
		t.Fatalf("lançamento pelo relógio: %+v", r)
	}
	a.srv.terminarHeadless(r.ID) // o claude falso só grava tudo quando o stdin fecha
	args, _ := os.ReadFile(filepath.Join(os.Getenv("FALSO_DIR"), "args"))
	if !contemLinha(string(args), "--permission-mode", "default") {
		t.Fatalf("o relógio devia lançar em default:\n%s", args)
	}
	a.esperar()
	s, ok, _ := a.srv.Quadro().ObterSessao(r.ID)
	if !ok || s.LancadaPor != "ap-1" || s.Agente != "claude" || !s.Headless {
		t.Fatalf("sessão mal registada: %+v", s)
	}
}

func TestLancarSessaoIdempotenteELimite(t *testing.T) {
	a, repo := ambienteLancar(t)
	ctx := context.Background()
	p := pedidoLancamento{RequestID: "mesmo", Origin: "phone", Root: repo, Task: "Primeira tarefa", lancadaPor: "ap-1"}
	r1 := a.srv.lancarSessao(ctx, p)
	r2 := a.srv.lancarSessao(ctx, p)
	if r1.Codigo != 200 || r1 != r2 {
		t.Fatalf("o mesmo request_id devia devolver o mesmo resultado: %+v %+v", r1, r2)
	}
	if n := a.srv.contarHeadless(); n != 1 {
		t.Fatalf("a repetição não devia arrancar outra sessão: %d", n)
	}
	// Outro aparelho com o mesmo id não herda o resultado.
	if r := a.srv.lancarSessao(ctx, pedidoLancamento{RequestID: "mesmo", Origin: "phone", Root: repo, Task: "Primeira tarefa", lancadaPor: "ap-2"}); r.Codigo != http.StatusConflict {
		t.Fatalf("a mesma branch noutro aparelho devia dar 409, deu %+v", r)
	}
	for i, tarefa := range []string{"Segunda", "Terceira"} {
		if r := a.srv.lancarSessao(ctx, pedidoLancamento{RequestID: "n" + tarefa, Origin: "phone", Root: repo, Task: tarefa}); r.Codigo != 200 {
			t.Fatalf("lançamento %d: %+v", i+2, r)
		}
	}
	r := a.srv.lancarSessao(ctx, pedidoLancamento{RequestID: "quarta", Origin: "phone", Root: repo, Task: "Quarta"})
	if r.Codigo != http.StatusConflict || r.Erro == "" {
		t.Fatalf("a quarta sessão devia dar 409: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(repo), "repo-wt", "feat-quarta")); err == nil {
		t.Fatal("acima do limite não se cria worktree")
	}
}

// contemLinha procura «chave valor» nos argumentos que o claude falso gravou, um por linha.
func contemLinha(args, chave, valor string) bool {
	return strings.Contains(args, chave+"\n"+valor+"\n")
}

const agyFalso = `#!/bin/sh
printf '%s\n' "$@" > "$FALSO_DIR/agy-args"
echo '{"event":"init","conversation_id":"3b483cf1-630a-45c7-9b9c-b5b123cad1af"}'
cat > "$FALSO_DIR/agy-stdin"
`

func TestLancarSessaoAntigravity(t *testing.T) {
	a, repo := ambienteLancar(t)
	falso := os.Getenv("FALSO_DIR")
	os.WriteFile(filepath.Join(falso, "agy"), []byte(agyFalso), 0o755)
	r := a.srv.lancarSessao(context.Background(), pedidoLancamento{RequestID: "g1", Origin: "phone", Agent: "antigravity",
		Root: repo, Role: "revisor", Task: "Rever com o agy", Mode: "acceptEdits"})
	if r.Codigo != 200 || r.ID != "3b483cf1-630a-45c7-9b9c-b5b123cad1af" {
		t.Fatalf("o id da sessão devia vir do init do agy: %+v", r)
	}
	if c, m := a.postJSON("/v1/sessoes/"+r.ID+"/mensagens", map[string]string{"texto": "e os testes"}); c != 200 || m["caminho"] != "stdin" {
		t.Fatalf("mensagem ao agy devia ir pelo stdin: %d %v", c, m)
	}
	if c, _ := a.postJSON("/v1/sessoes/"+r.ID+"/promover", nil); c != http.StatusConflict {
		t.Fatalf("só o Claude Code passa a terminal, deu %d", c)
	}
	a.srv.terminarHeadless(r.ID)
	args, _ := os.ReadFile(filepath.Join(falso, "agy-args"))
	stdin, _ := os.ReadFile(filepath.Join(falso, "agy-stdin"))
	if !contemLinha(string(args), "--mode", "accept-edits") || !strings.Contains(string(args), "-p=") {
		t.Fatalf("argumentos do agy inesperados:\n%s", args)
	}
	if !strings.Contains(string(stdin), `"event":"user"`) || !strings.Contains(string(stdin), "És um revisor.") || !strings.Contains(string(stdin), "e os testes") {
		t.Fatalf("stdin do agy inesperado: %s", stdin)
	}
	a.esperar()
	if s := a.sessao(r.ID); s.Agente != "antigravity" {
		t.Fatalf("o agente devia ficar registado: %+v", s)
	}
}

// codexFalso grava cada turno (argumentos e mensagem) e anuncia sempre o mesmo thread_id.
const codexFalso = `#!/bin/sh
printf '%s\n' "$@" >> "$FALSO_DIR/codex-args"
echo '--' >> "$FALSO_DIR/codex-args"
echo '{"type":"thread.started","thread_id":"01a183ab-215e-7c30-9846-85652e8147d6"}'
cat >> "$FALSO_DIR/codex-stdin"
echo '|' >> "$FALSO_DIR/codex-stdin"
`

func TestLancarSessaoCodexPorTurno(t *testing.T) {
	a, repo := ambienteLancar(t)
	falso := os.Getenv("FALSO_DIR")
	os.WriteFile(filepath.Join(falso, "codex"), []byte(codexFalso), 0o755)
	if r := a.srv.lancarSessao(context.Background(), pedidoLancamento{RequestID: "c0", Origin: "phone", Agent: "codex", Root: repo, Task: "x", Mode: "acceptEdits"}); r.Codigo != 400 {
		t.Fatalf("o Codex não tem acceptEdits: %+v", r)
	}
	r := a.srv.lancarSessao(context.Background(), pedidoLancamento{RequestID: "c1", Origin: "watch", Agent: "codex",
		Root: repo, Role: "revisor", Task: "Rever com o Codex"})
	id := "01a183ab-215e-7c30-9846-85652e8147d6"
	if r.Codigo != 200 || r.ID != id {
		t.Fatalf("o id da sessão devia vir do thread.started: %+v", r)
	}
	for _, texto := range []string{"segunda", "terceira"} {
		if c, m := a.postJSON("/v1/sessoes/"+id+"/mensagens", map[string]string{"texto": texto}); c != 200 || m["caminho"] != "stdin" {
			t.Fatalf("mensagem ao Codex: %d %v", c, m)
		}
	}
	// Os turnos correm um de cada vez: espera pelos três.
	var stdin []byte
	for prazo := time.Now().Add(5 * time.Second); time.Now().Before(prazo); time.Sleep(20 * time.Millisecond) {
		if stdin, _ = os.ReadFile(filepath.Join(falso, "codex-stdin")); strings.Count(string(stdin), "|") == 3 {
			break
		}
	}
	if !strings.Contains(string(stdin), "És um revisor.") || !strings.Contains(string(stdin), "segunda|") || !strings.Contains(string(stdin), "terceira|") {
		t.Fatalf("cada mensagem devia ser um turno: %q", stdin)
	}
	args, _ := os.ReadFile(filepath.Join(falso, "codex-args"))
	if !strings.Contains(string(args), "exec\n--json\n-s\nworkspace-write\n-\n") || strings.Count(string(args), "resume\n") != 2 ||
		!strings.Contains(string(args), `sandbox_mode="workspace-write"`+"\n"+id+"\n-\n") {
		t.Fatalf("argumentos do Codex inesperados:\n%s", args)
	}
	if !a.srv.terminarHeadless(id) || a.srv.obterHeadless(id) != nil {
		t.Fatal("terminar devia fechar a sessão por turno")
	}
	a.esperar()
	if s := a.sessao(id); s.Agente != "codex" {
		t.Fatalf("o agente devia ficar registado: %+v", s)
	}
}
