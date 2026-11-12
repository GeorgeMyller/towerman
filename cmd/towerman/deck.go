package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/GeorgeMyller/towerman/internal/config"
)

// deck abre a página /deck numa janela de app (sem barras de browser) quando há um Chrome,
// Chromium, Edge ou Brave; senão, no browser normal.
func deck(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("deck", flag.ExitOnError)
	semAbrir := fs.Bool("sem-abrir", false, "só imprimir o URL")
	fs.Parse(args)
	token, err := config.LerToken()
	if err != nil {
		return fmt.Errorf("sem token (%w); corre primeiro \"towerman install\"", err)
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/deck?token=%s", config.Porta(), token)
	if *semAbrir {
		fmt.Fprintln(out, url)
		return nil
	}
	if abrirJanela(url) {
		return nil
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(out, url)
		fmt.Fprintln(out, "não consegui abrir o browser; abre o URL acima à mão")
	}
	return nil
}

func abrirJanela(url string) bool {
	janela := "--app=" + url
	if runtime.GOOS == "darwin" {
		for _, app := range []string{"Google Chrome", "Chromium", "Microsoft Edge", "Brave Browser"} {
			if _, err := os.Stat("/Applications/" + app + ".app"); err == nil {
				return exec.Command("open", "-na", app, "--args", janela).Start() == nil
			}
		}
		return false
	}
	if runtime.GOOS == "windows" {
		for _, caminho := range navegadoresWindows(os.Getenv) {
			if _, err := os.Stat(caminho); err == nil {
				return exec.Command(caminho, janela).Start() == nil
			}
		}
		return false
	}
	for _, bin := range []string{"google-chrome", "chromium", "chromium-browser", "microsoft-edge", "brave-browser"} {
		if caminho, err := exec.LookPath(bin); err == nil {
			return exec.Command(caminho, janela).Start() == nil
		}
	}
	return false
}

// navegadoresWindows lista onde procurar um navegador com «--app»: o Edge vem sempre com o
// Windows; o Chrome pode estar instalado para todos ou só para o utilizador.
func navegadoresWindows(env func(string) string) []string {
	var r []string
	for _, base := range []string{env("ProgramFiles(x86)"), env("ProgramFiles")} {
		if base != "" {
			r = append(r, filepath.Join(base, "Microsoft", "Edge", "Application", "msedge.exe"))
		}
	}
	for _, base := range []string{env("ProgramFiles"), env("ProgramFiles(x86)"), env("LOCALAPPDATA")} {
		if base != "" {
			r = append(r, filepath.Join(base, "Google", "Chrome", "Application", "chrome.exe"))
		}
	}
	return r
}
