//go:build !windows

package instalar

// GarantirPathSistema e trocarCopiaAntiga só correm no Linux; os links simbólicos no Windows
// exigem privilégios.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGarantirPathEm(t *testing.T) {
	raiz, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(raiz, "go", "towerman")
	os.MkdirAll(filepath.Dir(exe), 0o755)
	os.WriteFile(exe, []byte("x"), 0o755)
	local, usr := filepath.Join(raiz, "local"), filepath.Join(raiz, "usr")
	os.MkdirAll(local, 0o755)
	os.MkdirAll(usr, 0o755)
	pastas := []string{local, usr}

	// Sem ligação: cria-a na primeira pasta.
	if msg := garantirPathEm(exe, pastas); !strings.HasPrefix(msg, "ligação") {
		t.Fatalf("esperava a ligação criada, veio %q", msg)
	}
	if alvo, _ := filepath.EvalSymlinks(filepath.Join(local, "towerman")); alvo != exe {
		t.Fatalf("ligação aponta para %q", alvo)
	}
	// Já certa: nada a dizer.
	if msg := garantirPathEm(exe, pastas); msg != "" {
		t.Fatalf("esperava silêncio, veio %q", msg)
	}

	// Outro towerman na primeira pasta: não o substitui, diz o comando.
	outro := filepath.Join(raiz, "outro")
	os.WriteFile(outro, []byte("y"), 0o755)
	os.Remove(filepath.Join(local, "towerman"))
	os.Symlink(outro, filepath.Join(local, "towerman"))
	if msg := garantirPathEm(exe, pastas); !strings.Contains(msg, "sudo ln -sf "+exe) {
		t.Fatalf("esperava o comando sudo, veio %q", msg)
	}

	// Sem permissão de escrita: diz o comando. O root escreve em qualquer pasta, por isso salta.
	if os.Geteuid() == 0 {
		return
	}
	os.Remove(filepath.Join(local, "towerman"))
	os.Chmod(local, 0o555)
	defer os.Chmod(local, 0o755)
	if msg := garantirPathEm(exe, pastas); !strings.Contains(msg, "sudo ln -sf") {
		t.Fatalf("esperava o comando sudo, veio %q", msg)
	}
}

func TestTrocarCopiaAntiga(t *testing.T) {
	raiz, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(raiz, "towerman-novo")
	os.WriteFile(exe, []byte("novo"), 0o755)
	copia := filepath.Join(raiz, "towerman")

	if msg := trocarCopiaAntiga(exe, copia); msg != "" {
		t.Fatalf("sem cópia, esperava silêncio, veio %q", msg)
	}
	os.WriteFile(copia, []byte("antigo"), 0o755)
	if msg := trocarCopiaAntiga(exe, copia); !strings.Contains(msg, "trocado") {
		t.Fatalf("esperava a troca, veio %q", msg)
	}
	if alvo, _ := filepath.EvalSymlinks(copia); alvo != exe {
		t.Fatalf("ligação aponta para %q", alvo)
	}
	if b, _ := os.ReadFile(copia + ".antigo"); string(b) != "antigo" {
		t.Fatalf("cópia antiga perdida: %q", b)
	}
	if msg := trocarCopiaAntiga(exe, copia); msg != "" {
		t.Fatalf("já ligada, esperava silêncio, veio %q", msg)
	}
}
