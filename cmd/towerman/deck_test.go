package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNavegadoresWindows(t *testing.T) {
	env := map[string]string{
		"ProgramFiles(x86)": `C:\Program Files (x86)`,
		"ProgramFiles":      `C:\Program Files`,
		"LOCALAPPDATA":      `C:\Users\g\AppData\Local`,
	}
	lista := navegadoresWindows(func(k string) string { return env[k] })
	if len(lista) != 5 {
		t.Fatalf("esperava 5 caminhos, veio %d: %v", len(lista), lista)
	}
	if !strings.HasSuffix(lista[0], filepath.Join("Microsoft", "Edge", "Application", "msedge.exe")) ||
		!strings.HasPrefix(lista[0], `C:\Program Files (x86)`) {
		t.Fatalf("o primeiro devia ser o Edge de Program Files (x86): %q", lista[0])
	}
	if vazio := navegadoresWindows(func(string) string { return "" }); len(vazio) != 0 {
		t.Fatalf("sem variáveis, esperava lista vazia: %v", vazio)
	}
}
