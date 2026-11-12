package servico

import (
	"bufio"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/GeorgeMyller/towerman/internal/papeis"
)

const (
	maxHeadless     = 3
	esperaFimSessao = 10 * time.Second
	esperaIDAgente  = 15 * time.Second
)

// sessaoHeadless é um agente em modo -p arrancado pelo Towerman, que recebe mensagens pelo stdin em stream-json.
type sessaoHeadless struct {
	id  string
	cwd string
	ag  arrancador
	mu  sync.Mutex
	// processo é o que está a correr agora; nos agentes por turno muda a cada mensagem.
	processo *exec.Cmd
	in       io.WriteCloser
	fim      chan struct{}
}

func (h *sessaoHeadless) matar() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.processo != nil && h.processo.Process != nil {
		h.processo.Process.Kill()
	}
}

func (h *sessaoHeadless) escrever(texto string) error {
	linha := h.ag.Linha(texto)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.in == nil {
		return errors.New("a sessão já não aceita mensagens")
	}
	_, err := h.in.Write(append(linha, '\n'))
	return err
}

func (h *sessaoHeadless) fecharEntrada() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.in != nil {
		h.in.Close()
		h.in = nil
	}
}

func novoUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// localizarCLI procura um CLI fora do PATH mínimo com que o systemd arranca o serviço. Os caminhos
// relativos são da pasta pessoal; os absolutos (ex.: dentro de um .app) usam-se como estão.
func localizarCLI(nome string, caminhos ...string) (string, error) {
	if p, err := exec.LookPath(nome); err == nil {
		return p, nil
	}
	h, _ := os.UserHomeDir()
	for _, p := range caminhos {
		c := p
		if !filepath.IsAbs(p) {
			c = filepath.Join(h, p)
		}
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c, nil
		}
	}
	return "", errors.New("não encontrei o comando " + nome)
}

func localizarClaude() (string, error) {
	return localizarCLI("claude", ".local/bin/claude", ".claude/local/claude", ".npm-global/bin/claude")
}

// arrancador sabe correr um agente em modo -p com mensagens em stream-json pelo stdin.
type arrancador interface {
	ID() string
	Nome() string
	// Modos são os modos de permissões do Towerman que o agente aceita, o primeiro é o padrão.
	Modos() []string
	Localizar() (string, error)
	Argumentos(id, nome string, papel *papeis.Papel, modo string) []string
	Tarefa(papel *papeis.Papel, tarefa string) string
	Linha(texto string) []byte
	// IDProprio diz que o agente escolhe o id da sessão e o anuncia no stdout (lido por IDNaSaida).
	IDProprio() bool
	IDNaSaida(linha []byte) (string, bool)
	// PorTurno: o agente corre um processo por mensagem (lida do stdin até ao fim); as seguintes
	// retomam a sessão com os argumentos de Retomar.
	PorTurno() bool
	Retomar(id, modo string) []string
}

var arrancadores = []arrancador{arrancadorClaude{}, arrancadorCodex{}, arrancadorAgy{}}

func procurarArrancador(id string) (arrancador, bool) {
	for _, a := range arrancadores {
		if a.ID() == id {
			return a, true
		}
	}
	return nil, false
}

type arrancadorClaude struct{}

func (arrancadorClaude) ID() string                      { return "claude" }
func (arrancadorClaude) Nome() string                    { return "Claude Code" }
func (arrancadorClaude) Modos() []string                 { return []string{"acceptEdits", "default", "plan"} }
func (arrancadorClaude) Localizar() (string, error)      { return localizarClaude() }
func (arrancadorClaude) IDProprio() bool                 { return false }
func (arrancadorClaude) IDNaSaida([]byte) (string, bool) { return "", false }
func (arrancadorClaude) PorTurno() bool                  { return false }
func (arrancadorClaude) Retomar(string, string) []string { return nil }

func (arrancadorClaude) Argumentos(id, nome string, papel *papeis.Papel, modo string) []string {
	args := []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose",
		"--session-id", id, "-n", nome, "--permission-mode", modo}
	if papel != nil {
		if papel.Prompt != "" {
			args = append(args, "--append-system-prompt", papel.Prompt)
		}
		if len(papel.Ferramentas) > 0 {
			args = append(args, "--allowedTools", strings.Join(papel.Ferramentas, ","))
		}
		if papel.Modelo != "" {
			args = append(args, "--model", papel.Modelo)
		}
	}
	return args
}

func (arrancadorClaude) Tarefa(_ *papeis.Papel, tarefa string) string { return tarefa }

func (arrancadorClaude) Linha(texto string) []byte {
	b, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": texto}})
	return b
}

