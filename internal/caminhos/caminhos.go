package caminhos

import (
	"path/filepath"
	"runtime"
	"strings"
)

// Normalizar devolve o caminho com separador "/" e relativo à worktree quando está dentro dela.
// Windows e macOS têm sistemas de ficheiros case-insensitive, por isso aí o resultado vem em minúsculas.
func Normalizar(caminho, worktree, cwd string) string {
	return normalizarPara(runtime.GOOS, caminho, worktree, cwd)
}

func normalizarPara(goos, caminho, worktree, cwd string) string {
	if caminho == "" {
		return ""
	}
	c := barras(caminho)
	if !absoluto(goos, c) && cwd != "" {
		c = barras(cwd) + "/" + c
	}
	c = limpar(c)
	if goos == "windows" || goos == "darwin" {
		c = strings.ToLower(c)
		worktree = strings.ToLower(worktree)
	}
	if worktree == "" {
		return c
	}
	w := limpar(barras(worktree))
	switch {
	case c == w:
		return "."
	case strings.HasPrefix(c, w+"/"):
		return c[len(w)+1:]
	}
	return c
}

func barras(c string) string { return strings.ReplaceAll(c, `\`, "/") }

func absoluto(goos, c string) bool {
	if strings.HasPrefix(c, "/") {
		return true
	}
	return goos == "windows" && len(c) >= 2 && c[1] == ':'
}

func limpar(c string) string {
	partes := []string{}
	for _, p := range strings.Split(c, "/") {
		switch p {
		case "", ".":
		case "..":
			if len(partes) > 0 {
				partes = partes[:len(partes)-1]
			}
		default:
			partes = append(partes, p)
		}
	}
	r := strings.Join(partes, "/")
	if strings.HasPrefix(c, "/") {
		r = "/" + r
	}
	return r
}

// Real resolve links simbólicos e, no Windows, nomes curtos 8.3, para o caminho casar com a worktree
// que o git devolve já resolvida. Se o ficheiro já não existir, resolve a pasta mais próxima que exista.
func Real(caminho, cwd string) string {
	if caminho == "" {
		return ""
	}
	if !filepath.IsAbs(caminho) && cwd != "" {
		caminho = filepath.Join(cwd, caminho)
	}
	resto := ""
	atual := filepath.Clean(caminho)
	for {
		if r, err := filepath.EvalSymlinks(atual); err == nil {
			return filepath.Join(r, resto)
		}
		pai := filepath.Dir(atual)
		if pai == atual {
			return caminho
		}
		resto = filepath.Join(filepath.Base(atual), resto)
		atual = pai
	}
}
