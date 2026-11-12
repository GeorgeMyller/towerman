package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func compilar(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "towerman")
	if os.PathSeparator == '\\' {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}

func portaLivre(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func correrHook(t *testing.T, bin string, env []string, evento string) (string, int) {
	t.Helper()
	cmd := exec.Command(bin, "hook")
	cmd.Env = env
	cmd.Stdin = strings.NewReader(evento)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	codigo := 0
	if e, ok := err.(*exec.ExitError); ok {
		codigo = e.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return out.String(), codigo
}

func lerEstado(porta int, token string) (map[string]any, error) {
	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/estado", porta), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var e map[string]any
	return e, json.NewDecoder(resp.Body).Decode(&e)
}

func pastaPessoal() string {
	casa, _ := os.UserHomeDir()
	return casa
}

// marcaFicheiro resume o conteúdo e a data de alteração de um ficheiro; vazio quando não existe.
func marcaFicheiro(t *testing.T, p string) string {
	t.Helper()
	info, err := os.Stat(p)
	if err != nil {
		return ""
	}
	b, _ := os.ReadFile(p)
	return fmt.Sprintf("%x %s", sha256.Sum256(b), info.ModTime().Format(time.RFC3339Nano))
}

func TestPontaAPonta(t *testing.T) {
	if testing.Short() {
		t.Skip("compila o binário")
	}
	bin := compilar(t)
	home := t.TempDir()
	porta := portaLivre(t)
	// O install e o uninstall mexem em ~/.gemini e ~/.codex: a pasta pessoal do teste é temporária.
	casa := t.TempDir()
	geminiReal := marcaFicheiro(t, filepath.Join(pastaPessoal(), ".gemini", "config", "hooks.json"))
	t.Cleanup(func() {
		if depois := marcaFicheiro(t, filepath.Join(pastaPessoal(), ".gemini", "config", "hooks.json")); depois != geminiReal {
			t.Errorf("o teste mexeu no ~/.gemini/config/hooks.json real: antes %q, depois %q", geminiReal, depois)
		}
	})
	env := append(os.Environ(), "HOME="+casa, "USERPROFILE="+casa, "TOWERMAN_HOME="+home, "TOWERMAN_PORTA="+strconv.Itoa(porta),
		"TOWERMAN_CLAUDE_DIR="+t.TempDir(), "TOWERMAN_TIMEOUT_MS=200", "TOWERMAN=")

	settings := filepath.Join(t.TempDir(), "settings.json")
	original := `{"model": "opus"}` + "\n"
	os.WriteFile(settings, []byte(original), 0o600)
	inst := exec.Command(bin, "install", "--settings", settings, "--sem-servico", "--sem-extensao")
	inst.Env = env
	if out, err := inst.CombinedOutput(); err != nil || !strings.Contains(string(out), "8 eventos acrescentados") {
		t.Fatalf("install: %v\n%s", err, out)
	}

	evento := func(i int) string {
		return fmt.Sprintf(`{"session_id":"e2e","hook_event_name":"Stop","cwd":%q,"i":%d}`, home, i)
	}
	for i := 0; i < 5; i++ {
		if out, codigo := correrHook(t, bin, env, evento(i)); codigo != 0 || out != "" {
			t.Fatalf("hook sem serviço: código %d, stdout %q", codigo, out)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(home, "diario.jsonl")); bytes.Count(b, []byte("\n")) != 5 {
		t.Fatalf("diário devia ter 5 linhas:\n%s", b)
	}
	if out, codigo := correrHook(t, bin, env, "lixo"); codigo != 0 || out != "" {
		t.Fatal("stdin inválido devia sair com 0 e stdout vazio")
	}

	serve := exec.Command(bin, "serve")
	serve.Env = env
	logServe := filepath.Join(t.TempDir(), "serve.log")
	if f, err := os.Create(logServe); err == nil {
		serve.Stderr = f
		defer f.Close()
	}
	if err := serve.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		serve.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() { serve.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			serve.Process.Kill()
		}
	})
	token, _ := os.ReadFile(filepath.Join(home, "token"))
	tok := strings.TrimSpace(string(token))

	// No runner do Pi o arranque passa dos 5 s; o log do serve diz porquê, se nem assim arrancar.
	prazo := time.Now().Add(30 * time.Second)
	for {
		if _, err := lerEstado(porta, tok); err == nil {
			break
		}
		if time.Now().After(prazo) {
			b, _ := os.ReadFile(logServe)
			t.Fatalf("o serviço não arrancou; log do serve:\n%s", b)
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Os 200 ms servem para a fase sem serviço. Com o serviço ligado e a máquina carregada (vários
	// jobs do CI ao mesmo tempo), um hook que passe do prazo cai também no diário, que só volta a
	// ser lido daqui a 30 s.
	envServico := append(append([]string(nil), env...), "TOWERMAN_TIMEOUT_MS=5000")
	for i := 5; i < 10; i++ {
		if _, codigo := correrHook(t, bin, envServico, evento(i)); codigo != 0 {
			t.Fatalf("hook com serviço: código %d", codigo)
		}
	}

	prazo = time.Now().Add(10 * time.Second)
	for {
		e, err := lerEstado(porta, tok)
		if err == nil && e["eventos"].(float64) == 10 && e["diario_pendente"].(float64) == 0 {
			break
		}
		if time.Now().After(prazo) {
			registo, _ := os.ReadFile(logServe)
			t.Fatalf("estado final inesperado: %v %v\nserve: %s", e, err, registo)
		}
		time.Sleep(100 * time.Millisecond)
	}

	des := exec.Command(bin, "uninstall", "--settings", settings, "--sem-servico", "--sem-extensao")
	des.Env = env
	if out, err := des.CombinedOutput(); err != nil {
		t.Fatalf("uninstall: %v\n%s", err, out)
	}
	var antes, depois any
	json.Unmarshal([]byte(original), &antes)
	b, _ := os.ReadFile(settings)
	json.Unmarshal(b, &depois)
	if fmt.Sprint(antes) != fmt.Sprint(depois) {
		t.Fatalf("uninstall não repôs o valor do settings.json:\n%s", b)
	}
}

func TestComandoDesconhecido(t *testing.T) {
	if testing.Short() {
		t.Skip("compila o binário")
	}
	cmd := exec.Command(compilar(t), "xyz")
	err := cmd.Run()
	if e, ok := err.(*exec.ExitError); !ok || e.ExitCode() != 2 {
		t.Fatalf("esperava código 2, obtive %v", err)
	}
}
