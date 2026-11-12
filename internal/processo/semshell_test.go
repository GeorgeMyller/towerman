//go:build !windows

package processo

import (
	"os"
	"os/exec"
	"testing"
)

func TestSemShell(t *testing.T) {
	// Um processo que não é shell fica com o próprio pid.
	if p := SemShell(os.Getpid()); p != os.Getpid() {
		t.Fatalf("SemShell(teste) = %d, esperava %d", p, os.Getpid())
	}
	// Um «sh -c» (como o que corre um hook) sobe até ao pai, que é este teste. Dois comandos
	// impedem o sh de se trocar pelo sleep.
	cmd := exec.Command("sh", "-c", "sleep 2; true")
	if err := cmd.Start(); err != nil {
		t.Skip("sem sh:", err)
	}
	defer cmd.Process.Kill()
	if p := SemShell(cmd.Process.Pid); p != os.Getpid() {
		t.Fatalf("SemShell(sh) = %d (%s), esperava o teste %d", p, nome(p), os.Getpid())
	}
}
