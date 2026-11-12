//go:build !windows

package servico

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// presencaViva sonda a marca de presença de uma conversa do Antigravity: enquanto a conversa está
// aberta, o processo do agente segura um flock em <dir>/<sessionID>.lock. Bloqueio ocupado diz
// que está viva; bloqueio obtido (logo solto) ou ficheiro ausente com a pasta presente diz que
// fechou. Sem pasta ou com erro, não se sabe e vale o PID guardado.
func presencaViva(dir, sessionID string) estadoPresenca {
	if dir == "" || sessionID == "" {
		return marcaDesconhecida
	}
	f, err := os.OpenFile(filepath.Join(dir, sessionID+".lock"), os.O_RDONLY, 0)
	if errors.Is(err, os.ErrNotExist) {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return marcaFechada
		}
		return marcaDesconhecida
	}
	if err != nil {
		return marcaDesconhecida
	}
	defer f.Close()
	switch err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); {
	case err == nil:
		// Soltar já: o agente pode estar a tentar o seu LOCK_NB neste instante.
		unix.Flock(int(f.Fd()), unix.LOCK_UN)
		return marcaFechada
	case errors.Is(err, unix.EWOULDBLOCK), errors.Is(err, unix.EAGAIN):
		return marcaViva
	}
	return marcaDesconhecida
}
