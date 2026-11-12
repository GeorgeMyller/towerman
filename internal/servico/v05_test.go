package servico

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/GeorgeMyller/towerman/internal/quadro"
)

func (a *ambiente) fluxos() []quadro.Fluxo {
	a.t.Helper()
	var fs []quadro.Fluxo
	a.obter("/v1/fluxos", &fs)
	return fs
}

func escreverTranscricao(t *testing.T, resposta string) string {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	os.WriteFile(p, []byte(`{"type":"user","timestamp":"t1","message":{"content":"implementa X"}}`+"\n"+
		`{"type":"assistant","timestamp":"t2","message":{"content":[{"type":"text","text":"`+resposta+`"}]}}`+"\n"), 0o600)
	return p
}

func TestFluxoPassaRespostaNoStop(t *testing.T) {
	a := novoAmbiente(t, nil)
	tr := escreverTranscricao(t, "Feito: implementei X. PRONTO")
	a.enviar(map[string]any{"session_id": "impl", "hook_event_name": "UserPromptSubmit", "prompt": "implementa X", "transcript_path": tr})
	a.enviar(map[string]any{"session_id": "rev", "hook_event_name": "UserPromptSubmit", "prompt": "revê"})
	a.esperar()

	if c, _ := a.postJSON("/v1/fluxos/arestas", map[string]any{"de": "impl", "para": "impl"}); c != http.StatusBadRequest {
		t.Fatalf("ligar uma sessão a si própria devia dar 400, deu %d", c)
	}
	if c, _ := a.postJSON("/v1/fluxos/arestas", map[string]any{"de": "impl", "para": "rev", "gatilho": "marca"}); c != http.StatusBadRequest {
		t.Fatalf("gatilho por marca sem marca devia dar 400, deu %d", c)
	}
	c, ar := a.postJSON("/v1/fluxos/arestas", map[string]any{"de": "impl", "para": "rev", "max_passos": 2, "modelo": "Revê: {resposta} [{toques}] de {de}"})
	if c != 200 {
		t.Fatalf("criar ligação: %d %v", c, ar)
	}

	saida := a.responder(map[string]any{"session_id": "impl", "hook_event_name": "Stop", "transcript_path": tr})
	if saida != nil {
		t.Fatalf("o Stop da origem não devia ser bloqueado: %v", saida)
	}
	var ms []quadro.Mensagem
	for prazo := time.Now().Add(3 * time.Second); time.Now().Before(prazo); time.Sleep(20 * time.Millisecond) {
		a.esperar()
		ms, _ = a.srv.Quadro().MensagensRecentes("rev", 5)
		if len(ms) == 1 && ms[0].Estado != "" {
			break
		}
	}
	if len(ms) != 1 || !strings.Contains(ms[0].Texto, "Revê: Feito: implementei X. PRONTO") || ms[0].Estado != quadro.MensagemPendente {
		t.Fatalf("a revisora devia ter recebido a resposta em fila: %+v", ms)
	}
	s := a.responder(map[string]any{"session_id": "rev", "hook_event_name": "Stop"})
	if s["decision"] != "block" || !strings.Contains(s["reason"].(string), "implementei X") {
		t.Fatalf("a passagem devia entrar no Stop da revisora: %v", s)
	}

	a.responder(map[string]any{"session_id": "impl", "hook_event_name": "Stop", "transcript_path": tr})
	a.responder(map[string]any{"session_id": "impl", "hook_event_name": "Stop", "transcript_path": tr})
	time.Sleep(100 * time.Millisecond)
	a.esperar()
	fs := a.fluxos()
	if len(fs) != 1 || fs[0].Passos != 2 || fs[0].Estado != quadro.FluxoLimite {
		t.Fatalf("o fluxo devia parar no limite de 2 passos: %+v", fs)
	}

	id := fs[0].ID
	if c, _ := a.postJSON(fmt.Sprintf("/v1/fluxos/%d/estado", id), map[string]any{"estado": "ativo", "max_passos": 3}); c != 200 {
		t.Fatalf("retomar com mais passos: %d", c)
	}
	a.postJSON(fmt.Sprintf("/v1/fluxos/%d/estado", id), map[string]any{"estado": "parado"})
	a.responder(map[string]any{"session_id": "impl", "hook_event_name": "Stop", "transcript_path": tr})
	time.Sleep(100 * time.Millisecond)
	a.esperar()
	if fs := a.fluxos(); fs[0].Passos != 2 {
		t.Fatalf("um fluxo parado não devia passar mais nada: %+v", fs[0])
	}
	if c, _ := a.postJSON(fmt.Sprintf("/v1/fluxos/arestas/%d/apagar", int64(ar["id"].(float64))), nil); c != 200 {
		t.Fatalf("apagar ligação: %d", c)
	}
}

