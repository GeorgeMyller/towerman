package servico

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/GeorgeMyller/towerman/internal/config"
	"github.com/GeorgeMyller/towerman/internal/hook"
	"github.com/GeorgeMyller/towerman/internal/inventario"
	"github.com/GeorgeMyller/towerman/internal/processo"
	"github.com/GeorgeMyller/towerman/internal/quadro"
)

const (
	esperaPermissao = 10 * time.Minute
	esperaExtensao  = 2 * time.Second
	tamanhoMensagem = 8000
)

// decisaoTerminal liberta a espera de um pedido que a pessoa já respondeu no terminal.
const decisaoTerminal = "terminal"

// espera é um PermissionRequest parado à espera da decisão no painel ou na app.
type espera struct {
	sessao     string
	ferramenta string
	entrada    json.RawMessage
	decisao    chan decisaoPermissao
}

type decisaoPermissao struct {
	comportamento string
	respostas     map[string]string
}

type eventoHook struct {
	SessionID     string          `json:"session_id"`
	HookEventName string          `json:"hook_event_name"`
	ToolName      string          `json:"tool_name"`
	ToolInput     json.RawMessage `json:"tool_input"`
}

type terminal struct {
	TermProgram string `json:"TERM_PROGRAM"`
	TmuxPane    string `json:"TMUX_PANE"`
}

func terminalDe(s quadro.Sessao) terminal {
	var t terminal
	json.Unmarshal([]byte(s.Terminal), &t)
	return t
}

// tarefa corre f no escritor único e espera pelo resultado.
func tarefa[T any](s *Servico, r *http.Request, f func() (T, error)) (T, error) {
	var zero T
	type res struct {
		v   T
		err error
	}
	c := make(chan res, 1)
	select {
	case s.fila <- func() { v, err := f(); c <- res{v, err} }:
	default:
		return zero, fmt.Errorf("fila cheia")
	}
	select {
	case x := <-c:
		return x.v, x.err
	case <-r.Context().Done():
		return zero, r.Context().Err()
	}
}

// responderEvento processa o evento no escritor e devolve ao hook o JSON que a sessão deve receber.
func (s *Servico) responderEvento(w http.ResponseWriter, r *http.Request, env hook.Envelope) {
	resp, err := tarefa(s, r, func() (Resposta, error) { return s.proc.Processar(env) })
	s.posProcessar(resp)
	if err != nil {
		if err.Error() == "fila cheia" {
			http.Error(w, `{"erro":"fila cheia"}`, http.StatusServiceUnavailable)
			return
		}
		escreverJSON(w, map[string]any{})
		return
	}
	var ev eventoHook
	json.Unmarshal(env.Evento, &ev)
	var saida any
	switch {
	case resp.Decisao != "":
		saida = map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PreToolUse",
			"permissionDecision": resp.Decisao, "permissionDecisionReason": resp.Motivo}}
	case resp.Bloquear != "":
		saida = map[string]any{"decision": "block", "reason": resp.Bloquear}
	case resp.Contexto != "":
		saida = map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": ev.HookEventName, "additionalContext": resp.Contexto}}
	case resp.PermissaoID > 0:
		saida = s.esperarPermissao(r, resp.PermissaoID, ev)
	}
	if saida == nil {
		escreverJSON(w, map[string]any{})
		return
	}
	escreverJSON(w, map[string]any{"saida": saida})
}

