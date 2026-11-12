package hook

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestTraduzirAgyFerramenta(t *testing.T) {
	ev := `{"conversationId":"c1","workspacePaths":["/w/proj"],"transcriptPath":"/u/.gemini/antigravity-cli/brain/c1/t.jsonl",
		"toolCall":{"name":"replace_file_content","args":{"TargetFile":"/w/proj/a.go"}},"stepIdx":3}`
	out, e := traduzirAgy("PreToolUse", []byte(ev))
	if out["session_id"] != "c1" || out["cwd"] != "/w/proj" || out["tool_name"] != "Edit" || out["hook_event_name"] != "PreToolUse" {
		t.Fatalf("tradução errada: %v", out)
	}
	if out["tool_input"].(map[string]any)["file_path"] != "/w/proj/a.go" {
		t.Fatalf("file_path em falta: %v", out["tool_input"])
	}
	if provedorAgy(e) != "agy" {
		t.Fatalf("o CLI devia ser agy, deu %s", provedorAgy(e))
	}
	cmd, _ := traduzirAgy("PreToolUse", []byte(`{"conversationId":"c1","toolCall":{"name":"run_command","args":{"CommandLine":"npm test"}}}`))
	if cmd["tool_name"] != "Bash" || cmd["tool_input"].(map[string]any)["command"] != "npm test" {
		t.Fatalf("run_command devia ser Bash com command: %v", cmd)
	}
}

func TestTraduzirAgyPedidoEContinuacao(t *testing.T) {
	p, e := traduzirAgy("PreInvocation", []byte(`{"conversationId":"c2","invocationNum":0,"lastUserInput":"corrige o teste","transcriptPath":"/u/.gemini/antigravity-ide/x"}`))
	if p["hook_event_name"] != "UserPromptSubmit" || p["prompt"] != "corrige o teste" || provedorAgy(e) != "antigravity" {
		t.Fatalf("primeira volta devia ser um pedido do IDE: %v", p)
	}
	q, _ := traduzirAgy("PreInvocation", []byte(`{"conversationId":"c2","invocationNum":3,"lastUserInput":"corrige o teste"}`))
	if q["hook_event_name"] != "Continuacao" || q["session_id"] != "c2" {
		t.Fatalf("voltas seguintes são continuação, não pedidos: %v", q)
	}
	if _, tem := q["prompt"]; tem {
		t.Fatalf("a continuação não leva o texto do pedido: %v", q)
	}
	if x, _ := traduzirAgy("PostInvocation", []byte(`{"conversationId":"c2"}`)); x != nil {
		t.Fatal("PostInvocation não interessa")
	}
}

func TestRespostaAgy(t *testing.T) {
	if b := respostaAgy("PreToolUse", nil); b != nil {
		t.Fatalf("sem decisão, o PreToolUse não deve escrever nada (o agy segue as permissões normais): %s", b)
	}
	b := respostaAgy("PreToolUse", []byte(`{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"ask","permissionDecisionReason":"outra sessão mexeu"}}`))
	var d map[string]string
	json.Unmarshal(b, &d)
	if d["decision"] != "ask" || d["reason"] != "outra sessão mexeu" {
		t.Fatalf("regra 2 mal traduzida: %s", b)
	}
	b = respostaAgy("Stop", []byte(`{"decision":"block","reason":"mensagem do iPhone"}`))
	json.Unmarshal(b, &d)
	if d["decision"] != "continue" || d["reason"] != "mensagem do iPhone" {
		t.Fatalf("mensagem em fila devia continuar o agente: %s", b)
	}
	if string(respostaAgy("Stop", nil)) != "{}" || string(respostaAgy("PostToolUse", nil)) != "{}" {
		t.Fatal("Stop e PostToolUse sem nada respondem {}")
	}
	b = respostaAgy("SessionStart", []byte(`{"hookSpecificOutput":{"additionalContext":"outra sessão nesta worktree"}}`))
	if !json.Valid(b) || string(b) == "{}" {
		t.Fatalf("contexto devia virar injectSteps: %s", b)
	}
}

