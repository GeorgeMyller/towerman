package hook

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

const eventoValido = `{"session_id":"s1","hook_event_name":"PostToolUse","cwd":"/r","tool_name":"Edit","tool_input":{"file_path":"/r/a.go"}}`

func prepararHome(t *testing.T, token string) string {
	t.Helper()
	h := t.TempDir()
	t.Setenv("TOWERMAN_HOME", h)
	t.Setenv("TOWERMAN", "")
	if token != "" {
		os.WriteFile(filepath.Join(h, "token"), []byte(token+"\n"), 0o600)
	}
	return h
}

func usarServidor(t *testing.T, h http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	_, porta, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	t.Setenv("TOWERMAN_PORTA", porta)
}

func linhasDiario(t *testing.T, home string) []string {
	t.Helper()
	f, err := os.Open(filepath.Join(home, "diario.jsonl"))
	if err != nil {
		return nil
	}
	defer f.Close()
	var l []string
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 0, 64*1024), 2<<20)
	for s.Scan() {
		l = append(l, s.Text())
	}
	return l
}

func TestEnviaComToken(t *testing.T) {
	prepararHome(t, "abc")
	var recebido Envelope
	var auth string
	usarServidor(t, func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &recebido)
		w.WriteHeader(http.StatusAccepted)
	})
	t.Setenv("TOWERMAN_TIMEOUT_MS", "500")
	if r := Executar(strings.NewReader(eventoValido), io.Discard); r != Enviado {
		t.Fatalf("esperava Enviado, obtive %v", r)
	}
	if auth != "Bearer abc" {
		t.Fatalf("Authorization inesperado: %q", auth)
	}
	if string(recebido.Evento) != eventoValido || recebido.Agente != "claude" || recebido.RecebidoEm == "" {
		t.Fatalf("envelope inesperado: %+v", recebido)
	}
}

func TestServicoForaDoArVaiParaDiario(t *testing.T) {
	h := prepararHome(t, "abc")
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	porta := l.Addr().(*net.TCPAddr).Port
	l.Close()
	t.Setenv("TOWERMAN_PORTA", strconv.Itoa(porta))
	for i := 0; i < 3; i++ {
		if r := Executar(strings.NewReader(eventoValido), io.Discard); r != NoDiario {
			t.Fatalf("esperava NoDiario, obtive %v", r)
		}
	}
	linhas := linhasDiario(t, h)
	if len(linhas) != 3 {
		t.Fatalf("esperava 3 linhas no diário, há %d", len(linhas))
	}
	var e Envelope
	if err := json.Unmarshal([]byte(linhas[0]), &e); err != nil || string(e.Evento) != eventoValido {
		t.Fatalf("linha inválida: %s", linhas[0])
	}
	if st, _ := os.Stat(filepath.Join(h, "diario.jsonl")); runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
		t.Fatalf("permissões do diário: %v", st.Mode().Perm())
	}
}

func TestRespostasFalhadasVaoParaDiario(t *testing.T) {
	casos := map[string]http.HandlerFunc{
		"401": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) },
		"timeout": func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(100 * time.Millisecond)
			w.WriteHeader(http.StatusAccepted)
		},
	}
	for nome, h := range casos {
		t.Run(nome, func(t *testing.T) {
			home := prepararHome(t, "abc")
			usarServidor(t, h)
			t.Setenv("TOWERMAN_TIMEOUT_MS", "20")
			if r := Executar(strings.NewReader(eventoValido), io.Discard); r != NoDiario {
				t.Fatalf("esperava NoDiario, obtive %v", r)
			}
			if len(linhasDiario(t, home)) != 1 {
				t.Fatal("esperava 1 linha no diário")
			}
		})
	}
}

func TestSemTokenVaiParaDiario(t *testing.T) {
	h := prepararHome(t, "")
	if r := Executar(strings.NewReader(eventoValido), io.Discard); r != NoDiario {
		t.Fatalf("esperava NoDiario, obtive %v", r)
	}
	if len(linhasDiario(t, h)) != 1 {
		t.Fatal("esperava 1 linha no diário")
	}
}

func TestEntradasInvalidasSaoDescartadas(t *testing.T) {
	h := prepararHome(t, "abc")
	for _, s := range []string{"", "não é json", `{"session_id":"s"}`, `{"hook_event_name":"Stop"}`, `[1]`, strings.Repeat("x", TamanhoMaximo+1)} {
		if r := Executar(strings.NewReader(s), io.Discard); r != Descartado {
			t.Errorf("esperava Descartado para %.30q, obtive %v", s, r)
		}
	}
	if len(linhasDiario(t, h)) != 0 {
		t.Fatal("entradas inválidas não deviam ir para o diário")
	}
}

func TestDesligado(t *testing.T) {
	prepararHome(t, "abc")
	t.Setenv("TOWERMAN", "off")
	if r := Executar(strings.NewReader(eventoValido), io.Discard); r != Desligado {
		t.Fatalf("esperava Desligado, obtive %v", r)
	}
}

func TestEscreveASaidaDoServico(t *testing.T) {
	prepararHome(t, "abc")
	var consulta string
	usarServidor(t, func(w http.ResponseWriter, r *http.Request) {
		consulta = r.URL.RawQuery
		w.Write([]byte(`{"saida":{"decision":"block","reason":"olá"}}`))
	})
	var out strings.Builder
	stop := `{"session_id":"s1","hook_event_name":"Stop"}`
	if r := Executar(strings.NewReader(stop), &out); r != Enviado {
		t.Fatalf("esperava Enviado, obtive %v", r)
	}
	if consulta != "responder=1" || out.String() != `{"decision":"block","reason":"olá"}` {
		t.Fatalf("consulta %q, saída %q", consulta, out.String())
	}
}
