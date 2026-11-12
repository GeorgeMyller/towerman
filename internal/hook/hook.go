// Package hook é o processo que o agente invoca em cada evento. Nunca falha nem bloqueia:
// qualquer erro termina no diário local ou no descarte do evento, sempre com código 0.
package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/GeorgeMyller/towerman/internal/config"
	"github.com/GeorgeMyller/towerman/internal/processo"
)

const TamanhoMaximo = 1 << 20

type Resultado int

const (
	Desligado Resultado = iota
	Descartado
	Enviado
	NoDiario
)

func (r Resultado) String() string {
	return [...]string{"desligado", "descartado", "enviado", "no-diario"}[r]
}

type Terminal struct {
	TermProgram string `json:"TERM_PROGRAM,omitempty"`
	Tmux        string `json:"TMUX,omitempty"`
	TmuxPane    string `json:"TMUX_PANE,omitempty"`
	WeztermPane string `json:"WEZTERM_PANE,omitempty"`
	VscodePid   string `json:"VSCODE_PID,omitempty"`
}

type Envelope struct {
	Agente     string          `json:"agente"`
	RecebidoEm string          `json:"recebido_em"`
	Ppid       int             `json:"ppid"`
	Terminal   Terminal        `json:"terminal"`
	Evento     json.RawMessage `json:"evento"`
	// Ausente vem só no Stop: a pessoa está longe ou noutra janela, e o serviço avisa que terminou.
	Ausente bool `json:"ausente,omitempty"`
}

// esperaResposta são os eventos em que a sessão aguarda o que o serviço decide (contexto,
// mensagem em fila, decisão de permissão) e o tempo máximo dessa espera.
var esperaResposta = map[string]time.Duration{
	"SessionStart":      300 * time.Millisecond,
	"PreToolUse":        10 * time.Millisecond,
	"PostToolUse":       15 * time.Millisecond,
	"Stop":              200 * time.Millisecond,
	"PermissionRequest": 10*time.Minute + 5*time.Second,
}

type Resposta struct {
	Saida json.RawMessage `json:"saida,omitempty"`
}

// Executar lê o evento de entrada e entrega-o ao serviço ou ao diário. O que o serviço
// devolver para a sessão é escrito em saida.
func Executar(entrada io.Reader, saida io.Writer) (r Resultado) {
	return ExecutarAgente("claude", entrada, saida)
}

// ExecutarAgente serve os agentes com o contrato de hooks do Claude Code (Claude, Codex):
// o nome do agente fica registado na sessão.
func ExecutarAgente(agente string, entrada io.Reader, saida io.Writer) (r Resultado) {
	defer func() {
		if recover() != nil {
			r = Descartado
		}
	}()
	dados, err := io.ReadAll(io.LimitReader(entrada, TamanhoMaximo+1))
	if err != nil || len(dados) > TamanhoMaximo {
		return Descartado
	}
	resp, r := executar(dados, agente)
	if len(resp) > 0 {
		saida.Write(resp)
	}
	return r
}

// executar entrega um evento na forma do Claude Code ao serviço (ou ao diário) e devolve o
// que o serviço disse para a sessão.
func executar(dados []byte, agente string) (saida []byte, r Resultado) {
	return executarCtx(context.Background(), dados, agente)
}

// executarCtx é o executar que se pode cancelar (o pedido remoto do agy larga quando a pessoa volta).
func executarCtx(ctx context.Context, dados []byte, agente string) (saida []byte, r Resultado) {
	defer func() {
		if recover() != nil {
			saida, r = nil, Descartado
		}
	}()
	if os.Getenv("TOWERMAN") == "off" {
		return nil, Desligado
	}
	var minimo struct {
		SessionID     string `json:"session_id"`
		HookEventName string `json:"hook_event_name"`
		Cwd           string `json:"cwd"`
	}
	if json.Unmarshal(dados, &minimo) != nil || minimo.SessionID == "" || minimo.HookEventName == "" {
		return nil, Descartado
	}
	env := Envelope{
		Agente:     agente,
		RecebidoEm: time.Now().UTC().Format(time.RFC3339Nano),
		Ppid:       processo.SemShell(os.Getppid()),
		Terminal: Terminal{
			TermProgram: os.Getenv("TERM_PROGRAM"),
			Tmux:        os.Getenv("TMUX"),
			TmuxPane:    os.Getenv("TMUX_PANE"),
			WeztermPane: os.Getenv("WEZTERM_PANE"),
			VscodePid:   os.Getenv("VSCODE_PID"),
		},
		Evento: json.RawMessage(bytes.TrimSpace(dados)),
	}
	if minimo.HookEventName == "Stop" {
		env.Ausente = Ausente(minimo.Cwd)
	}
	corpo, err := json.Marshal(env)
	if err != nil {
		return nil, Descartado
	}
	espera, responder := esperaResposta[minimo.HookEventName]
	if !responder {
		espera = config.TimeoutHook()
	} else if d, ok := config.TimeoutHookDefinido(); ok && d > espera {
		// Definido à mão (máquina lenta, testes com o CI carregado), é o prazo mínimo também
		// dos eventos que esperam resposta: um Stop que passe dos 200 ms ia também para o diário.
		espera = d
	}
	resp, err := enviar(ctx, corpo, responder, espera)
	if ctx.Err() != nil {
		return nil, Enviado
	}
	if err == nil {
		if len(resp.Saida) > 0 && string(resp.Saida) != "null" {
			return resp.Saida, Enviado
		}
		return nil, Enviado
	}
	if anotarNoDiario(corpo) == nil {
		return nil, NoDiario
	}
	return nil, Descartado
}

func enviar(ctx context.Context, corpo []byte, responder bool, espera time.Duration) (Resposta, error) {
	var r Resposta
	token, err := config.LerToken()
	if err != nil {
		return r, err
	}
	cliente := &http.Client{
		Timeout:   espera,
		Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true},
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/v1/eventos", config.Porta())
	if responder {
		url += "?responder=1"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(corpo))
	if err != nil {
		return r, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := cliente.Do(req)
	if err != nil {
		return r, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return r, fmt.Errorf("serviço respondeu %d", resp.StatusCode)
	}
	if responder {
		dados, err := io.ReadAll(io.LimitReader(resp.Body, TamanhoMaximo))
		if err != nil {
			return r, err
		}
		json.Unmarshal(dados, &r)
	}
	return r, nil
}

// anotarNoDiario escreve a linha numa só chamada Write com O_APPEND, para que linhas de
// hooks concorrentes nunca se intercalem.
func anotarNoDiario(corpo []byte) error {
	p, err := config.CaminhoDiario()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	linha := append(corpo, '\n')
	_, errW := f.Write(linha)
	errC := f.Close()
	if errW != nil {
		return errW
	}
	return errC
}