func TestPrecisaAprovacaoAgy(t *testing.T) {
	casa := t.TempDir()
	t.Setenv("HOME", casa)
	t.Setenv("USERPROFILE", casa) // os.UserHomeDir no Windows
	if !precisaAprovacaoAgy("rm -rf build") {
		t.Fatal("sem settings, tudo precisa de aprovação")
	}
	os.MkdirAll(filepath.Join(casa, ".gemini", "antigravity-cli"), 0o755)
	os.WriteFile(filepath.Join(casa, ".gemini", "antigravity-cli", "settings.json"),
		[]byte(`{"permissions":{"allow":["command(ls)","command(docker images)","url(x)"]}}`), 0o600)
	casos := map[string]bool{"ls": false, "ls -la": false, "lsof -i": true, "docker images": false, "docker run x": true, "": false}
	for linha, quer := range casos {
		if got := precisaAprovacaoAgy(linha); got != quer {
			t.Errorf("precisaAprovacaoAgy(%q) = %v, queria %v", linha, got, quer)
		}
	}
}

func TestAusenteForcado(t *testing.T) {
	t.Setenv("TOWERMAN_AGY_REMOTO", "sempre")
	if !Ausente("") {
		t.Fatal("sempre devia forçar")
	}
	t.Setenv("TOWERMAN_AGY_REMOTO", "nunca")
	if Ausente("") {
		t.Fatal("nunca devia desligar")
	}
	t.Setenv("TOWERMAN_AGY_REMOTO", "")
	t.Setenv("TOWERMAN_AUSENTE_S", "0")
	if _, ok := Inatividade(); ok && !Ausente("") {
		t.Fatal("com limite 0, qualquer inatividade conta como ausente")
	}
}