func TestFluxoPorMarcaEDestinoTerminado(t *testing.T) {
	a := novoAmbiente(t, nil)
	semMarca := escreverTranscricao(t, "ainda a trabalhar")
	comMarca := escreverTranscricao(t, "tudo PRONTO")
	for _, id := range []string{"o", "d"} {
		a.enviar(map[string]any{"session_id": id, "hook_event_name": "UserPromptSubmit", "prompt": "x"})
	}
	a.esperar()
	a.postJSON("/v1/fluxos/arestas", map[string]any{"de": "o", "para": "d", "gatilho": "marca", "marca": "PRONTO"})
	a.responder(map[string]any{"session_id": "o", "hook_event_name": "Stop", "transcript_path": semMarca})
	if fs := a.fluxos(); fs[0].Passos != 0 {
		t.Fatalf("sem a marca não há passagem: %+v", fs[0])
	}
	a.enviar(map[string]any{"session_id": "d", "hook_event_name": "SessionEnd"})
	a.esperar()
	a.responder(map[string]any{"session_id": "o", "hook_event_name": "Stop", "transcript_path": comMarca})
	fs := a.fluxos()
	if fs[0].Passos != 1 || fs[0].Passagens[0].Estado != quadro.MensagemFalhada {
		t.Fatalf("passagem para destino terminado devia falhar com motivo: %+v", fs[0].Passagens)
	}
}

const claudeFalso = `#!/bin/sh
printf '%s\n' "$@" > "$FALSO_DIR/args"
pwd > "$FALSO_DIR/cwd"
cat > "$FALSO_DIR/stdin"
`

