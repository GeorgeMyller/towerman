//go:build windows

package instalar

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"

	"github.com/GeorgeMyller/towerman/internal/config"
)

// Atalho «Towerman» no menu Iniciar, que abre o deck. O formato .lnk é COM (IShellLink): sem CGO,
// quem o escreve é o PowerShell, pelo objeto WScript.Shell. O ícone é o PNG do radar dentro de
// um .ico na pasta do Towerman.

func caminhoAtalhoWindows() (string, error) {
	appdata := os.Getenv("APPDATA")
	if appdata == "" {
		return "", fmt.Errorf("APPDATA não definido")
	}
	return filepath.Join(appdata, "Microsoft", "Windows", "Start Menu", "Programs", "Towerman.lnk"), nil
}

// psTexto põe s entre aspas simples do PowerShell.
func psTexto(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func instalarAtalhoWindows(executavel string) (string, error) {
	lnk, err := caminhoAtalhoWindows()
	if err != nil {
		return "", err
	}
	home, err := config.Home()
	if err != nil {
		return "", err
	}
	icone := filepath.Join(home, "towerman.ico")
	if err := os.MkdirAll(home, 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(icone, icoDePNG(iconeTowerman), 0o644); err != nil {
		return "", err
	}
	// WindowStyle 7: a consola do «towerman deck» abre minimizada (só lança o navegador e sai).
	script := fmt.Sprintf(`$s=(New-Object -ComObject WScript.Shell).CreateShortcut(%s);`+
		`$s.TargetPath=%s;$s.Arguments='deck';$s.IconLocation=%s;$s.WindowStyle=7;`+
		`$s.Description='Radar dos seus agentes de código';$s.Save()`,
		psTexto(lnk), psTexto(executavel), psTexto(icone+",0"))
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("PowerShell: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return "atalho «Towerman» no menu Iniciar: " + lnk, nil
}

func removerAtalhoWindows() {
	if lnk, err := caminhoAtalhoWindows(); err == nil {
		os.Remove(lnk)
	}
	if home, err := config.Home(); err == nil {
		os.Remove(filepath.Join(home, "towerman.ico"))
	}
}