func (s *Servico) esperarPermissao(r *http.Request, id int64, ev eventoHook) any {
	e := &espera{sessao: ev.SessionID, ferramenta: ev.ToolName, entrada: ev.ToolInput, decisao: make(chan decisaoPermissao, 1)}
	s.esperas.Store(id, e)
	s.painel.Difusor.Mudou()
	defer func() {
		s.esperas.Delete(id)
		s.painel.Difusor.Mudou()
	}()
	var d decisaoPermissao
	select {
	case d = <-e.decisao:
	case <-time.After(esperaPermissao):
	case <-r.Context().Done():
	}
	switch d.comportamento {
	case "":
		s.enfileirarSimples(func() { s.quadro.DecidirPermissao(id, "expirada", quadro.Resposta{}, time.Now()) })
		return nil
	case decisaoTerminal:
		s.enfileirarSimples(func() {
			s.quadro.DecidirPermissao(id, decisaoTerminal, quadro.Resposta{Canal: quadro.CanalTerminal}, time.Now())
		})
		return nil
	}
	saida := map[string]any{"behavior": d.comportamento}
	if d.comportamento == "deny" {
		saida["message"] = "Recusado no painel do Towerman."
	}
	if d.comportamento == "allow" && len(d.respostas) > 0 {
		// AskUserQuestion: a escolha vai em updatedInput.answers, sem teclas no terminal.
		var entrada map[string]any
		json.Unmarshal(e.entrada, &entrada)
		if entrada == nil {
			entrada = map[string]any{}
		}
		entrada["answers"] = d.respostas
		saida["updatedInput"] = entrada
	}
	return map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PermissionRequest", "decision": saida}}
}

// libertarEsperas solta os pedidos que a sessão já resolveu no terminal: o Claude Code mostra o
// pedido no terminal enquanto o hook espera e não cancela o hook quando a pessoa responde lá.
// Um PostToolUse da mesma ferramenta (e entrada) prova a resposta; Stop, UserPromptSubmit e
// SessionEnd provam que nada daquela sessão continua à espera.
func (s *Servico) libertarEsperas(env hook.Envelope) {
	var ev eventoHook
	if json.Unmarshal(env.Evento, &ev) != nil || ev.SessionID == "" {
		return
	}
	todas := false
	switch ev.HookEventName {
	case "Stop", "UserPromptSubmit", "SessionEnd":
		todas = true
	case "PostToolUse":
	default:
		return
	}
	var mesmaFerramenta []*espera
	s.esperas.Range(func(_, v any) bool {
		e := v.(*espera)
		if e.sessao != ev.SessionID {
			return true
		}
		if todas {
			libertar(e)
		} else if e.ferramenta == ev.ToolName {
			mesmaFerramenta = append(mesmaFerramenta, e)
		}
		return true
	})
	for _, e := range mesmaFerramenta {
		if len(mesmaFerramenta) == 1 || mesmaEntrada(e.entrada, ev.ToolInput) {
			libertar(e)
		}
	}
}

func libertar(e *espera) {
	select {
	case e.decisao <- decisaoPermissao{comportamento: decisaoTerminal}:
	default:
	}
}

func mesmaEntrada(a, b json.RawMessage) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}

func (s *Servico) enfileirarSimples(f func()) {
	select {
	case s.fila <- f:
	default:
	}
}

// permissaoPendente junta ao registo do quadro a entrada da ferramenta (comando, perguntas e
// opções do AskUserQuestion) para a app desenhar o pedido.
type permissaoPendente struct {
	quadro.Permissao
	Entrada json.RawMessage `json:"entrada,omitempty"`
}

type estadoPainel struct {
	Regra2              string              `json:"regra2"`
	Decisoes            []map[string]any    `json:"decisoes"`
	ResponderPermissoes bool                `json:"responder_permissoes"`
	Extensoes           int                 `json:"extensoes"`
	Permissoes          []permissaoPendente `json:"permissoes"`
	Headless            []string            `json:"headless"`
	MaxHeadless         int                 `json:"max_headless"`
	// LancadaPor: nome do aparelho que pediu cada sessão headless lançada à distância.
	LancadaPor map[string]string `json:"lancada_por"`
}

