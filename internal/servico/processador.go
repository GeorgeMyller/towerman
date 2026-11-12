package servico

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/GeorgeMyller/towerman/internal/caminhos"
	"github.com/GeorgeMyller/towerman/internal/gitinfo"
	"github.com/GeorgeMyller/towerman/internal/hook"
	"github.com/GeorgeMyller/towerman/internal/processo"
	"github.com/GeorgeMyller/towerman/internal/quadro"
	"github.com/GeorgeMyller/towerman/internal/sessoesclaude"
)

const tamanhoResumo = 120

var reGit = regexp.MustCompile(`\bgit\s+(?:-C\s+\S+\s+)?(checkout|switch|commit|push|worktree)\b`)

type evento struct {
	SessionID      string          `json:"session_id"`
	HookEventName  string          `json:"hook_event_name"`
	Cwd            string          `json:"cwd"`
	TranscriptPath string          `json:"transcript_path"`
	ToolName       string          `json:"tool_name"`
	ToolInput      json.RawMessage `json:"tool_input"`
	Prompt         string          `json:"prompt"`
	// UltimaResposta vem no Stop; a transcrição pode ainda não ter a última mensagem escrita.
	UltimaResposta string `json:"last_assistant_message"`
	// Notification: "idle_prompt" quando o Claude só espera um novo pedido.
	TipoNotificacao string `json:"notification_type"`
	Mensagem        string `json:"message"`
	// Origem vem no SessionStart: "startup", "resume", "clear" ou "compact".
	Origem string `json:"source"`
}

// estadoAoArrancar é o estado de uma sessão no SessionStart. Uma sessão que acaba de abrir ou de
// ser retomada (claude --resume, «Focar») está à espera do primeiro pedido: fica parada, para que
// uma mensagem do telemóvel entre pelo terminal e não fique na fila de um Stop que nunca chega.
// Passa a ativa no UserPromptSubmit, ou na primeira ferramenta, como antes; as sessões headless
// recebem a tarefa logo a seguir pelo stdin. O "clear" também deixa a sessão no prompt. O
// "compact" pode acontecer a meio de um passo (compactação automática): aí o estado fica como
// estava, para não dar como parada uma sessão que continua a trabalhar.
func estadoAoArrancar(ev evento, atual string) string {
	if ev.Origem == "compact" {
		if atual == "" || atual == quadro.Terminada {
			return quadro.Ativa
		}
		return atual
	}
	return quadro.Parada
}

// ociosa diz se a notificação é só "à espera do seu pedido" (a sessão terminou o passo),
// e não um pedido de permissão ou uma pergunta.
func ociosa(ev evento) bool {
	if ev.TipoNotificacao != "" {
		return ev.TipoNotificacao == "idle_prompt"
	}
	return strings.Contains(strings.ToLower(ev.Mensagem), "waiting for your input")
}

type entradaFerramenta struct {
	FilePath     string `json:"file_path"`
	NotebookPath string `json:"notebook_path"`
	Command      string `json:"command"`
	Description  string `json:"description"`
	Pattern      string `json:"pattern"`
	URL          string `json:"url"`
	Questions    []struct {
		Question string `json:"question"`
	} `json:"questions"`
}

type Processador struct {
	Quadro    *quadro.Quadro
	Git       *gitinfo.Resolvedor
	ClaudeDir string
	Agora     func() time.Time

	// PermissoesNoPainel diz se um PermissionRequest deve esperar pela decisão no painel.
	PermissoesNoPainel func() bool
	ModoRegra2         func() string
	// DecisoesLigadas diz se o Stop procura decisões em texto livre (preferência «decisoes»).
	DecisoesLigadas func() bool

	mu sync.Mutex
	// retomadas: sessões cuja transcrição já foi lida à procura da conversa de origem.
	retomadas map[string]bool
}

