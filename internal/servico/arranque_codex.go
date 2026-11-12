package servico

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/GeorgeMyller/towerman/internal/papeis"
)

// arrancadorCodex corre o Codex CLI com codex exec --json: um processo por mensagem. O primeiro
// anuncia o thread_id; as mensagens seguintes retomam-no com codex exec resume.
//
// Sem terminal, o codex exec não pede aprovações: corre tudo dentro do sandbox. O modo default
// usa workspace-write (edita a worktree, sem rede); plan usa read-only.
type arrancadorCodex struct{}

func (arrancadorCodex) ID() string      { return "codex" }
func (arrancadorCodex) Nome() string    { return "Codex" }
func (arrancadorCodex) Modos() []string { return []string{"default", "plan"} }
func (arrancadorCodex) IDProprio() bool { return true }
func (arrancadorCodex) PorTurno() bool  { return true }

func (arrancadorCodex) Localizar() (string, error) {
	return localizarCLI("codex", ".local/bin/codex", ".npm-global/bin/codex",
		"/Applications/ChatGPT.app/Contents/Resources/codex-cli/bin/codex")
}

func (arrancadorCodex) IDNaSaida(linha []byte) (string, bool) {
	var ev struct {
		Type     string `json:"type"`
		ThreadID string `json:"thread_id"`
	}
	if json.Unmarshal(linha, &ev) != nil || ev.Type != "thread.started" || ev.ThreadID == "" {
		return "", false
	}
	return ev.ThreadID, true
}

func sandboxCodex(modo string) string {
	if modo == "plan" {
		return "read-only"
	}
	return "workspace-write"
}

// Argumentos do primeiro turno; o «-» lê a mensagem do stdin.
func (arrancadorCodex) Argumentos(_, _ string, papel *papeis.Papel, modo string) []string {
	args := []string{"exec", "--json", "-s", sandboxCodex(modo)}
	return append(args, "-")
}

func (arrancadorCodex) Retomar(id, modo string) []string {
	return []string{"exec", "resume", "--json", "-c", `sandbox_mode="` + sandboxCodex(modo) + `"`, id, "-"}
}

// Tarefa: o codex exec não tem prompt de sistema por argumento; o papel entra no início da tarefa.
func (arrancadorCodex) Tarefa(papel *papeis.Papel, tarefa string) string {
	return arrancadorAgy{}.Tarefa(papel, tarefa)
}

func (arrancadorCodex) Linha(texto string) []byte { return []byte(texto) }

// entradaTurnos faz de stdin de uma sessão por turno: cada escrita é uma mensagem na fila, que
// entra no próximo turno.
type entradaTurnos struct {
	mu      sync.Mutex
	fila    []string
	fechado bool
	sinal   chan struct{}
}

func (e *entradaTurnos) Write(b []byte) (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.fechado {
		return 0, errors.New("a sessão já não aceita mensagens")
	}
	e.fila = append(e.fila, strings.TrimSuffix(string(b), "\n"))
	e.avisar()
	return len(b), nil
}

func (e *entradaTurnos) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.fechado = true
	e.avisar()
	return nil
}

func (e *entradaTurnos) avisar() {
	select {
	case e.sinal <- struct{}{}:
	default:
	}
}

// proxima espera pela mensagem seguinte; false quando a sessão foi terminada.
func (e *entradaTurnos) proxima() (string, bool) {
	for {
		e.mu.Lock()
		if e.fechado {
			e.mu.Unlock()
			return "", false
		}
		if len(e.fila) > 0 {
			t := e.fila[0]
			e.fila = e.fila[1:]
			e.mu.Unlock()
			return t, true
		}
		e.mu.Unlock()
		<-e.sinal
	}
}

// arrancarPorTurno corre o primeiro turno, espera pelo id e deixa um supervisor a correr os turnos
// seguintes, um de cada vez, à medida que chegam mensagens.
func (s *Servico) arrancarPorTurno(ag arrancador, exe, cwd, nome string, papel *papeis.Papel, tarefa, modo string) (string, error) {
	registo := filepath.Join(s.op.Home, "headless", novoUUID()+".jsonl")
	os.MkdirAll(filepath.Dir(registo), 0o700)
	saida, err := os.OpenFile(registo, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return "", err
	}
	var mu sync.Mutex // um turno escreve no registo de cada vez, mas o stderr vem de outro fio
	escrita := escritorSeguro{w: saida, mu: &mu}
	// Cada turno tem o seu canal: todos anunciam o thread_id, só o do primeiro é lido.
	correr := func(args []string, texto string, idLido chan string) (*exec.Cmd, chan struct{}, error) {
		cmd := exec.Command(exe, args...)
		cmd.Dir = cwd
		cmd.Env = ambienteFilho()
		cmd.Stdin = strings.NewReader(texto)
		cmd.Stderr = escrita
		out, err := cmd.StdoutPipe()
		if err != nil {
			return nil, nil, err
		}
		if err := cmd.Start(); err != nil {
			return nil, nil, err
		}
		copiado := make(chan struct{})
		go func() { copiarSaida(out, escrita, ag, idLido); close(copiado) }()
		return cmd, copiado, nil
	}

	idLido := make(chan string, 1)
	cmd, copiado, err := correr(ag.Argumentos("", nome, papel, modo), ag.Tarefa(papel, tarefa), idLido)
	if err != nil {
		saida.Close()
		return "", err
	}
	var id string
	select {
	case id = <-idLido:
	case <-time.After(esperaIDAgente):
		cmd.Process.Kill()
		<-copiado
		cmd.Wait()
		saida.Close()
		return "", errors.New(ag.Nome() + " não anunciou o id da sessão")
	}
	entrada := &entradaTurnos{sinal: make(chan struct{}, 1)}
	h := &sessaoHeadless{id: id, cwd: cwd, ag: ag, processo: cmd, in: entrada, fim: make(chan struct{})}
	s.headless.Store(id, h)
	pid := cmd.Process.Pid
	go func() {
		for {
			if cmd != nil {
				<-copiado
				cmd.Wait()
			}
			texto, ok := entrada.proxima()
			if !ok {
				break
			}
			novo, c, err := correr(ag.Retomar(id, modo), texto, make(chan string, 1))
			if err != nil {
				escrita.Write([]byte(`{"towerman_erro":` + jsonTexto(err.Error()) + "}\n"))
				cmd = nil
				continue
			}
			h.mu.Lock()
			h.processo = novo
			h.mu.Unlock()
			cmd, copiado = novo, c
		}
		saida.Close()
		h.fecharEntrada()
		s.headless.Delete(id)
		close(h.fim)
		s.painel.Difusor.Mudou()
	}()
	s.enfileirarSimples(func() { s.quadro.MarcarHeadless(id, nome, cwd, papelNome(papel), pid, true, time.Now()) })
	return id, nil
}

type escritorSeguro struct {
	w  *os.File
	mu *sync.Mutex
}

func (e escritorSeguro) Write(b []byte) (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.w.Write(b)
}

func papelNome(p *papeis.Papel) string {
	if p == nil {
		return ""
	}
	return p.Nome
}

func jsonTexto(t string) string {
	b, _ := json.Marshal(t)
	return string(b)
}
