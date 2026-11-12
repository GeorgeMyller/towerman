package processo

import (
	"os"
	"os/exec"
	"runtime"
	"testing"
)

func TestVivo(t *testing.T) {
	if !Vivo(os.Getpid()) {
		t.Fatal("o próprio processo devia estar vivo")
	}
	if Vivo(0) || Vivo(-1) {
		t.Fatal("pids inválidos não estão vivos")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", "exit")
	}
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if Vivo(cmd.Process.Pid) {
		t.Fatal("processo terminado não devia estar vivo")
	}
}

func TestAncestrais(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sem árvore de processos no Windows")
	}
	a := Ancestrais(os.Getpid())
	if len(a) < 2 || a[0] != os.Getpid() || a[1] != os.Getppid() {
		t.Fatalf("ancestrais inesperados: %v (ppid %d)", a, os.Getppid())
	}
}
