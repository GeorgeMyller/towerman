package extensao

import (
	"archive/zip"
	"encoding/json"
	"io"
	"testing"
)

func TestEmpacotar(t *testing.T) {
	p, err := Empacotar(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	z, err := zip.OpenReader(p)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	nomes := map[string]*zip.File{}
	for _, f := range z.File {
		nomes[f.Name] = f
	}
	for _, n := range []string{"[Content_Types].xml", "extension.vsixmanifest", "extension/package.json", "extension/extension.js", "extension/README.md"} {
		if nomes[n] == nil {
			t.Fatalf("falta %s no vsix", n)
		}
	}
	rc, _ := nomes["extension/package.json"].Open()
	b, _ := io.ReadAll(rc)
	var pkg struct{ Name, Publisher, Version, Main string }
	if err := json.Unmarshal(b, &pkg); err != nil || pkg.Publisher+"."+pkg.Name != ID || pkg.Version != Versao() || pkg.Main == "" {
		t.Fatalf("package.json inesperado: %+v %v", pkg, err)
	}
}