func TestNovaSessaoHeadless(t *testing.T) {
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
	os.WriteFile(filepath.Join(claude, "agents", "revisor.md"), []byte("---\nname: revisor\ntools: Read, Grep\n---\nÉs um revisor.\n"), 0o644)
	a := novoAmbiente(t, func(op *Opcoes) { op.ClaudeDir = claude })
	a.enviar(map[string]any{"session_id": "x", "hook_event_name": "SessionStart", "cwd": repo})
	a.esperar()

	var ps []map[string]any
	a.obter("/v1/papeis?raiz="+repo, &ps)
	if len(ps) != 1 || ps[0]["nome"] != "revisor" {
		t.Fatalf("papéis inesperados: %v", ps)
	}
	if c, _ := a.postJSON("/v1/sessoes/nova", map[string]any{"raiz": "/não/existe", "tarefa": "x"}); c != http.StatusBadRequest {
		t.Fatalf("repo desconhecido devia dar 400, deu %d", c)
	}
	c, r := a.postJSON("/v1/sessoes/nova", map[string]any{"raiz": repo, "papel": "revisor", "tarefa": "Rever o módulo de pagamentos"})
	if c != 200 {
		t.Fatalf("nova sessão: %d %v", c, r)
	}
	id := r["id"].(string)
	if r["branch"] != "feat/rever-o-modulo-de-pagamentos" || !strings.HasSuffix(r["worktree"].(string), filepath.Join("repo-wt", "feat-rever-o-modulo-de-pagamentos")) {
		t.Fatalf("worktree inesperada: %v", r)
	}
	if c, m := a.postJSON("/v1/sessoes/"+id+"/mensagens", map[string]string{"texto": "e os testes também"}); c != 200 || m["caminho"] != "stdin" {
		t.Fatalf("mensagem a sessão headless devia ir pelo stdin: %d %v", c, m)
	}
	if c, _ := a.postJSON("/v1/sessoes/"+id+"/terminar", nil); c != 200 {
		t.Fatalf("terminar: %d", c)
	}
	args, _ := os.ReadFile(filepath.Join(falso, "args"))
	stdin, _ := os.ReadFile(filepath.Join(falso, "stdin"))
	cwd, _ := os.ReadFile(filepath.Join(falso, "cwd"))
	for _, esperado := range []string{"--session-id\n" + id, "--append-system-prompt\nÉs um revisor.", "--allowedTools\nRead,Grep", "--permission-mode\nacceptEdits", "stream-json"} {
		if !strings.Contains(string(args), esperado) {
			t.Errorf("argumentos sem %q:\n%s", esperado, args)
		}
	}
	if !strings.Contains(string(stdin), "Rever o módulo de pagamentos") || !strings.Contains(string(stdin), "e os testes também") {
		t.Fatalf("stdin inesperado: %s", stdin)
	}
	if !strings.Contains(string(cwd), "repo-wt") {
		t.Fatalf("a sessão devia correr na worktree nova: %s", cwd)
	}
	s, ok, _ := a.srv.Quadro().ObterSessao(id)
	if !ok || !s.Headless || s.Papel != "revisor" {
		t.Fatalf("sessão headless mal registada: %+v", s)
	}
	if c, _ := a.postJSON("/v1/sessoes/nova", map[string]any{"raiz": repo, "tarefa": "Rever o módulo de pagamentos"}); c != http.StatusConflict {
		t.Fatalf("branch repetida devia dar 409, deu %d", c)
	}
}

func TestFluxoUsaUltimaRespostaDoHookEDiff(t *testing.T) {
	repo, wt := novoRepo(t)
	os.WriteFile(filepath.Join(wt, "novo.py"), []byte("def f():\n    return 1\n"), 0o644)
	os.WriteFile(filepath.Join(wt, "a.txt"), []byte("alterado"), 0o644)
	a := novoAmbiente(t, nil)
	a.enviar(map[string]any{"session_id": "o", "hook_event_name": "UserPromptSubmit", "cwd": wt, "prompt": "x"})
	a.enviar(map[string]any{"session_id": "d", "hook_event_name": "UserPromptSubmit", "cwd": repo, "prompt": "y"})
	a.esperar()
	a.postJSON("/v1/fluxos/arestas", map[string]any{"de": "o", "para": "d", "modelo": "R={resposta}|P={pasta}|D={diff}"})
	a.responder(map[string]any{"session_id": "o", "hook_event_name": "Stop", "cwd": wt, "last_assistant_message": "criei novo.py"})
	time.Sleep(150 * time.Millisecond)
	a.esperar()
	ms, _ := a.srv.Quadro().MensagensRecentes("d", 1)
	if len(ms) != 1 {
		t.Fatal("a passagem não chegou ao destino")
	}
	m := ms[0].Texto
	for _, esperado := range []string{"R=criei novo.py", "P=" + wt, "a.txt", "-a", "+alterado", "ficheiro novo novo.py", "return 1"} {
		if !strings.Contains(m, esperado) {
			t.Errorf("mensagem sem %q:\n%s", esperado, m)
		}
	}
	if out, _ := execGit(wt, "diff", "--cached", "--name-only"); out != "" {
		t.Fatalf("o índice da origem não devia ser tocado: %q", out)
	}
}

func execGit(dir string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	return strings.TrimSpace(string(out)), err
}
