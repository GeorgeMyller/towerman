//go:build !windows

package processo

import (
	"errors"
	"syscall"
)

func Vivo(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// No Unix, o pai e o nome vêm de /proc ou do ps (ver ancestrais.go).
func paiSO(int) int     { return 0 }
func nomeSO(int) string { return "" }