func (p *Processador) Processar(env hook.Envelope) (Resposta, error) {
	var resp Resposta
	var ev evento
	if err := json.Unmarshal(env.Evento, &ev); err != nil {
		return resp, err
	}
	if ev.SessionID == "" || ev.HookEventName == "" {
		return resp, errors.New("evento sem session_id ou hook_event_name")
	}
	agora := p.Agora()
	novo, err := p.Quadro.RegistarEvento(ev.SessionID, ev.HookEventName, env.RecebidoEm, agora)
	if err != nil || !novo {
		return resp, err
	}
	agoraTxt := agora.UTC().Format(quadro.FormatoData)

	s, existe, err := p.Quadro.ObterSessao(ev.SessionID)
	if err != nil {
		return resp, err
	}
	if !existe {
		s = quadro.Sessao{SessionID: ev.SessionID, Terminal: "{}", IniciadaEm: agoraTxt}
	}
	if env.Agente != "" {
		s.Agente = env.Agente
	} else if s.Agente == "" {
		s.Agente = "claude"
	}
	if s.Estado == quadro.Terminada && ev.HookEventName != "SessionEnd" {
		s.TerminadaEm = ""
	}

	cwdMudou := ev.Cwd != "" && ev.Cwd != s.Cwd
	if ev.Cwd != "" {
		s.Cwd = ev.Cwd
	}
	if ev.TranscriptPath != "" {
		s.TranscriptPath = ev.TranscriptPath
	}
	if !existe || cwdMudou || ev.HookEventName == "SessionStart" {
		p.atualizarGit(&s, false)
	}
	if ev.HookEventName == "SessionStart" || ev.HookEventName == "UserPromptSubmit" || s.Pid == 0 || !processo.Vivo(s.Pid) {
		p.identificar(&s, env.Ppid, env.Terminal)
	}

	// No SessionStart (retomada incluída), o terminal é o de agora, mesmo vazio: um TMUX_PANE
	// antigo apontava para outro painel e a resposta do telemóvel ia parar lá.
	if env.Terminal != (hook.Terminal{}) || ev.HookEventName == "SessionStart" {
		if t, err := json.Marshal(env.Terminal); err == nil {
			s.Terminal = string(t)
		}
	}

	// Antes do Stop ler a fila: as mensagens da cópia antiga já entram neste.
	if ev.HookEventName == "SessionStart" || ev.HookEventName == "Stop" {
		if err := p.terminarCopiaAntiga(s, agora); err != nil {
			return resp, err
		}
	}

	switch ev.HookEventName {
	case "SessionStart":
		s.Estado = estadoAoArrancar(ev, s.Estado)
		s.IniciadaEm = agoraTxt
		if resp.Contexto, err = p.contextoEntrada(s); err != nil {
			return resp, err
		}
	case "Continuacao":
		// Uma volta do agente do Antigravity a continuar o pedido: só atividade. Não é pedido novo,
		// por isso não fecha a decisão aberta nem muda o resumo.
		s.Estado = quadro.Ativa
	case "UserPromptSubmit":
		s.Estado = quadro.Ativa
		// A pessoa respondeu no terminal: a decisão no relógio já não está pendente.
		if err := p.fecharDecisao(s.SessionID, quadro.DecisaoNoTerminal, quadro.Resposta{Canal: quadro.CanalTerminal}, agora); err != nil {
			return resp, err
		}
		if r := resumir(ev.Prompt); r != "" {
			s.Resumo = r
		}
	case "PostToolUse":
		s.Estado = quadro.Ativa
		caminho, err := p.registarToques(&s, ev, agoraTxt)
		if err != nil {
			return resp, err
		}
		if resp.Contexto, err = p.contextoEdicao(s, caminho, agora); err != nil {
			return resp, err
		}
	case "PreToolUse":
		s.Estado = quadro.Ativa
		if alvo := alvoEdicao(ev); alvo != "" {
			c := caminhos.Normalizar(caminhos.Real(alvo, s.Cwd), s.Worktree, s.Cwd)
			if resp.Decisao, resp.Motivo, err = p.decisaoRegra2(s, c, agora); err != nil {
				return resp, err
			}
		}
	case "Notification":
		// Ociosa, a sessão aceita texto no terminal; à espera, está bloqueada num pedido.
		if ociosa(ev) {
			s.Estado = quadro.Parada
		} else {
			s.Estado = quadro.AEspera
		}
	case "PermissionRequest":
		s.Estado = quadro.AEspera
		if p.PermissoesNoPainel != nil && p.PermissoesNoPainel() {
			if resp.PermissaoID, err = p.Quadro.CriarPermissao(s.SessionID, ev.ToolName, descreverEntrada(ev.ToolInput), agora); err != nil {
				return resp, err
			}
		}
	case "Stop":
		s.Estado = quadro.Parada
		if resp.Bloquear, err = p.mensagensEmFila(s, agora); err != nil {
			return resp, err
		}
		if resp.Bloquear != "" {
			s.Estado = quadro.Ativa
		} else if resp.Passagens, err = p.dispararArestas(s, ev.UltimaResposta, agora); err != nil {
			return resp, err
		} else if err = p.registarDecisao(s, ev.UltimaResposta, agora); err != nil {
			return resp, err
		} else if (env.Ausente && !s.Headless) || s.LancadaPor != "" {
			// Longe da sessão, ou lançada à distância (sem terminal, só o aparelho a acompanha).
			resp.Fim = &fimSessao{SessionID: s.SessionID, Titulo: nomeOuID(s.Nome, filepath.Base(strings.TrimRight(s.Cwd, "/"))),
				Resposta: ev.UltimaResposta}
		}
	case "SessionEnd":
		s.Estado = quadro.Terminada
		s.TerminadaEm = agoraTxt
		if err := p.fecharDecisao(s.SessionID, quadro.DecisaoTerminada, quadro.Resposta{}, agora); err != nil {
			return resp, err
		}
		if err := p.Quadro.FalharMensagensPendentes(s.SessionID, "a sessão terminou"); err != nil {
			return resp, err
		}
	default:
		if s.Estado == "" || s.Estado == quadro.Terminada {
			s.Estado = quadro.Ativa
		}
	}
	s.VistoEm = agoraTxt
	return resp, p.Quadro.GuardarSessao(s)
}