// Com «Permitir» no telemóvel, o agente do Antigravity IDE recebe o allow na resposta do hook; o
// agy (CLI) não o aceita e o serviço carrega em Enter no terminal.
func TestAprovarRemotoPorProvedor(t *testing.T) {
	prepararHome(t, "abc")
	t.Setenv("TOWERMAN_AGY_REMOTO", "sempre")
	var mu sync.Mutex
	var confirmados []string
	usarServidor(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/confirmar") {
			mu.Lock()
			confirmados = append(confirmados, r.URL.Path)
			mu.Unlock()
			return
		}
		io.Copy(io.Discard, r.Body)
		w.Write([]byte(`{"saida":{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"allow"}}}}`))
	})
	pedido := map[string]any{"session_id": "c3", "hook_event_name": "PreToolUse", "tool_name": "Bash",
		"tool_input": map[string]any{"command": "rm -rf build"}}

	var saida bytes.Buffer
	aprovarRemoto(pedido, "antigravity", "", &saida)
	var d map[string]string
	if json.Unmarshal(saida.Bytes(), &d) != nil || d["decision"] != "allow" {
		t.Fatalf("o IDE devia receber {\"decision\":\"allow\"}: %q", saida.String())
	}
	if len(confirmados) != 0 {
		t.Fatalf("com o IDE, nada de Enter no terminal: %v", confirmados)
	}

	saida.Reset()
	aprovarRemoto(pedido, "agy", "", &saida)
	if saida.Len() != 0 {
		t.Fatalf("o agy não aceita allow do hook; nada devia ser escrito: %q", saida.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(confirmados) != 1 || confirmados[0] != "/v1/sessoes/c3/confirmar" {
		t.Fatalf("o agy devia pedir o Enter ao serviço: %v", confirmados)
	}
}

func TestClassificarArgsAgy(t *testing.T) {
	casos := map[string]string{
		"":                               "normal",
		"--model pro":                    "normal",
		"--mode auto":                    "auto",
		"--mode=auto":                    "auto",
		"--mode plan":                    "normal",
		"--dangerously-skip-permissions": "auto",
		"-c --mode auto --effort high":   "auto",
	}
	for args, quer := range casos {
		if d := classificarArgsAgy(strings.Fields(args)); d != quer {
			t.Errorf("%q: %q, queria %q", args, d, quer)
		}
	}
}

// O agy em modo automático não pergunta nada: o Towerman também não, mesmo com a pessoa longe.
// Em modo normal, com a pessoa longe e um comando fora dos permitidos, o pedido vai ao telemóvel.
// O agente do IDE e um hook sem agy entre os antepassados seguem direto.
func TestExecutarAgyAprovacaoRemotaPeloModo(t *testing.T) {
	prepararHome(t, "abc")
	t.Setenv("TOWERMAN_AGY_REMOTO", "sempre") // força ausente
	var mu sync.Mutex
	var eventos []string
	usarServidor(t, func(w http.ResponseWriter, r *http.Request) {
		var env Envelope
		json.NewDecoder(r.Body).Decode(&env)
		var ev map[string]any
		json.Unmarshal(env.Evento, &ev)
		mu.Lock()
		eventos = append(eventos, fmt.Sprint(ev["hook_event_name"]))
		mu.Unlock()
		w.Write([]byte(`{"saida":{}}`))
	})
	original := modoAgy
	t.Cleanup(func() { modoAgy = original })
	cli := `{"conversationId":"c-cli","transcriptPath":"/u/.gemini/antigravity-cli/t.jsonl",
		"toolCall":{"name":"run_command","args":{"CommandLine":"rm -rf build"}}}`
	ide := `{"conversationId":"c-ide","transcriptPath":"/u/.gemini/antigravity/t.jsonl",
		"toolCall":{"name":"run_command","args":{"CommandLine":"rm -rf build"}}}`
	casos := []struct {
		nome, modo, entrada, quer string
	}{
		{"agy automático", "auto", cli, "PreToolUse"},
		{"sem agy nos antepassados", "", cli, "PreToolUse"},
		{"agy normal", "normal", cli, "PermissionRequest"},
		{"agente do IDE", "normal", ide, "PreToolUse"},
	}
	for _, c := range casos {
		modoAgy = func() string { return c.modo }
		mu.Lock()
		eventos = nil
		mu.Unlock()
		var saida bytes.Buffer
		ExecutarAgy("PreToolUse", strings.NewReader(c.entrada), &saida)
		mu.Lock()
		if len(eventos) != 1 || eventos[0] != c.quer {
			t.Errorf("%s: o serviço recebeu %v, queria %s", c.nome, eventos, c.quer)
		}
		mu.Unlock()
	}
}

func TestExecutarAgyPreToolUseNaoInterferePorPadrao(t *testing.T) {
	prepararHome(t, "abc")
	t.Setenv("TOWERMAN_AGY_REMOTO", "")
	t.Setenv("TOWERMAN_AUSENTE_S", "0") // Forçaria ausente se medido

	var chamadaHook string
	usarServidor(t, func(w http.ResponseWriter, r *http.Request) {
		var env Envelope
		json.NewDecoder(r.Body).Decode(&env)
		var ev map[string]any
		json.Unmarshal(env.Evento, &ev)
		chamadaHook, _ = ev["hook_event_name"].(string)
		w.Write([]byte(`{"saida":{}}`))
	})

	// 1. Antigravity IDE: mesmo em run_command e ausente, segue direto como PreToolUse sem bloquear
	entradaIDE := strings.NewReader(`{"conversationId":"c-ide","transcriptPath":"/u/.gemini/antigravity/t.jsonl",
		"toolCall":{"name":"run_command","args":{"CommandLine":"npm test"}}}`)
	var saida bytes.Buffer
	res := ExecutarAgy("PreToolUse", entradaIDE, &saida)
	if res != Enviado {
		t.Fatalf("esperava Enviado, deu %v", res)
	}
	if chamadaHook != "PreToolUse" {
		t.Fatalf("devia enviar PreToolUse normal ao serviço, enviou %q", chamadaHook)
	}

	// 2. agy CLI: por omissão (sem TOWERMAN_AGY_REMOTO=sempre), também não intercepta
	entradaCLI := strings.NewReader(`{"conversationId":"c-cli","transcriptPath":"/u/.gemini/antigravity-cli/t.jsonl",
		"toolCall":{"name":"run_command","args":{"CommandLine":"git status"}}}`)
	saida.Reset()
	chamadaHook = ""
	res = ExecutarAgy("PreToolUse", entradaCLI, &saida)
	if res != Enviado {
		t.Fatalf("esperava Enviado, deu %v", res)
	}
	if chamadaHook != "PreToolUse" {
		t.Fatalf("devia enviar PreToolUse normal ao serviço, enviou %q", chamadaHook)
	}
}
