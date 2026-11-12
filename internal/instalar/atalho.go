package instalar

// Atalho de aplicação no Linux: «Towerman» no menu de aplicações (GNOME, KDE…), com o ícone do
// radar, que abre o deck numa janela própria. O servidor já arranca sozinho pelo systemd.

import (
	_ "embed"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

//go:embed towerman.png
var iconeTowerman []byte

func pastaDados() (string, error) {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return d, nil
	}
	casa, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(casa, ".local", "share"), nil
}

// InstalarAtalho escreve o .desktop e o ícone (Linux) ou o atalho do menu Iniciar (Windows); no
// macOS não faz nada (o Mac tem o app).
func InstalarAtalho(executavel string) (string, error) {
	if runtime.GOOS == "windows" {
		return instalarAtalhoWindows(executavel)
	}
	if runtime.GOOS != "linux" {
		return "", nil
	}
	dados, err := pastaDados()
	if err != nil {
		return "", err
	}
	icone := filepath.Join(dados, "icons", "hicolor", "256x256", "apps", "towerman.png")
	desktop := filepath.Join(dados, "applications", "towerman.desktop")
	for _, d := range []string{filepath.Dir(icone), filepath.Dir(desktop)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return "", err
		}
	}
	if err := os.WriteFile(icone, iconeTowerman, 0o644); err != nil {
		return "", err
	}
	conteudo := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=Towerman
Comment=Radar dos seus agentes de código: pedidos, partidas e respostas
Exec=%q deck
Icon=towerman
Terminal=false
Categories=Development;
StartupWMClass=Towerman
`, executavel)
	if err := os.WriteFile(desktop, []byte(conteudo), 0o644); err != nil {
		return "", err
	}
	return "atalho «Towerman» no menu de aplicações: " + desktop, nil
}

// RemoverAtalho apaga o .desktop e o ícone (ou o atalho do menu Iniciar).
func RemoverAtalho() {
	if runtime.GOOS == "windows" {
		removerAtalhoWindows()
		return
	}
	if runtime.GOOS != "linux" {
		return
	}
	if dados, err := pastaDados(); err == nil {
		os.Remove(filepath.Join(dados, "applications", "towerman.desktop"))
		os.Remove(filepath.Join(dados, "icons", "hicolor", "256x256", "apps", "towerman.png"))
	}
}

// icoDePNG embrulha um PNG de 256×256 num ficheiro .ico (o Windows aceita PNG dentro do ICO desde
// o Vista): ICONDIR de 6 bytes, uma ICONDIRENTRY de 16 bytes (0 = 256 px) e os dados do PNG.
func icoDePNG(png []byte) []byte {
	b := make([]byte, 0, 22+len(png))
	b = append(b, 0, 0, 1, 0, 1, 0)
	b = append(b, 0, 0, 0, 0, 1, 0, 32, 0)
	b = binary.LittleEndian.AppendUint32(b, uint32(len(png)))
	b = binary.LittleEndian.AppendUint32(b, 22)
	return append(b, png...)
}
