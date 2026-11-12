//go:build !windows

package servico

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/GeorgeMyller/towerman/internal/hook"
	"github.com/GeorgeMyller/towerman/internal/quadro"
)

// pidMorto devolve o PID de um processo que já terminou.
func pidMorto(t *testing.T) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Run()
	return cmd.Process.Pid
}

// Para o Antigravity, a marca de presença decide a vida da sessão antes do PID guardado.
func TestVigiarAntigravityPelaMarca(t *testing.T) {
	presenca := filepath.Join(t.TempDir(), "presence")
	os.MkdirAll(presenca, 0o700)
	a := novoAmbiente(t, func(op *Opcoes) { op.PresencaAntigravity = presenca })
	morto := pidMorto(t)
	sessao := func(id string, pid int) {
		evAgente(t, a.srv.proc, "antigravity", hook.Terminal{}, pid, map[string]any{"session_id": id, "hook_event_name": "UserPromptSubmit", "prompt": "olá"})
	}

	// Conversa aberta: o agente segura o bloqueio, o processo guardado já morreu.
	sessao("viva", morto)
	os.WriteFile(filepath.Join(presenca, "viva.lock"), nil, 0o600)
	dono, _ := os.Open(filepath.Join(presenca, "viva.lock"))
	defer dono.Close()
	if err := unix.Flock(int(dono.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}

	// Conversa fechada: marca sem dono, com o IDE (PID) ainda vivo.
	sessao("fechada", os.Getpid())
	os.WriteFile(filepath.Join(presenca, "fechada.lock"), nil, 0o600)
	id, err := a.srv.Quadro().CriarMensagem("fechada", "continua", "stop", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	a.srv.vigiar()
	if s := a.sessao("viva"); s.Estado == quadro.Terminada {
		t.Fatal("com o bloqueio seguro, a sessão não termina mesmo com o PID morto")
	}
	if s := a.sessao("fechada"); s.Estado != quadro.Terminada {
		t.Fatalf("com a marca livre, a sessão termina mesmo com o PID vivo: %q", s.Estado)
	}
	if m, _ := a.srv.Quadro().ObterMensagem(id); m.Estado != quadro.MensagemFalhada {
		t.Fatalf("as mensagens pendentes da sessão terminada falham: %q", m.Estado)
	}
}

// Sem marca legível (pasta ausente), vale a regra do PID.
func TestVigiarAntigravitySemMarca(t *testing.T) {
	a := novoAmbiente(t, func(op *Opcoes) { op.PresencaAntigravity = filepath.Join(t.TempDir(), "nao-existe") })
	evAgente(t, a.srv.proc, "antigravity", hook.Terminal{}, pidMorto(t), map[string]any{"session_id": "morta", "hook_event_name": "UserPromptSubmit"})
	evAgente(t, a.srv.proc, "antigravity", hook.Terminal{}, os.Getpid(), map[string]any{"session_id": "viva", "hook_event_name": "UserPromptSubmit"})
	a.srv.vigiar()
	if s := a.sessao("morta"); s.Estado != quadro.Terminada {
		t.Fatalf("sem marca e com o PID morto, termina: %q", s.Estado)
	}
	if s := a.sessao("viva"); s.Estado == quadro.Terminada {
		t.Fatal("sem marca e com o PID vivo, continua")
	}
}
