package instalar

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Os hooks chamam «towerman» sem caminho. Um IDE aberto pelo menu de aplicações (Antigravity IDE,
// por exemplo) não lê o ~/.bashrc: só vê o PATH do sistema, sem ~/go/bin. Sem uma ligação numa
// pasta do sistema, os hooks desse IDE falham em silêncio e as sessões não aparecem.

// pastasSistema são as pastas do PATH que qualquer programa vê no Linux.
var pastasSistema = []string{"/usr/local/bin", "/usr/bin", "/bin"}

// GarantirPathSistema liga exe em /usr/local/bin/towerman quando nenhuma pasta do sistema o tem.
// Devolve o que fez, ou o comando a correr quando não tem permissão; "" quando já estava certo.
func GarantirPathSistema(exe string) string {
	if runtime.GOOS != "linux" {
		return ""
	}
	msg := garantirPathEm(exe, pastasSistema)
	if h, err := os.UserHomeDir(); err == nil {
		if m := trocarCopiaAntiga(exe, filepath.Join(h, ".local", "bin", "towerman")); m != "" {
			msg = strings.TrimSpace(msg + "\n" + m)
		}
	}
	return msg
}

// trocarCopiaAntiga troca um towerman antigo em ~/.local/bin (vem antes de ~/go/bin no PATH de
// muitos ambientes, e os hooks corriam-no) por uma ligação para exe. A cópia fica com «.antigo».
func trocarCopiaAntiga(exe, caminho string) string {
	if _, err := os.Lstat(caminho); err != nil {
		return ""
	}
	if alvo, err := filepath.EvalSymlinks(caminho); err == nil && alvo == exe {
		return ""
	}
	if err := os.Rename(caminho, caminho+".antigo"); err != nil {
		return fmt.Sprintf("aviso: %s é outro towerman e os hooks podem corrê-lo; apague-o: %v", caminho, err)
	}
	if err := os.Symlink(exe, caminho); err != nil {
		return fmt.Sprintf("aviso: %s não ligado a %s: %v", caminho, exe, err)
	}
	return fmt.Sprintf("towerman antigo em %s trocado por uma ligação (cópia em %s.antigo)", caminho, caminho)
}

func garantirPathEm(exe string, pastas []string) string {
	for _, d := range pastas {
		if alvo, err := filepath.EvalSymlinks(filepath.Join(d, "towerman")); err == nil && alvo == exe {
			return ""
		}
	}
	ligacao := filepath.Join(pastas[0], "towerman")
	comando := fmt.Sprintf("sudo ln -sf %s %s", exe, ligacao)
	if _, err := os.Lstat(ligacao); err == nil {
		return fmt.Sprintf("aviso: %s aponta para outro towerman; para os apps abertos pelo menu usarem este, corra: %s", ligacao, comando)
	}
	if err := os.Symlink(exe, ligacao); err != nil {
		return fmt.Sprintf("aviso: os apps abertos pelo menu (Antigravity IDE, por exemplo) não encontram o towerman; corra: %s", comando)
	}
	return fmt.Sprintf("ligação %s → %s (para os apps abertos pelo menu)", ligacao, exe)
}