func (s *Servico) obterPainel(w http.ResponseWriter, r *http.Request) {
	ps, err := s.quadro.PermissoesPendentes()
	if err != nil {
		http.Error(w, `{"erro":"quadro"}`, http.StatusInternalServerError)
		return
	}
	e := estadoPainel{Regra2: config.LerPreferenciasEm(s.op.Home).Regra2, ResponderPermissoes: s.permissoesNoPainel.Load(), Extensoes: s.ext.ligadas(), Permissoes: []permissaoPendente{},
		Headless: []string{}, MaxHeadless: maxHeadless, LancadaPor: map[string]string{}}
	s.headless.Range(func(k, _ any) bool { e.Headless = append(e.Headless, k.(string)); return true })
	e.Decisoes = s.decisoesPainel()
	for _, id := range e.Headless {
		if q, ok, _ := s.quadro.ObterSessao(id); ok && q.LancadaPor != "" {
			e.LancadaPor[id] = "outro aparelho"
			if ap, ok := s.aparelhos.obterPorID(q.LancadaPor); ok {
				e.LancadaPor[id] = ap.Nome
			}
		}
	}
	for _, p := range ps {
		if v, ok := s.esperas.Load(p.ID); ok {
			e.Permissoes = append(e.Permissoes, permissaoPendente{Permissao: p, Entrada: v.(*espera).entrada})
		}
	}
	escreverJSON(w, e)
}

