package servico

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/GeorgeMyller/towerman/internal/visao"
)

func (a *ambiente) pedir(metodo, caminho string, ajustar func(*http.Request)) *http.Response {
	a.t.Helper()
	req, _ := http.NewRequest(metodo, a.url+caminho, nil)
	if ajustar != nil {
		ajustar(req)
	}
	cliente := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := cliente.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	return resp
}

func comCookie(token string) func(*http.Request) {
	return func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "towerman_token", Value: token}) }
}

func TestPainelAutenticacao(t *testing.T) {
	a := novoAmbiente(t, nil)

	r := a.pedir("GET", "/", nil)
	r.Body.Close()
	if r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("sem cookie esperava 401, obtive %d", r.StatusCode)
	}

	r = a.pedir("GET", "/?token="+a.token, nil)
	r.Body.Close()
	var cookie *http.Cookie
	for _, c := range r.Cookies() {
		if c.Name == "towerman_token" {
			cookie = c
		}
	}
	if r.StatusCode != http.StatusSeeOther || r.Header.Get("Location") != "/" || cookie == nil || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("login por token inesperado: %d %v %+v", r.StatusCode, r.Header, cookie)
	}

	r = a.pedir("GET", "/?token=errado", nil)
	r.Body.Close()
	if r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("token errado esperava 401, obtive %d", r.StatusCode)
	}

	r = a.pedir("GET", "/", comCookie(a.token))
	corpo := make([]byte, 64)
	n, _ := r.Body.Read(corpo)
	r.Body.Close()
	if r.StatusCode != http.StatusOK || !strings.Contains(string(corpo[:n]), "<!doctype html>") {
		t.Fatalf("página esperada com cookie: %d", r.StatusCode)
	}

	r = a.pedir("GET", "/v1/sessoes", func(q *http.Request) {
		comCookie(a.token)(q)
		q.Header.Set("Origin", "https://exemplo.com")
	})
	r.Body.Close()
	if r.StatusCode != http.StatusForbidden {
		t.Fatalf("outra origem esperava 403, obtive %d", r.StatusCode)
	}

	r = a.pedir("GET", "/v1/sessoes", func(q *http.Request) {
		comCookie(a.token)(q)
		q.Header.Set("Origin", fmt.Sprintf("http://127.0.0.1:%d", a.srv.Porta()))
	})
	r.Body.Close()
	if r.StatusCode != http.StatusOK {
		t.Fatalf("própria origem esperava 200, obtive %d", r.StatusCode)
	}

	r = a.pedir("GET", "/nada", comCookie(a.token))
	r.Body.Close()
	if r.StatusCode != http.StatusNotFound {
		t.Fatalf("rota desconhecida esperava 404, obtive %d", r.StatusCode)
	}
}

func TestPainelSessoesEConversa(t *testing.T) {
	a := novoAmbiente(t, nil)
	transcricao := filepath.Join(t.TempDir(), "s.jsonl")
	os.WriteFile(transcricao, []byte(`{"type":"user","timestamp":"t1","message":{"content":"olá painel"}}`+"\n"), 0o600)
	a.enviar(map[string]any{"session_id": "p1", "hook_event_name": "SessionStart", "cwd": t.TempDir(), "transcript_path": transcricao})
	a.enviar(map[string]any{"session_id": "p2", "hook_event_name": "SessionEnd", "cwd": t.TempDir()})
	a.esperar()

	ler := func(caminho string, v any) {
		t.Helper()
		r := a.pedir("GET", caminho, comCookie(a.token))
		defer r.Body.Close()
		if r.StatusCode != http.StatusOK {
			t.Fatalf("%s: %d", caminho, r.StatusCode)
		}
		json.NewDecoder(r.Body).Decode(v)
	}
	contar := func(repos []visao.Repo) int {
		n := 0
		for _, r := range repos {
			for _, w := range r.Worktrees {
				n += len(w.Sessoes)
			}
		}
		return n
	}
	var vivas, todas []visao.Repo
	ler("/v1/sessoes", &vivas)
	ler("/v1/sessoes?todas=1", &todas)
	if contar(vivas) != 1 || contar(todas) != 2 {
		t.Fatalf("contagens inesperadas: vivas %d, todas %d", contar(vivas), contar(todas))
	}

	var conversa struct {
		Disponivel bool `json:"disponivel"`
		Mensagens  []struct{ Papel, Texto string }
	}
	ler("/v1/sessoes/p1/conversa", &conversa)
	if !conversa.Disponivel || len(conversa.Mensagens) != 1 || conversa.Mensagens[0].Texto != "olá painel" {
		t.Fatalf("conversa inesperada: %+v", conversa)
	}
	ler("/v1/sessoes/p2/conversa", &conversa)
	if conversa.Disponivel {
		t.Fatal("sessão sem transcrição devia vir indisponível")
	}
	r := a.pedir("GET", "/v1/sessoes/nao-existe/conversa", comCookie(a.token))
	r.Body.Close()
	if r.StatusCode != http.StatusNotFound {
		t.Fatalf("sessão desconhecida esperava 404, obtive %d", r.StatusCode)
	}
}

func TestPainelAoVivo(t *testing.T) {
	a := novoAmbiente(t, nil)
	ctx, cancelar := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelar()
	req, _ := http.NewRequestWithContext(ctx, "GET", a.url+"/v1/ao-vivo", nil)
	req.Header.Set("Authorization", "Bearer "+a.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type inesperado: %q", ct)
	}
	linhas := bufio.NewScanner(resp.Body)
	linhas.Scan()
	a.enviar(map[string]any{"session_id": "v1", "hook_event_name": "Notification"})
	for linhas.Scan() {
		if linhas.Text() == "event: mudou" {
			return
		}
	}
	t.Fatalf("não recebi «mudou»: %v", linhas.Err())
}

func TestPaginaTorre(t *testing.T) {
	a := novoAmbiente(t, nil)
	r := a.pedir("GET", "/torre", nil)
	r.Body.Close()
	if r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("torre sem cookie esperava 401, obtive %d", r.StatusCode)
	}
	r = a.pedir("GET", "/torre?token="+a.token, nil)
	r.Body.Close()
	if r.StatusCode != http.StatusSeeOther || r.Header.Get("Location") != "/torre" {
		t.Fatalf("login na torre devia voltar à torre: %d %q", r.StatusCode, r.Header.Get("Location"))
	}
	r = a.pedir("GET", "/torre", comCookie(a.token))
	corpo := make([]byte, 400)
	n, _ := io.ReadFull(r.Body, corpo)
	r.Body.Close()
	if r.StatusCode != http.StatusOK || !strings.Contains(string(corpo[:n]), "Towerman · Torre") {
		t.Fatalf("página da torre inesperada: %d", r.StatusCode)
	}
}
