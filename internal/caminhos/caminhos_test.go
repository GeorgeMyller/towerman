package caminhos

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizar(t *testing.T) {
	casos := []struct {
		goos, caminho, worktree, cwd, esperado string
	}{
		{"linux", "/repo/api/x.go", "/repo", "/repo", "api/x.go"},
		{"linux", "api/../web/y.ts", "/repo", "/repo/sub", "sub/web/y.ts"},
		{"linux", "/repo", "/repo", "/repo", "."},
		{"linux", "/fora/z.go", "/repo", "/repo", "/fora/z.go"},
		{"linux", "/repo2/a.go", "/repo", "/repo", "/repo2/a.go"},
		{"linux", "/Repo/A.go", "/Repo", "", "A.go"},
		{"linux", "/x/a.go", "", "", "/x/a.go"},
		{"windows", `C:\Repo\A.go`, `C:\Repo`, `C:\Repo`, "a.go"},
		{"windows", `src\B.go`, `C:\Repo`, `C:\Repo`, "src/b.go"},
		{"darwin", "/Users/G/Repo/X.go", "/Users/G/Repo", "", "x.go"},
		{"linux", "", "/repo", "/repo", ""},
	}
	for _, c := range casos {
		if r := normalizarPara(c.goos, c.caminho, c.worktree, c.cwd); r != c.esperado {
			t.Errorf("%s %q (wt %q, cwd %q) = %q, esperado %q", c.goos, c.caminho, c.worktree, c.cwd, r, c.esperado)
		}
	}
}

func TestRealResolveLinks(t *testing.T) {
	base := t.TempDir()
	alvo := filepath.Join(base, "real")
	os.MkdirAll(filepath.Join(alvo, "api"), 0o755)
	os.WriteFile(filepath.Join(alvo, "api", "x.go"), nil, 0o644)
	link := filepath.Join(base, "link")
	if err := os.Symlink(alvo, link); err != nil {
		t.Skip("sem suporte a links simbólicos:", err)
	}
	esperado, _ := filepath.EvalSymlinks(filepath.Join(alvo, "api", "x.go"))
	if r := Real(filepath.Join(link, "api", "x.go"), ""); r != esperado {
		t.Fatalf("ficheiro existente: %q, esperado %q", r, esperado)
	}
	if r := Real(filepath.Join("api", "novo.go"), link); r != filepath.Join(filepath.Dir(esperado), "novo.go") {
		t.Fatalf("ficheiro inexistente e relativo: %q", r)
	}
}