// ambienteFilho garante que as sessões filhas encontram «towerman hook» e o próprio claude.
func ambienteFilho() []string {
	h, _ := os.UserHomeDir()
	extra := []string{filepath.Join(h, ".local", "bin")}
	if exe, err := os.Executable(); err == nil {
		extra = append([]string{filepath.Dir(exe)}, extra...)
	}
	env := os.Environ()
	for i, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			env[i] = "PATH=" + strings.Join(extra, string(os.PathListSeparator)) + string(os.PathListSeparator) + kv[5:]
			return env
		}
	}
	return append(env, "PATH="+strings.Join(extra, string(os.PathListSeparator))+string(os.PathListSeparator)+"/usr/local/bin:/usr/bin:/bin")
}

func (s *Servico) contarHeadless() int {
	n := 0
	s.headless.Range(func(_, _ any) bool { n++; return true })
	return n
}

func (s *Servico) obterHeadless(id string) *sessaoHeadless {
	if v, ok := s.headless.Load(id); ok {
		return v.(*sessaoHeadless)
	}
	return nil
}

// arrancarHeadless corre o agente numa pasta com o papel aplicado e a tarefa como primeira mensagem.
func (s *Servico) arrancarHeadless(ag arrancador, cwd, nome string, papel *papeis.Papel, tarefa, modo string) (string, error) {
	if s.contarHeadless() >= maxHeadless {
		return "", fmt.Errorf("já há %d sessões abertas pelo Towerman (máximo %d)", maxHeadless, maxHeadless)
	}
	exe, err := ag.Localizar()
	if err != nil {
		return "", err
	}
	if ag.PorTurno() {
		return s.arrancarPorTurno(ag, exe, cwd, nome, papel, tarefa, modo)
	}
	// O id do Towerman nomeia o registo; o Claude usa-o como id da sessão, outros agentes escolhem o seu.
	id := novoUUID()
	nomePapel := ""
	if papel != nil {
		nomePapel = papel.Nome
	}
	cmd := exec.Command(exe, ag.Argumentos(id, nome, papel, modo)...)
	cmd.Dir = cwd
	cmd.Env = ambienteFilho()
	registo := filepath.Join(s.op.Home, "headless", id+".jsonl")
	os.MkdirAll(filepath.Dir(registo), 0o700)
	saida, err := os.OpenFile(registo, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return "", err
	}
	cmd.Stderr = saida
	out, err := cmd.StdoutPipe()
	if err != nil {
		saida.Close()
		return "", err
	}
	in, err := cmd.StdinPipe()
	if err != nil {
		saida.Close()
		return "", err
	}
	if err := cmd.Start(); err != nil {
		saida.Close()
		return "", err
	}
	idLido := make(chan string, 1)
	copiado := make(chan struct{})
	go func() { copiarSaida(out, saida, ag, idLido); close(copiado) }()
	parar := func() {
		cmd.Process.Kill()
		<-copiado
		cmd.Wait()
		saida.Close()
	}
	// A tarefa entra antes de esperar pelo id: há agentes que só o anunciam depois da primeira mensagem.
	if _, err := in.Write(append(ag.Linha(ag.Tarefa(papel, tarefa)), '\n')); err != nil {
		parar()
		return "", err
	}
	if ag.IDProprio() {
		select {
		case id = <-idLido:
		case <-time.After(esperaIDAgente):
			parar()
			return "", errors.New(ag.Nome() + " não anunciou o id da sessão")
		}
	}
	h := &sessaoHeadless{id: id, cwd: cwd, ag: ag, processo: cmd, in: in, fim: make(chan struct{})}
	s.headless.Store(id, h)
	go func() {
		// O Wait fecha o stdout: só depois de o ler até ao fim.
		<-copiado
		cmd.Wait()
		saida.Close()
		h.fecharEntrada()
		s.headless.Delete(id)
		close(h.fim)
		s.painel.Difusor.Mudou()
	}()
	s.enfileirarSimples(func() { s.quadro.MarcarHeadless(id, nome, cwd, nomePapel, cmd.Process.Pid, true, time.Now()) })
	return id, nil
}

// copiarSaida grava o stdout do agente no registo e entrega o primeiro id que encontrar.
func copiarSaida(out io.Reader, registo io.Writer, ag arrancador, idLido chan<- string) {
	r := bufio.NewReader(out)
	enviado := false
	for {
		linha, err := r.ReadBytes('\n')
		if len(linha) > 0 {
			registo.Write(linha)
			if !enviado {
				if id, ok := ag.IDNaSaida(linha); ok && idSessao.MatchString(id) {
					idLido <- id
					enviado = true
				}
			}
		}
		if err != nil {
			return
		}
	}
}

