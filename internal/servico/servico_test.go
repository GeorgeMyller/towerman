package servico

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/GeorgeMyller/towerman/internal/hook"
	"github.com/GeorgeMyller/towerman/internal/quadro"
)

type ambiente struct {
	t      *testing.T
	home   string
	claude string
	srv    *Servico
	url    string
	token  string
	feito  chan error
	parar  context.CancelFunc
}

func novoAmbiente(t *testing.T, ajustar func(*Opcoes)) *ambiente {
	t.Helper()
	a := &ambiente{t: t, home: t.TempDir(), claude: t.TempDir(), token: "segredo"}
	os.MkdirAll(filepath.Join(a.claude, "sessions"), 0o700)
	// Sem marcas de presença do Antigravity (nunca as do utilizador): vale a regra do PID.
	op := Opcoes{Porta: 0, Token: a.token, Home: a.home, ClaudeDir: a.claude, EsperaDiario: time.Millisecond,
		PresencaAntigravity: filepath.Join(a.home, "sem-presenca")}
	if ajustar != nil {
		ajustar(&op)
	}
	srv, err := Novo(op)
	if err != nil {
		t.Fatal(err)
	}
	a.srv = srv
	ctx, cancel := context.WithCancel(context.Background())
	a.parar = cancel
	a.feito = make(chan error, 1)
	pronto := make(chan int, 1)
	go func() { a.feito <- srv.Executar(ctx, func(p int) { pronto <- p }) }()
	select {
	case p := <-pronto:
		a.url = fmt.Sprintf("http://127.0.0.1:%d", p)
	case err := <-a.feito:
		t.Fatal(err)
	}
	t.Cleanup(a.fechar)
	return a
}

func (a *ambiente) fechar() {
	if a.parar != nil {
		a.parar()
		<-a.feito
		a.parar = nil
	}
}

func envelope(evento map[string]any, recebido string) []byte {
	ev, _ := json.Marshal(evento)
	b, _ := json.Marshal(hook.Envelope{Agente: "claude", RecebidoEm: recebido, Ppid: os.Getpid(), Evento: ev,
		Terminal: hook.Terminal{TermProgram: "vscode"}})
	return b
}

var contador int

func (a *ambiente) enviar(evento map[string]any) int {
	a.t.Helper()
	contador++
	return a.enviarBruto(envelope(evento, fmt.Sprintf("2026-10-07T10:00:00.%09dZ", contador)), a.token)
}