func (s *Servico) alterarPainel(w http.ResponseWriter, r *http.Request) {
	var pedido struct {
		ResponderPermissoes *bool   `json:"responder_permissoes"`
		Regra2              *string `json:"regra2"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&pedido); err != nil || (pedido.ResponderPermissoes == nil && pedido.Regra2 == nil) {
		http.Error(w, `{"erro":"json"}`, http.StatusBadRequest)
		return
	}
	if pedido.Regra2 != nil {
		switch *pedido.Regra2 {
		case config.Regra2Pedir, config.Regra2Recusar, config.Regra2Off:
		default:
			http.Error(w, `{"erro":"regra2 tem de ser ask, deny ou off"}`, http.StatusBadRequest)
			return
		}
		p := config.LerPreferenciasEm(s.op.Home)
		p.Regra2 = *pedido.Regra2
		if err := config.GuardarPreferenciasEm(s.op.Home, p); err != nil {
			http.Error(w, `{"erro":"não consegui gravar config.json"}`, http.StatusInternalServerError)
			return
		}
	}
	if pedido.ResponderPermissoes != nil {
		s.permissoesNoPainel.Store(*pedido.ResponderPermissoes)
	}
	s.painel.Difusor.Mudou()
	s.obterPainel(w, r)
}

func (s *Servico) decidirPermissao(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var pedido struct {
		Decisao   string            `json:"decisao"`
		Respostas map[string]string `json:"respostas"`
	}
	if err != nil || json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384)).Decode(&pedido) != nil ||
		(pedido.Decisao != "allow" && pedido.Decisao != "deny") || (pedido.Decisao == "deny" && len(pedido.Respostas) > 0) {
		http.Error(w, `{"erro":"pedido inválido"}`, http.StatusBadRequest)
		return
	}
	v, ok := s.esperas.Load(id)
	if !ok {
		http.Error(w, `{"erro":"esta permissão já não está à espera"}`, http.StatusGone)
		return
	}
	// Entregar primeiro: se o terminal já libertou a espera, o canal está cheio e a decisão chega tarde.
	select {
	case v.(*espera).decisao <- decisaoPermissao{comportamento: pedido.Decisao, respostas: pedido.Respostas}:
	default:
		http.Error(w, `{"erro":"já respondida no terminal"}`, http.StatusConflict)
		return
	}
	if _, err := tarefa(s, r, func() (bool, error) {
		return true, s.quadro.DecidirPermissao(id, pedido.Decisao, canalDoPedido(r), time.Now())
	}); err != nil {
		http.Error(w, `{"erro":"já decidida"}`, http.StatusConflict)
		return
	}
	escreverJSON(w, map[string]bool{"ok": true})
}

func (s *Servico) listarAvisos(w http.ResponseWriter, r *http.Request) {
	as, err := s.quadro.AvisosDesde(time.Now().Add(-7 * 24 * time.Hour))
	if err != nil {
		http.Error(w, `{"erro":"quadro"}`, http.StatusInternalServerError)
		return
	}
	if as == nil {
		as = []quadro.Aviso{}
	}
	escreverJSON(w, as)
}

func (s *Servico) marcarFalsoAlarme(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, `{"erro":"id"}`, http.StatusBadRequest)
		return
	}
	if _, err := tarefa(s, r, func() (bool, error) { return true, s.quadro.MarcarFalsoAlarme(id) }); err != nil {
		http.Error(w, `{"erro":"aviso desconhecido"}`, http.StatusNotFound)
		return
	}
	escreverJSON(w, map[string]bool{"ok": true})
}

func (s *Servico) listarMensagens(w http.ResponseWriter, r *http.Request) {
	ms, err := s.quadro.MensagensRecentes(r.PathValue("id"), 20)
	if err != nil {
		http.Error(w, `{"erro":"quadro"}`, http.StatusInternalServerError)
		return
	}
	if ms == nil {
		ms = []quadro.Mensagem{}
	}
	escreverJSON(w, ms)
}

// enviarMensagem guarda a mensagem e tenta entregá-la já se a sessão estiver parada;
// com a sessão a trabalhar, fica para o próximo Stop.
func (s *Servico) enviarMensagem(w http.ResponseWriter, r *http.Request) {
	var pedido struct {
		Texto string `json:"texto"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&pedido) != nil || strings.TrimSpace(pedido.Texto) == "" {
		http.Error(w, `{"erro":"texto vazio"}`, http.StatusBadRequest)
		return
	}
	texto := strings.TrimSpace(pedido.Texto)
	if len([]rune(texto)) > tamanhoMensagem {
		http.Error(w, `{"erro":"mensagem demasiado longa"}`, http.StatusRequestEntityTooLarge)
		return
	}
	type criada struct {
		id      int64
		sessao  quadro.Sessao
		decisao *quadro.Decisao
	}
	resp := canalDoPedido(r)
	resp.Forma = quadro.FormaTexto
	c, err := tarefa(s, r, func() (criada, error) {
		sessao, ok, err := s.quadro.ObterSessao(r.PathValue("id"))
		if err != nil || !ok {
			return criada{}, fmt.Errorf("sessão desconhecida")
		}
		if sessao.Estado == quadro.Terminada {
			return criada{}, fmt.Errorf("a sessão já terminou")
		}
		id, err := s.quadro.CriarMensagem(sessao.SessionID, texto, "stop", time.Now())
		if err != nil {
			return criada{}, err
		}
		// Texto para uma sessão com decisão pendente responde-lhe. Fecha antes de entregar: o
		// UserPromptSubmit da entrega chegaria primeiro e fecharia a decisão como «terminal».
		d, aberta, err := s.quadro.DecisaoAberta(sessao.SessionID)
		if err != nil || !aberta {
			return criada{id, sessao, nil}, err
		}
		if err := s.proc.fecharDecisao(sessao.SessionID, quadro.DecisaoRespondida, resp, time.Now()); err != nil {
			return criada{}, err
		}
		return criada{id, sessao, &d}, nil
	})
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"erro":%q}`, err.Error()), http.StatusConflict)
		return
	}
	caminho, estado, motivo := s.encaminhar(c.sessao, texto)
	m, err := tarefa(s, r, func() (quadro.Mensagem, error) {
		if err := s.quadro.MarcarMensagem(c.id, estado, caminho, motivo, time.Now()); err != nil {
			return quadro.Mensagem{}, err
		}
		// A mensagem ficou na fila ou falhou: a decisão continua por responder.
		if c.decisao != nil && estado != quadro.MensagemEntregue {
			if err := s.quadro.ReabrirDecisao(*c.decisao); err != nil {
				return quadro.Mensagem{}, err
			}
		}
		return s.quadro.ObterMensagem(c.id)
	})
	if err != nil {
		http.Error(w, `{"erro":"quadro"}`, http.StatusInternalServerError)
		return
	}
	escreverJSON(w, m)
}

func (s *Servico) entregarNoTerminal(sessao quadro.Sessao, texto string) (caminho, estado, motivo string) {
	linha := strings.Join(strings.Fields(texto), " ")
	t := terminalDe(sessao)
	switch {
	case t.TmuxPane != "":
		if err := exec.Command("tmux", "send-keys", "-t", t.TmuxPane, "-l", linha).Run(); err == nil {
			if err := exec.Command("tmux", "send-keys", "-t", t.TmuxPane, "Enter").Run(); err == nil {
				return registarEntrega(sessao, "tmux", quadro.MensagemEntregue, "")
			}
		}
		motivo = "o tmux não aceitou o texto"
	case terminalMac(t.TermProgram) && runtime.GOOS == "darwin":
		err := escreverNoTerminalMac(t.TermProgram, sessao.Pid, linha)
		if err == nil {
			return registarEntrega(sessao, "applescript", quadro.MensagemEntregue, "")
		}
		motivo = "AppleScript: " + err.Error()
	case s.viaExtensao():
		pids := processo.Ancestrais(sessao.Pid)
		r, ok := s.ext.pedirEntrega("enviar", pids, sessao.Cwd, linha)
		if ok {
			return registarEntrega(sessao, "terminal-ide", quadro.MensagemEntregue, "")
		}
		// O agente do painel do Antigravity IDE não tem terminal: a mensagem vai à conversa aberta
		// no painel da janela dona da pasta (extensão 0.9.4).
		if sessao.Agente == "antigravity" {
			rp, ok := s.ext.pedirEntrega("painel", pids, sessao.Cwd, linha)
			if ok {
				return registarEntrega(sessao, "painel-ide", quadro.MensagemEntregue, "")
			}
			log.Printf("entrega falhada na sessão %s: painel do IDE: %s", sessao.SessionID, rp.Erro)
		}
		// Sem o terminal da sessão, a extensão não escreve noutro (R-01): a mensagem espera pelo Stop.
		log.Printf("entrega falhada na sessão %s: extensão do IDE: %s", sessao.SessionID, r.Erro)
		motivo := "a extensão do IDE não encontrou o terminal desta sessão"
		if r.Erro != semTerminal && r.Erro != "" {
			motivo = "a extensão do IDE falhou: " + r.Erro
		}
		return registarEntrega(sessao, "fila", quadro.MensagemPendente, motivo+"; entra quando ela voltar a trabalhar")
	default:
		motivo = semEntrega
	}
	log.Printf("entrega falhada na sessão %s: %s", sessao.SessionID, motivo)
	return registarEntrega(sessao, "fila", quadro.MensagemPendente, "o terminal desta sessão não aceita texto de fora; entra quando ela voltar a trabalhar")
}

// registarEntrega deixa no log por onde foi (ou ficou) cada mensagem, para diagnóstico remoto.
func registarEntrega(sessao quadro.Sessao, caminho, estado, motivo string) (string, string, string) {
	if motivo == "" {
		log.Printf("mensagem para a sessão %s: %s (%s)", sessao.SessionID, caminho, estado)
	} else {
		log.Printf("mensagem para a sessão %s: %s (%s): %s", sessao.SessionID, caminho, estado, motivo)
	}
	return caminho, estado, motivo
}

// semEntrega é o motivo mostrado nos botões desativados de uma decisão (RF-19).
const semEntrega = "este terminal não aceita texto"

// viaExtensao diz se a extensão do IDE pode levar texto a uma sessão: basta haver uma janela
// ligada. O TERM_PROGRAM não conta (o Antigravity IDE e os terminais de IDE no Linux não o
// definem); a janela certa escolhe-se pelo terminal ou pela pasta (pedirEntrega).
func (s *Servico) viaExtensao() bool {
	return s.ext.ligadas() > 0
}

// entregavel diz se o texto de uma resposta chega já a esta sessão parada. Os botões de uma
// decisão só ficam ativos quando chega: uma sessão à espera de decisão nunca volta a trabalhar
// sozinha, por isso a fila do Stop não serve. A extensão vem por último, para não roubar as
// entregas que hoje vão ao tmux ou ao Terminal/iTerm.
func (s *Servico) entregavel(sessao quadro.Sessao) (bool, string) {
	if s.obterHeadless(sessao.SessionID) != nil {
		return true, ""
	}
	t := terminalDe(sessao)
	switch {
	case t.TmuxPane != "":
		return true, ""
	case terminalMac(t.TermProgram) && runtime.GOOS == "darwin" && sessao.Pid > 0:
		return true, ""
	case s.viaExtensao():
		return true, ""
	}
	return false, semEntrega
}

// ativarApp traz para a frente, no macOS, o app (IDE ou terminal) que corre a sessão. No Linux
// o xdotool faz isso; no Mac usa AppleScript «activate» e depois «open -a». No Windows,
// a janela do processo mais próximo que tenha uma (focarJanelaSO).
func ativarApp(pids []int) bool {
	if runtime.GOOS == "windows" {
		return focarJanelaSO(pids)
	}
	app := processo.AppMac(pids)
	if app == "" {
		return false
	}
	if runtime.GOOS == "darwin" {
		script := fmt.Sprintf("tell application %q to activate", app)
		if correrMac("osascript", "-e", script) == nil {
			return true
		}
	}
	return correrMac("open", "-a", app) == nil
}

// abrirPastaMac traz para a frente a janela do IDE que tem a pasta aberta: «open -a» com a
// pasta foca essa janela, mesmo noutro Space, onde o System Events não a vê.
func abrirPastaMac(pids []int, pasta string) bool {
	app := processo.AppMac(pids)
	if app == "" || pasta == "" {
		return false
	}
	return correrMac("open", "-a", app, pasta) == nil
}

func focarWorkspaceMac(workspace string) {
	if workspace == "" {
		return
	}
	script := fmt.Sprintf(`tell application "System Events"
		repeat with p in (every process whose background only is false)
			try
				repeat with w in (every window of p)
					if name of w contains %q then
						set frontmost of p to true
						tell w to perform action "AXRaise"
						return
					end if
				end repeat
			end try
		end repeat
	end tell`, workspace)
	correrMac("osascript", "-e", script)
}

// confirmar carrega em Enter no terminal da sessão, um pouco depois: o agy não aceita um
// «allow» de hook sem a sua própria confirmação, por isso a aprovação vinda do telemóvel
// confirma a opção pré-selecionada («Yes, run command») quando o pedido aparece no terminal.
func (s *Servico) confirmar(w http.ResponseWriter, r *http.Request) {
	sessao, ok, err := s.quadro.ObterSessao(r.PathValue("id"))
	if err != nil || !ok {
		http.Error(w, `{"erro":"sessão desconhecida"}`, http.StatusNotFound)
		return
	}
	t := terminalDe(sessao)
	if t.TmuxPane == "" && !s.viaExtensao() {
		escreverJSON(w, map[string]any{"ok": false, "erro": "o terminal desta sessão não aceita teclas de fora"})
		return
	}
	go func() {
		time.Sleep(atrasoConfirmar)
		if t.TmuxPane != "" {
			exec.Command("tmux", "send-keys", "-t", t.TmuxPane, "Enter").Run()
			return
		}
		s.ext.pedirEntrega("enviar", processo.Ancestrais(sessao.Pid), sessao.Cwd, "")
	}()
	escreverJSON(w, map[string]any{"ok": true})
}

var atrasoConfirmar = 1200 * time.Millisecond

var reservadosRegex = regexp.MustCompile(`[\\.+*?()|\[\]{}^$]`)

// focar traz para a frente a janela do terminal da sessão: pela extensão do IDE (que mostra o
// terminal e diz o nome da pasta, que está no título da janela) ou, sem ela, pela janela do processo.
func (s *Servico) focar(w http.ResponseWriter, r *http.Request) {
	sessao, ok, err := s.quadro.ObterSessao(r.PathValue("id"))
	if err != nil || !ok {
		http.Error(w, `{"erro":"sessão desconhecida"}`, http.StatusNotFound)
		return
	}
	garantirAmbienteGrafico()
	pids := processo.Ancestrais(sessao.Pid)
	t := terminalDe(sessao)
	if s.viaExtensao() {
		if resp, ok := s.ext.pedirEntrega("focar", pids, sessao.Cwd, ""); ok {
			if runtime.GOOS == "darwin" && abrirPastaMac(pids, resp.Pasta) {
				escreverJSON(w, map[string]any{"ok": true, "como": "extensão do IDE"})
				return
			}
			if resp.Workspace != "" {
				if runtime.GOOS == "linux" {
					titulo := "^" + reservadosRegex.ReplaceAllString(resp.Workspace, `\$0`) + " - "
					exec.Command("xdotool", "search", "--onlyvisible", "--name", titulo, "windowactivate").Run()
				} else if runtime.GOOS == "darwin" {
					focarWorkspaceMac(resp.Workspace)
				}
			}
			ativarApp(pids)
			escreverJSON(w, map[string]any{"ok": true, "como": "extensão do IDE"})
			return
		}
	}
	if runtime.GOOS == "darwin" && terminalMac(t.TermProgram) {
		if focarTerminalMac(t.TermProgram, sessao.Pid) == nil {
			escreverJSON(w, map[string]any{"ok": true, "como": "terminal do mac"})
			return
		}
	}
	if ativarApp(pids) {
		escreverJSON(w, map[string]any{"ok": true, "como": "app do terminal"})
		return
	}
	if runtime.GOOS == "linux" && focarPorPid(pids, sessao.Cwd) {
		escreverJSON(w, map[string]any{"ok": true, "como": "janela do processo"})
		return
	}
	// Sem janela à vista (tmux desligado, sessão terminada): abrir uma nova.
	como, err := s.abrirJanela(sessao, t)
	if err == nil {
		escreverJSON(w, map[string]any{"ok": true, "como": como})
		return
	}
	motivo := err.Error()
	if !s.viaExtensao() {
		motivo += "; se a sessão corre num IDE, a extensão do Towerman não está ligada (corre «towerman install» e recarrega a janela)"
	}
	escreverJSON(w, map[string]any{"ok": false, "erro": motivo})
}

// focarPorPid ativa, pelo xdotool, uma janela de um dos processos. Um IDE abre várias pastas
// no mesmo processo: primeiro procura a janela cujo título começa pela pasta da sessão, e só
// depois qualquer janela do processo.
func focarPorPid(pids []int, cwd string) bool {
	for _, padrao := range padroesJanela(cwd) {
		for _, pid := range pids {
			// Sem padrão, o xdotool lia «windowactivate» como padrão e não ativava nada; os critérios
			// juntam-se com «ou» sem o --all, e apanhavam janelas de outros programas.
			if exec.Command("xdotool", "search", "--all", "--onlyvisible", "--pid", strconv.Itoa(pid), "--name", padrao, "windowactivate").Run() == nil {
				return true
			}
		}
	}
	return false
}

// padroesJanela devolve os padrões de título a tentar, do mais certo ao mais largo.
func padroesJanela(cwd string) []string {
	pasta := filepath.Base(cwd)
	// No Windows, filepath.Base("/") dá «\».
	if cwd == "" || pasta == "." || strings.Trim(pasta, `/\`) == "" {
		return []string{"."}
	}
	return []string{"^" + reservadosRegex.ReplaceAllString(pasta, `\$0`) + " - ", "."}
}

func (s *Servico) listarWorktrees(w http.ResponseWriter, r *http.Request) {
	repos, sessoes, err := s.quadro.ReposESessoes()
	if err != nil {
		http.Error(w, `{"erro":"quadro"}`, http.StatusInternalServerError)
		return
	}
	lista := inventario.ReposConhecidos(repos, filepath.Join(s.op.Home, "repos.txt"))
	escreverJSON(w, inventario.Inventariar(r.Context(), lista, sessoes))
}

// encaminhar decide como a mensagem chega à sessão: pelo stdin se foi aberta pelo Towerman, pelo
// terminal se está parada, ou em fila para o próximo Stop se está a trabalhar. Uma sessão parada
// sem entrega (ex.: Antigravity IDE sem extensão ligada) também fica na fila do Stop; só a
// resposta a uma decisão a recusa (entregarResposta).
func (s *Servico) encaminhar(sessao quadro.Sessao, texto string) (caminho, estado, motivo string) {
	if h := s.obterHeadless(sessao.SessionID); h != nil {
		if err := h.escrever(texto); err == nil {
			return "stdin", quadro.MensagemEntregue, ""
		}
	}
	if sessao.Estado == quadro.Parada {
		return s.entregarNoTerminal(sessao, texto)
	}
	return "stop", quadro.MensagemPendente, "entra quando a sessão terminar o passo atual"
}