func (s *Servico) terminarHeadless(id string) bool {
	h := s.obterHeadless(id)
	if h == nil {
		return false
	}
	h.fecharEntrada()
	select {
	case <-h.fim:
	case <-time.After(esperaFimSessao):
		h.matar()
		<-h.fim
	}
	return true
}

var reNaoSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slug(t string, max int) string {
	t = strings.ToLower(t)
	for de, para := range map[string]string{"á": "a", "à": "a", "ã": "a", "â": "a", "é": "e", "ê": "e", "í": "i", "ó": "o", "õ": "o", "ô": "o", "ú": "u", "ç": "c"} {
		t = strings.ReplaceAll(t, de, para)
	}
	t = strings.Trim(reNaoSlug.ReplaceAllString(t, "-"), "-")
	if len(t) > max {
		t = strings.Trim(t[:max], "-")
	}
	return t
}

var reBranch = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,99}$`)

type pedidoNovaSessao struct {
	Raiz   string `json:"raiz"`
	Base   string `json:"base"`
	Branch string `json:"branch"`
	Papel  string `json:"papel"`
	Tarefa string `json:"tarefa"`
	Modo   string `json:"modo"`
}

// novaSessao cria uma worktree ao lado do repo (<repo>-wt/<nome>) e arranca lá uma sessão headless.
// Só aceita repos que o Towerman já conhece, para o painel não poder criar pastas em sítios arbitrários.
func (s *Servico) novaSessao(w http.ResponseWriter, r *http.Request) {
	var p pedidoNovaSessao
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&p); err != nil {
		http.Error(w, `{"erro":"json"}`, http.StatusBadRequest)
		return
	}
	res := s.lancarSessao(r.Context(), pedidoLancamento{Origin: "painel", Root: p.Raiz, Base: p.Base,
		Branch: p.Branch, Role: p.Papel, Task: p.Tarefa, Mode: p.Modo})
	if res.Erro != "" {
		corpo := map[string]string{"erro": res.Erro}
		if res.Worktree != "" {
			corpo["worktree"] = res.Worktree
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(res.Codigo)
		json.NewEncoder(w).Encode(corpo)
		return
	}
	escreverJSON(w, map[string]any{"ok": true, "id": res.ID, "nome": res.Nome, "worktree": res.Worktree, "branch": res.Branch})
}

func (s *Servico) listarPapeis(w http.ResponseWriter, r *http.Request) {
	ps := papeis.Listar(r.URL.Query().Get("raiz"), s.op.ClaudeDir)
	if ps == nil {
		ps = []papeis.Papel{}
	}
	escreverJSON(w, ps)
}

func (s *Servico) terminarSessao(w http.ResponseWriter, r *http.Request) {
	if !s.terminarHeadless(r.PathValue("id")) {
		http.Error(w, `{"erro":"esta sessão não foi aberta pelo Towerman ou já terminou"}`, http.StatusConflict)
		return
	}
	escreverJSON(w, map[string]bool{"ok": true})
}

// promover termina o processo headless e retoma a mesma sessão num terminal: no IDE que tem a pasta
// aberta (pela extensão) ou, sem ela, num emulador de terminal do sistema.
func (s *Servico) promover(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	h := s.obterHeadless(id)
	if h == nil {
		http.Error(w, `{"erro":"esta sessão não está a correr pelo Towerman"}`, http.StatusConflict)
		return
	}
	if h.ag.ID() != "claude" {
		http.Error(w, `{"erro":"só as sessões do Claude Code passam a terminal"}`, http.StatusConflict)
		return
	}
	cwd := h.cwd
	s.terminarHeadless(id)
	claude, err := localizarClaude()
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"erro":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}
	s.enfileirarSimples(func() { s.quadro.MarcarHeadless(id, "", cwd, "", 0, false, time.Now()) })
	comando := "claude --resume " + id
	if s.ext.ligadas() > 0 {
		if resp, ok := s.ext.pedirAbrir(cwd, comando, esperaExtensao); ok {
			escreverJSON(w, map[string]any{"ok": true, "como": "terminal do IDE (" + resp.Workspace + ")"})
			return
		}
	}
	for _, t := range [][]string{
		{"xfce4-terminal", "--working-directory", cwd, "-x", claude, "--resume", id},
		{"x-terminal-emulator", "-e", claude, "--resume", id},
	} {
		if _, err := exec.LookPath(t[0]); err != nil {
			continue
		}
		c := exec.Command(t[0], t[1:]...)
		c.Dir = cwd
		c.Env = ambienteFilho()
		if c.Start() == nil {
			go c.Wait()
			escreverJSON(w, map[string]any{"ok": true, "como": t[0]})
			return
		}
	}
	escreverJSON(w, map[string]any{"ok": false, "erro": "abre um terminal em " + cwd + " e corre: " + comando})
}
