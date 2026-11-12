//go:build windows

package hook

import (
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32           = windows.NewLazySystemDLL("user32.dll")
	kernel32         = windows.NewLazySystemDLL("kernel32.dll")
	getLastInputInfo = user32.NewProc("GetLastInputInfo")
	getTickCount     = kernel32.NewProc("GetTickCount")
)

type lastInputInfo struct {
	cbSize uint32
	dwTime uint32
}

// inatividadeSO mede o tempo sem teclado nem rato na sessão do utilizador. Os dois contadores
// são de 32 bits em milissegundos: a subtração sem sinal continua certa depois de darem a volta.
func inatividadeSO() (time.Duration, bool) {
	info := lastInputInfo{cbSize: uint32(unsafe.Sizeof(lastInputInfo{}))}
	if r, _, _ := getLastInputInfo.Call(uintptr(unsafe.Pointer(&info))); r == 0 {
		return 0, false
	}
	agora, _, _ := getTickCount.Call()
	return time.Duration(uint32(agora)-info.dwTime) * time.Millisecond, true
}
