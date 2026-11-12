package hook

// Provedor Antigravity: o `agy` (CLI) e o agente do Antigravity IDE correm hooks de
// ~/.gemini/config/hooks.json com outro contrato (conversationId, toolCall, decision). Aqui o
// evento é traduzido para a forma do Claude Code antes de seguir para o serviço, e a resposta
// do serviço volta traduzida para o contrato do Antigravity. O serviço não muda.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/GeorgeMyller/towerman/internal/config"
	"github.com/GeorgeMyller/towerman/internal/processo"
)

type eventoAgy struct {
	ConversationID string          `json:"conversationId"`
	WorkspacePaths []string        `json:"workspacePaths"`
	TranscriptPath string          `json:"transcriptPath"`
	LastUserInput  string          `json:"lastUserInput"`
	InvocationNum  *int            `json:"invocationNum"`
	ToolCall       *chamadaAgy     `json:"toolCall"`
	Error          string          `json:"error"`
	Extra          json.RawMessage `json:"-"`
}

type chamadaAgy struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

// ferramentasAgy põe as ferramentas do Antigravity com os nomes que as regras de colisão conhecem.
var ferramentasAgy = map[string]string{
	"run_command":                "Bash",
	"write_to_file":              "Write",
	"create_file":                "Write",
	"replace_file_content":       "Edit",
	"multi_replace_file_content": "Edit",
	"edit_file":                  "Edit",
	"code_edit":                  "Edit",
}

// provedorAgy distingue o CLI do IDE pela pasta da transcrição.
func provedorAgy(e eventoAgy) string {
	if strings.Contains(e.TranscriptPath, "antigravity-cli") {
		return "agy"
	}
	return "antigravity"
}

// cwdAgy é a pasta da sessão: a primeira do workspace.
func cwdAgy(e eventoAgy) string {
	if len(e.WorkspacePaths) > 0 {
		return e.WorkspacePaths[0]
	}
	return ""
}

// traduzirAgy devolve o evento na forma do Claude Code, ou nil quando o evento não interessa.
func traduzirAgy(evento string, dados []byte) (map[string]any, eventoAgy) {
	var e eventoAgy
	if json.Unmarshal(dados, &e) != nil || e.ConversationID == "" {
		return nil, e
	}
	out := map[string]any{"session_id": e.ConversationID, "transcript_path": e.TranscriptPath}
	if len(e.WorkspacePaths) > 0 {
		out["cwd"] = e.WorkspacePaths[0]
	}
	switch evento {
	case "SessionStart":
		out["hook_event_name"] = "SessionStart"
	case "PreInvocation":
		// Só a primeira volta de um pedido é um pedido novo; as outras são o agente a continuar:
		// sinal de atividade, sem o texto do pedido.
		if e.InvocationNum != nil && *e.InvocationNum > 0 {
			out["hook_event_name"] = "Continuacao"
			return out, e
		}
		out["hook_event_name"] = "UserPromptSubmit"
		out["prompt"] = e.LastUserInput
	case "PreToolUse", "PostToolUse":
		if e.ToolCall == nil {
			return nil, e
		}
		out["hook_event_name"] = evento
		nome, entrada := e.ToolCall.Name, map[string]any{}
		if n, ok := ferramentasAgy[nome]; ok {
			nome = n
		}
		for k, v := range e.ToolCall.Args {
			entrada[k] = v
		}
		if c, ok := e.ToolCall.Args["CommandLine"]; ok {
			entrada["command"] = c
		}
		for _, k := range []string{"TargetFile", "AbsolutePath", "FilePath"} {
			if f, ok := e.ToolCall.Args[k].(string); ok && f != "" {
				entrada["file_path"] = f
				break
			}
		}
		out["tool_name"] = nome
		out["tool_input"] = entrada
	case "Stop":
		out["hook_event_name"] = "Stop"
	default:
		return nil, e
	}
	return out, e
}

