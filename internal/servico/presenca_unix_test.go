//go:build !windows

package servico

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestPresencaViva(t *testing.T) {
	dir := t.TempDir()
	marca := filepath.Join(dir, "conversa.lock")
	os.WriteFile(marca, nil, 0o600)

	// O agente segura o bloqueio noutro descritor: a conversa está aberta.
	dono, err := os.Open(marca)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Flock(int(dono.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if e := presencaViva(dir, "conversa"); e != marcaViva {
		t.Fatalf("com o bloqueio seguro, devia estar viva: %v", e)
	}
	dono.Close()

	// Sem dono, a sonda obtém o bloqueio, diz fechada e solta-o logo.
	if e := presencaViva(dir, "conversa"); e != marcaFechada {
		t.Fatalf("sem dono, devia estar fechada: %v", e)
	}
	outro, _ := os.Open(marca)
	defer outro.Close()
	if err := unix.Flock(int(outro.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatalf("a sonda devia ter soltado o bloqueio: %v", err)
	}
	unix.Flock(int(outro.Fd()), unix.LOCK_UN)

	if e := presencaViva(dir, "outra"); e != marcaFechada {
		t.Fatalf("ficheiro ausente com a pasta presente devia ser fechada: %v", e)
	}
	if e := presencaViva(filepath.Join(dir, "nao-existe"), "conversa"); e != marcaDesconhecida {
		t.Fatalf("sem pasta, não se sabe: %v", e)
	}
	if _, err := os.Stat(filepath.Join(dir, "outra.lock")); !os.IsNotExist(err) {
		t.Fatal("a sonda nunca cria a marca")
	}
}