func descreverEntrada(bruto json.RawMessage) string {
	var e entradaFerramenta
	json.Unmarshal(bruto, &e)
	pergunta := ""
	if len(e.Questions) > 0 {
		pergunta = e.Questions[0].Question
	}
	for _, v := range []string{e.Command, e.FilePath, e.NotebookPath, e.URL, e.Pattern, e.Description, pergunta} {
		if v = strings.Join(strings.Fields(v), " "); v != "" {
			if r := []rune(v); len(r) > 300 {
				v = string(r[:299]) + "…"
			}
			return v
		}
	}
	return ""
}

func (p *Processador) atualizarGit(s *quadro.Sessao, ignorarCache bool) {
	info := p.Git.Resolver(s.Cwd, ignorarCache)
	s.Repo, s.Worktree, s.Branch = info.Repo, info.Worktree, info.Branch
}

func (p *Processador) identificar(s *quadro.Sessao, ppid int, t hook.Terminal) {
	if r, ok := sessoesclaude.Procurar(p.ClaudeDir, s.SessionID); ok {
		s.Pid = r.Pid
		s.Nome = r.Nome
		return
	}
	// No Antigravity IDE, o ppid é um processo efémero do hook; o IDE (VSCODE_PID) dura enquanto
	// a janela está aberta.
	if s.Agente == "antigravity" {
		if pid, err := strconv.Atoi(t.VscodePid); err == nil && processo.Vivo(pid) {
			s.Pid = pid
			return
		}
	}
	// O pid guardado pode ser o de um processo que já morreu (o «sh -c» de um hook antigo).
	if s.Pid == 0 || !processo.Vivo(s.Pid) {
		s.Pid = ppid
	}
}

func alvoEdicao(ev evento) string {
	var e entradaFerramenta
	json.Unmarshal(ev.ToolInput, &e)
	switch ev.ToolName {
	case "Edit", "MultiEdit", "Write":
		return e.FilePath
	case "NotebookEdit":
		if e.NotebookPath != "" {
			return e.NotebookPath
		}
		return e.FilePath
	}
	return ""
}

// registarToques devolve o caminho normalizado do ficheiro editado, se houver.
func (p *Processador) registarToques(s *quadro.Sessao, ev evento, em string) (string, error) {
	var entrada entradaFerramenta
	if len(ev.ToolInput) > 0 {
		json.Unmarshal(ev.ToolInput, &entrada)
	}
	toque := func(caminho, tipo string) error {
		return p.Quadro.InserirToque(quadro.Toque{
			SessionID: s.SessionID, Repo: s.Repo, Worktree: s.Worktree, Branch: s.Branch,
			Caminho: caminho, Tipo: tipo, Ferramenta: ev.ToolName, Em: em,
		})
	}
	switch ev.ToolName {
	case "Edit", "MultiEdit", "Write", "NotebookEdit":
		alvo := entrada.FilePath
		if ev.ToolName == "NotebookEdit" && entrada.NotebookPath != "" {
			alvo = entrada.NotebookPath
		}
		if alvo == "" {
			return "", nil
		}
		tipo := "edit"
		if ev.ToolName == "Write" {
			tipo = "write"
		}
		c := caminhos.Normalizar(caminhos.Real(alvo, s.Cwd), s.Worktree, s.Cwd)
		return c, toque(c, tipo)
	case "Bash":
		subs := reGit.FindAllStringSubmatch(entrada.Command, -1)
		if len(subs) == 0 {
			return "", nil
		}
		p.atualizarGit(s, true)
		for _, m := range subs {
			if err := toque(m[1], "git"); err != nil {
				return "", err
			}
		}
	}
	return "", nil
}

// resumir salta as linhas de marcação que o harness injeta como pedido (ex.: <task-notification>).
func resumir(prompt string) string {
	for _, l := range strings.Split(prompt, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "<") {
			continue
		}
		if utf8.RuneCountInString(l) > tamanhoResumo {
			r := []rune(l)
			l = string(r[:tamanhoResumo])
		}
		return l
	}
	return ""
}