// respostaAgy traduz a resposta do serviço (contrato do Claude Code) para o do Antigravity.
// Sem nada a dizer, não escreve nada: o Antigravity segue com as suas permissões normais.
func respostaAgy(evento string, saida []byte) []byte {
	var r struct {
		Decision           string `json:"decision"`
		Reason             string `json:"reason"`
		HookSpecificOutput struct {
			PermissionDecision       string `json:"permissionDecision"`
			PermissionDecisionReason string `json:"permissionDecisionReason"`
			AdditionalContext        string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	json.Unmarshal(saida, &r)
	contexto := func(texto string) []byte {
		b, _ := json.Marshal(map[string]any{"injectSteps": []any{
			map[string]any{"systemMessage": map[string]any{"systemMessage": texto}}}})
		return b
	}
	switch evento {
	case "PreToolUse":
		// Regra 2 (ficheiro tocado por outra sessão): ask ou deny com o motivo.
		if d := r.HookSpecificOutput.PermissionDecision; d == "ask" || d == "deny" || d == "allow" {
			b, _ := json.Marshal(map[string]string{"decision": d, "reason": r.HookSpecificOutput.PermissionDecisionReason})
			return b
		}
		return nil
	case "PostToolUse":
		return []byte("{}")
	case "Stop":
		// Mensagem em fila (do telemóvel ou de um fluxo): o agente continua com ela.
		if r.Decision == "block" && r.Reason != "" {
			b, _ := json.Marshal(map[string]string{"decision": "continue", "reason": r.Reason})
			return b
		}
		return []byte("{}")
	case "SessionStart", "PreInvocation":
		if c := r.HookSpecificOutput.AdditionalContext; c != "" {
			return contexto(c)
		}
		return []byte("{}")
	}
	return nil
}

// ExecutarAgy é o «towerman hook --agente agy <Evento>»: o Antigravity não diz o nome do
// evento no stdin, por isso vem no argumento.
func ExecutarAgy(evento string, entrada io.Reader, saida io.Writer) Resultado {
	dados, err := io.ReadAll(io.LimitReader(entrada, TamanhoMaximo+1))
	if err != nil || len(dados) > TamanhoMaximo {
		escreverPadraoAgy(evento, saida)
		return Descartado
	}
	traduzido, e := traduzirAgy(evento, dados)
	if traduzido == nil {
		escreverPadraoAgy(evento, saida)
		return Descartado
	}
	// Um comando que o agy (CLI) pediria para aprovar vai ao telemóvel só com a pessoa longe do
	// computador e o agy arrancado em modo normal. No modo automático o agy não pergunta nada, e o
	// Towerman também não. O agente do IDE nunca é intercetado aqui.
	if evento == "PreToolUse" && e.ToolCall != nil && e.ToolCall.Name == "run_command" && provedorAgy(e) == "agy" {
		if linha, _ := e.ToolCall.Args["CommandLine"].(string); precisaAprovacaoAgy(linha) && modoAgy() == "normal" && Ausente(cwdAgy(e)) {
			return aprovarRemoto(traduzido, provedorAgy(e), cwdAgy(e), saida)
		}
	}
	corpo, _ := json.Marshal(traduzido)
	resp, r := executar(corpo, provedorAgy(e))
	if b := respostaAgy(evento, resp); len(b) > 0 {
		saida.Write(b)
	}
	return r
}

// aprovarRemoto envia o comando como pedido de permissão (o iPhone, o relógio e o Mac mostram
// Permitir/Negar) e devolve a decisão ao provedor (agy ou antigravity). Se a pessoa voltar à
// sessão enquanto espera, larga o pedido e o agente mostra a aprovação normal.
func aprovarRemoto(traduzido map[string]any, agente, cwd string, saida io.Writer) Resultado {
	pedido := map[string]any{}
	for k, v := range traduzido {
		pedido[k] = v
	}
	pedido["hook_event_name"] = "PermissionRequest"
	corpo, _ := json.Marshal(pedido)

	ctx, cancelar := context.WithCancel(context.Background())
	defer cancelar()
	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if !Ausente(cwd) {
					cancelar()
					return
				}
			}
		}
	}()
	resp, r := executarCtx(ctx, corpo, agente)
	var d struct {
		HookSpecificOutput struct {
			Decision struct {
				Behavior string `json:"behavior"`
				Message  string `json:"message"`
			} `json:"decision"`
		} `json:"hookSpecificOutput"`
	}
	json.Unmarshal(resp, &d)
	switch d.HookSpecificOutput.Decision.Behavior {
	case "allow":
		// O agente do Antigravity IDE aceita o allow na resposta do hook. O agy (CLI) não o aceita
		// sem a sua confirmação: sai sem decidir (o pedido aparece no terminal) e o serviço carrega
		// em Enter na opção pré-selecionada.
		if agente == "antigravity" {
			saida.Write([]byte(`{"decision":"allow"}`))
			break
		}
		confirmarNoTerminal(fmt.Sprint(traduzido["session_id"]))
	case "deny":
		motivo := d.HookSpecificOutput.Decision.Message
		if motivo == "" {
			motivo = "Recusado no Towerman."
		}
		b, _ := json.Marshal(map[string]string{"decision": "deny", "reason": motivo})
		saida.Write(b)
	}
	// Sem decisão (sem aparelho pareado, tempo esgotado ou pessoa de volta): nada escrito, o
	// agy segue o seu fluxo normal de aprovação.
	return r
}

