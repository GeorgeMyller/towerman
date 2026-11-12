//go:build windows

package processo

import (
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const stillActive = 259

func Vivo(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var codigo uint32
	if err := windows.GetExitCodeProcess(h, &codigo); err != nil {
		return false
	}
	return codigo == stillActive
}

// processoSO procura pid na lista de processos do sistema (CreateToolhelp32Snapshot) e devolve
// o pai e o nome do executável em minúsculas, sem «.exe».
func processoSO(pid int) (paiPid int, nome string, ok bool) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0, "", false
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		if int(e.ProcessID) == pid {
			n := strings.ToLower(windows.UTF16ToString(e.ExeFile[:]))
			return int(e.ParentProcessID), strings.TrimSuffix(n, ".exe"), true
		}
	}
	return 0, "", false
}

func paiSO(pid int) int {
	p, _, _ := processoSO(pid)
	return p
}

func nomeSO(pid int) string {
	_, n, _ := processoSO(pid)
	return n
}
