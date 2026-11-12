package papeis

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListar(t *testing.T) {
	repo, claude := t.TempDir(), t.TempDir()
	os.MkdirAll(filepath.Join(repo, ".claude", "agents"), 0o755)
	os.MkdirAll(filepath.Join(claude, "agents"), 0o755)
	os.WriteFile(filepath.Join(repo, ".claude", "agents", "revisor.md"), []byte("---\nname: revisor\ndescription: \"Revê PRs\"\ntools: Read, Grep, Bash\nmodel: opus\n---\n\nÉs um revisor.\n"), 0o644)
	os.WriteFile(filepath.Join(claude, "agents", "revisor.md"), []byte("---\nname: revisor\n---\noutro\n"), 0o644)
	os.WriteFile(filepath.Join(claude, "agents", "simples.md"), []byte("Sem frontmatter.\n"), 0o644)
	ps := Listar(repo, claude)
	if len(ps) != 2 {
		t.Fatalf("esperava 2 papéis, obtive %+v", ps)
	}
	r := ps[0]
	if r.Nome != "revisor" || r.Origem != "repo" || r.Descricao != "Revê PRs" || r.Modelo != "opus" || len(r.Ferramentas) != 3 || r.Prompt != "És um revisor." {
		t.Fatalf("papel do repo inesperado: %+v", r)
	}
	if ps[1].Nome != "simples" || ps[1].Prompt != "Sem frontmatter." {
		t.Fatalf("papel sem frontmatter inesperado: %+v", ps[1])
	}
}