func (a *ambiente) enviarBruto(corpo []byte, token string) int {
	a.t.Helper()
	req, _ := http.NewRequest(http.MethodPost, a.url+"/v1/eventos", bytes.NewReader(corpo))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func (a *ambiente) estado() Estado {
	a.t.Helper()
	req, _ := http.NewRequest(http.MethodGet, a.url+"/estado", nil)
	req.Header.Set("Authorization", "Bearer "+a.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer resp.Body.Close()
	var e Estado
	json.NewDecoder(resp.Body).Decode(&e)
	return e
}

// esperar aguarda que a fila processe tudo o que foi aceite até aqui.
func (a *ambiente) esperar() {
	a.t.Helper()
	feito := make(chan struct{})
	a.srv.fila <- func() { close(feito) }
	select {
	case <-feito:
	case <-time.After(5 * time.Second):
		a.t.Fatal("fila não esvaziou")
	}
}

func (a *ambiente) sessao(id string) quadro.Sessao {
	a.t.Helper()
	s, ok, err := a.srv.Quadro().ObterSessao(id)
	if err != nil || !ok {
		a.t.Fatalf("sessão %s ausente: %v", id, err)
	}
	return s
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func novoRepo(t *testing.T) (repo, worktree string) {
	t.Helper()
	repo = filepath.Join(t.TempDir(), "repo")
	os.MkdirAll(repo, 0o755)
	git(t, repo, "init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a"), 0o644)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "i")
	worktree = filepath.Join(filepath.Dir(repo), "wt")
	git(t, repo, "worktree", "add", "-q", "-b", "feat/x", worktree)
	return repo, worktree
}

func real(t *testing.T, p string) string {
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestFluxoDeUmaSessao(t *testing.T) {
	repo, wt := novoRepo(t)
	a := novoAmbiente(t, nil)
	reg, _ := json.Marshal(map[string]any{"pid": os.Getpid(), "sessionId": "s1", "cwd": wt, "name": "yourlex-redux-fd"})
	os.WriteFile(filepath.Join(a.claude, "sessions", fmt.Sprint(os.Getpid())+".json"), reg, 0o600)

	if c := a.enviar(map[string]any{"session_id": "s1", "hook_event_name": "SessionStart", "cwd": wt, "transcript_path": "/t.jsonl"}); c != http.StatusAccepted {
		t.Fatalf("código %d", c)
	}
	a.enviar(map[string]any{"session_id": "s1", "hook_event_name": "UserPromptSubmit", "cwd": wt, "prompt": "\n  corrige o bug X  \nmais detalhe"})
	a.enviar(map[string]any{"session_id": "s1", "hook_event_name": "PostToolUse", "cwd": wt, "tool_name": "Edit",
		"tool_input": map[string]any{"file_path": filepath.Join(wt, "api", "x.go")}})
	a.esperar()

	s := a.sessao("s1")
	if s.Repo != real(t, filepath.Join(repo, ".git")) || s.Worktree != real(t, wt) || s.Branch != "feat/x" {
		t.Fatalf("git mal resolvido: repo=%q wt=%q branch=%q", s.Repo, s.Worktree, s.Branch)
	}
	if s.Pid != os.Getpid() || s.Nome != "yourlex-redux-fd" {
		t.Fatalf("identificação errada: pid=%d nome=%q", s.Pid, s.Nome)
	}
	if s.Resumo != "corrige o bug X" || s.Estado != quadro.Ativa || !strings.Contains(s.Terminal, "vscode") {
		t.Fatalf("sessão inesperada: %+v", s)
	}
	toques, _ := a.srv.Quadro().Toques("s1")
	if len(toques) != 1 || toques[0].Caminho != "api/x.go" || toques[0].Tipo != "edit" || toques[0].Branch != "feat/x" {
		t.Fatalf("toques inesperados: %+v", toques)
	}

	a.enviar(map[string]any{"session_id": "s1", "hook_event_name": "UserPromptSubmit", "cwd": wt, "prompt": "<task-notification>\n<status>completed</status>"})
	a.esperar()
	if r := a.sessao("s1").Resumo; r != "corrige o bug X" {
		t.Fatalf("notificação não devia substituir o resumo: %q", r)
	}
	a.enviar(map[string]any{"session_id": "s1", "hook_event_name": "Notification", "cwd": wt})
	a.esperar()
	if a.sessao("s1").Estado != quadro.AEspera {
		t.Fatal("esperava a_espera")
	}
	a.enviar(map[string]any{"session_id": "s1", "hook_event_name": "Stop", "cwd": wt})
	a.esperar()
	if a.sessao("s1").Estado != quadro.Parada {
		t.Fatal("esperava parada")
	}
	a.enviar(map[string]any{"session_id": "s1", "hook_event_name": "SessionEnd", "cwd": wt})
	a.esperar()
	if s := a.sessao("s1"); s.Estado != quadro.Terminada || s.TerminadaEm == "" {
		t.Fatal("esperava terminada")
	}
}

func TestRepoPartilhadoEntreWorktreesEGit(t *testing.T) {
	repo, wt := novoRepo(t)
	a := novoAmbiente(t, nil)
	a.enviar(map[string]any{"session_id": "p", "hook_event_name": "SessionStart", "cwd": repo})
	a.enviar(map[string]any{"session_id": "w", "hook_event_name": "SessionStart", "cwd": wt})
	a.esperar()
	if a.sessao("p").Repo != a.sessao("w").Repo {
		t.Fatal("worktrees do mesmo repo deviam partilhar identidade")
	}

	git(t, wt, "checkout", "-q", "-b", "feat/y")
	a.enviar(map[string]any{"session_id": "w", "hook_event_name": "PostToolUse", "cwd": wt, "tool_name": "Bash",
		"tool_input": map[string]any{"command": "git checkout -b feat/y && git commit -m z"}})
	a.esperar()
	if b := a.sessao("w").Branch; b != "feat/y" {
		t.Fatalf("branch devia ser feat/y, é %q", b)
	}
	toques, _ := a.srv.Quadro().Toques("w")
	if len(toques) != 2 || toques[0].Caminho != "checkout" || toques[1].Caminho != "commit" || toques[0].Tipo != "git" {
		t.Fatalf("toques git inesperados: %+v", toques)
	}

	a.enviar(map[string]any{"session_id": "w", "hook_event_name": "PostToolUse", "cwd": wt, "tool_name": "Bash",
		"tool_input": map[string]any{"command": "ls -la"}})
	a.esperar()
	if toques, _ := a.srv.Quadro().Toques("w"); len(toques) != 2 {
		t.Fatal("bash sem git não devia criar toques")
	}
}

func TestForaDeGit(t *testing.T) {
	a := novoAmbiente(t, nil)
	dir := t.TempDir()
	a.enviar(map[string]any{"session_id": "f", "hook_event_name": "PostToolUse", "cwd": dir, "tool_name": "Write",
		"tool_input": map[string]any{"file_path": filepath.Join(dir, "n.md")}})
	a.esperar()
	s := a.sessao("f")
	if s.Repo != "" || s.Branch != "" || s.Pid != os.Getpid() {
		t.Fatalf("sessão fora de git inesperada: %+v", s)
	}
	toques, _ := a.srv.Quadro().Toques("f")
	if len(toques) != 1 || toques[0].Tipo != "write" || !strings.HasSuffix(toques[0].Caminho, "/n.md") {
		t.Fatalf("toque inesperado: %+v", toques)
	}
}

func TestAutenticacaoEValidacao(t *testing.T) {
	a := novoAmbiente(t, nil)
	corpo := envelope(map[string]any{"session_id": "a", "hook_event_name": "Stop"}, "2026-10-07T10:00:00Z")
	if c := a.enviarBruto(corpo, ""); c != http.StatusUnauthorized {
		t.Fatalf("sem token devia dar 401, deu %d", c)
	}
	if c := a.enviarBruto(corpo, "errado"); c != http.StatusUnauthorized {
		t.Fatalf("token errado devia dar 401, deu %d", c)
	}
	if c := a.enviarBruto([]byte("{"), a.token); c != http.StatusBadRequest {
		t.Fatalf("JSON inválido devia dar 400, deu %d", c)
	}
	if c := a.enviarBruto(envelope(map[string]any{"session_id": "a"}, "x"), a.token); c != http.StatusBadRequest {
		t.Fatalf("evento incompleto devia dar 400, deu %d", c)
	}
	grande := bytes.Repeat([]byte("x"), hook.TamanhoMaximo+128*1024)
	if c := a.enviarBruto(grande, a.token); c != http.StatusRequestEntityTooLarge {
		t.Fatalf("corpo grande devia dar 413, deu %d", c)
	}
	a.esperar()
	if e := a.estado(); e.Eventos != 0 {
		t.Fatalf("nada devia ter sido gravado: %+v", e)
	}
}

func TestDeduplicacao(t *testing.T) {
	a := novoAmbiente(t, nil)
	corpo := envelope(map[string]any{"session_id": "d", "hook_event_name": "Stop"}, "2026-10-07T10:00:00.1Z")
	a.enviarBruto(corpo, a.token)
	a.enviarBruto(corpo, a.token)
	a.esperar()
	if e := a.estado(); e.Eventos != 1 {
		t.Fatalf("esperava 1 evento, há %d", e.Eventos)
	}
}

func TestIngestaoDoDiario(t *testing.T) {
	home := t.TempDir()
	var linhas bytes.Buffer
	for i := 0; i < 1000; i++ {
		linhas.Write(envelope(map[string]any{"session_id": fmt.Sprintf("s%d", i%7), "hook_event_name": "Stop"},
			fmt.Sprintf("2026-10-07T10:00:00.%09dZ", i)))
		linhas.WriteByte('\n')
	}
	linhas.WriteString("isto não é json\n")
	os.WriteFile(filepath.Join(home, "diario.jsonl"), linhas.Bytes(), 0o600)

	a := novoAmbiente(t, func(op *Opcoes) { op.Home = home })
	// No Mac demora 0,15 s; no runner do Pi, com o SQLite no cartão SD, passa dos 10 s.
	prazo := time.Now().Add(60 * time.Second)
	for {
		e := a.estado()
		if e.Eventos == 1000 && !e.IngerindoDiario {
			break
		}
		if time.Now().After(prazo) {
			t.Fatalf("ingestão incompleta: %+v", e)
		}
		time.Sleep(20 * time.Millisecond)
	}
	a.esperar()
	restos, _ := filepath.Glob(filepath.Join(home, "diario.*jsonl"))
	if len(restos) != 1 || filepath.Base(restos[0]) != "diario.rejeitado.jsonl" {
		t.Fatalf("ficheiros de diário inesperados: %v", restos)
	}
	if b, _ := os.ReadFile(restos[0]); !strings.Contains(string(b), "isto não é json") {
		t.Fatal("linha inválida não foi para o rejeitado")
	}
	if e := a.estado(); e.Sessoes[quadro.Parada] != 7 {
		t.Fatalf("esperava 7 sessões paradas: %+v", e.Sessoes)
	}
}

func TestVigiaTerminaSessoesMortas(t *testing.T) {
	a := novoAmbiente(t, func(op *Opcoes) { op.IntervaloVigia = 20 * time.Millisecond })
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Run()
	morto := cmd.Process.Pid
	reg, _ := json.Marshal(map[string]any{"pid": morto, "sessionId": "m"})
	os.WriteFile(filepath.Join(a.claude, "sessions", fmt.Sprint(morto)+".json"), reg, 0o600)
	a.enviar(map[string]any{"session_id": "m", "hook_event_name": "SessionStart", "cwd": t.TempDir()})
	a.enviar(map[string]any{"session_id": "v", "hook_event_name": "SessionStart", "cwd": t.TempDir()})
	a.esperar()
	prazo := time.Now().Add(5 * time.Second)
	for a.sessao("m").Estado != quadro.Terminada {
		if time.Now().After(prazo) {
			t.Fatal("sessão com pid morto não foi terminada")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if a.sessao("v").Estado == quadro.Terminada {
		t.Fatal("sessão viva não devia ser terminada")
	}
}

func TestRetencao(t *testing.T) {
	a := novoAmbiente(t, nil)
	a.enviar(map[string]any{"session_id": "r", "hook_event_name": "SessionEnd"})
	a.esperar()
	q := a.srv.Quadro()
	q.Reter(time.Now().Add(time.Hour))
	if c, _ := q.Contar(); c.Eventos != 0 || c.Sessoes[quadro.Terminada] != 0 {
		t.Fatalf("retenção não apagou: %+v", c)
	}
}

func TestPortaOcupada(t *testing.T) {
	a := novoAmbiente(t, nil)
	srv, err := Novo(Opcoes{Porta: a.srv.Porta(), Token: "x", Home: t.TempDir(), ClaudeDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Executar(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "ocupada") {
		t.Fatalf("esperava erro de porta ocupada, obtive %v", err)
	}
}
