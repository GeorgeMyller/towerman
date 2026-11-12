//go:build windows

package servico

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32                   = windows.NewLazySystemDLL("user32.dll")
	procEnumWindows          = user32.NewProc("EnumWindows")
	procIsWindowVisible      = user32.NewProc("IsWindowVisible")
	procIsIconic             = user32.NewProc("IsIconic")
	procShowWindow           = user32.NewProc("ShowWindow")
	procSetForegroundWindow  = user32.NewProc("SetForegroundWindow")
	procKeybdEvent           = user32.NewProc("keybd_event")
	procGetWindowThreadPID   = user32.NewProc("GetWindowThreadProcessId")
	procGetWindowTextLengthW = user32.NewProc("GetWindowTextLengthW")
)

const (
	swRestore      = 9
	vkMenu         = 0x12
	keyeventfKeyUp = 0x0002
)

// janelasPorPid devolve a primeira janela visível, com título, de nível superior de cada processo.
func janelasPorPid() map[uint32]uintptr {
	r := map[uint32]uintptr{}
	cb := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		if v, _, _ := procIsWindowVisible.Call(hwnd); v == 0 {
			return 1
		}
		if n, _, _ := procGetWindowTextLengthW.Call(hwnd); n == 0 {
			return 1
		}
		var pid uint32
		procGetWindowThreadPID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		if _, ja := r[pid]; !ja {
			r[pid] = hwnd
		}
		return 1
	})
	procEnumWindows.Call(cb, 0)
	return r
}

// focarJanelaSO traz para a frente a janela do processo mais próximo da sessão que tenha uma
// (o agente, o Windows Terminal, o IDE). O Windows só deixa mudar o primeiro plano a quem recebeu
// a última entrada: uma tecla Alt simulada desbloqueia-o.
func focarJanelaSO(pids []int) bool {
	janelas := janelasPorPid()
	for _, pid := range pids {
		hwnd, ok := janelas[uint32(pid)]
		if !ok {
			continue
		}
		if min, _, _ := procIsIconic.Call(hwnd); min != 0 {
			procShowWindow.Call(hwnd, swRestore)
		}
		procKeybdEvent.Call(vkMenu, 0, 0, 0)
		procKeybdEvent.Call(vkMenu, 0, keyeventfKeyUp, 0)
		r, _, _ := procSetForegroundWindow.Call(hwnd)
		return r != 0
	}
	return false
}