// confirmarNoTerminal pede ao serviço o Enter que aceita o pedido no terminal da sessão.
func confirmarNoTerminal(sessao string) {
	token, err := config.LerToken()
	if err != nil || sessao == "" {
		return
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/v1/sessoes/%s/confirmar", config.Porta(), sessao)
	req, _ := http.NewRequest(http.MethodPost, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req); err == nil {
		resp.Body.Close()
	}
}

// precisaAprovacaoAgy diz se o agy pediria aprovação para esta linha: fora das regras
// «command(…)» de permissions.allow em ~/.gemini/antigravity-cli/settings.json.
func precisaAprovacaoAgy(linha string) bool {
	linha = strings.TrimSpace(linha)
	if linha == "" {
		return false
	}
	casa, _ := os.UserHomeDir()
	dados, err := os.ReadFile(filepath.Join(casa, ".gemini", "antigravity-cli", "settings.json"))
	if err != nil {
		return true
	}
	var cfg struct {
		Permissions struct {
			Allow []string `json:"allow"`
		} `json:"permissions"`
	}
	json.Unmarshal(dados, &cfg)
	for _, regra := range cfg.Permissions.Allow {
		if !strings.HasPrefix(regra, "command(") || !strings.HasSuffix(regra, ")") {
			continue
		}
		permitido := strings.TrimSuffix(strings.TrimPrefix(regra, "command("), ")")
		if linha == permitido || strings.HasPrefix(linha, permitido+" ") {
			return false
		}
	}
	return true
}

// modoAgy diz o modo com que arrancou o agy que lançou este hook: «auto» (--mode auto ou
// --dangerously-skip-permissions), «normal», ou vazio sem agy entre os antepassados. Uma troca de
// modo durante a sessão (Shift+Tab) não aparece nos argumentos.
var modoAgy = func() string {
	for _, pid := range processo.Ancestrais(os.Getppid()) {
		out, err := exec.Command("ps", "-o", "args=", "-p", strconv.Itoa(pid)).Output()
		if err != nil {
			continue
		}
		args := strings.Fields(string(out))
		if len(args) == 0 {
			continue
		}
		if nome := filepath.Base(args[0]); nome == "agy" || nome == "agy-ide" {
			return classificarArgsAgy(args[1:])
		}
	}
	return ""
}

func classificarArgsAgy(args []string) string {
	for i, a := range args {
		switch {
		case a == "--dangerously-skip-permissions", a == "--mode=auto":
			return "auto"
		case a == "--mode" && i+1 < len(args) && args[i+1] == "auto":
			return "auto"
		}
	}
	return "normal"
}

// escreverPadraoAgy responde o mínimo válido quando o evento não chega ao serviço.
func escreverPadraoAgy(evento string, saida io.Writer) {
	if evento != "PreToolUse" {
		saida.Write([]byte("{}"))
	}
}
