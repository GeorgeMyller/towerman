//go:build windows

package processo

import (
	"os"
	"os/exec"
	"testing"
)

func TestPaiENomeWindows(t *testing.T) {
	if n := nome(os.Getpid()); n == "" {
		t.Fatal("nome do próprio teste vazio")
	}
	// «cmd /c» com dois comandos não se troca pelo filho: o pai do cmd é este teste.
	cmd := exec.Command("cmd", "/c", "ping -n 3 127.0.0.1 >NUL & ver >NUL")
	if err := cmd.Start(); err != nil {
		t.Skip("sem cmd:", err)
	}
	defer cmd.Process.Kill()
	if p := pai(cmd.Process.Pid); p != os.Getpid() {
		t.Fatalf("pai(cmd) = %d, esperava %d", p, os.Getpid())
	}
	if n := nome(cmd.Process.Pid); n != "cmd" {
		t.Fatalf("nome(cmd) = %q, esperava \"cmd\"", n)
	}
	if p := SemShell(cmd.Process.Pid); p != os.Getpid() {
		t.Fatalf("SemShell(cmd) = %d, esperava o teste %d", p, os.Getpid())
	}
}
