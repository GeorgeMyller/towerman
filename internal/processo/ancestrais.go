package processo

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Ancestrais devolve pid e os seus antecessores, do mais próximo ao mais distante (máximo 32).
// Serve para encontrar o terminal do IDE, cujo processId é o shell que lançou a sessão.
func Ancestrais(pid int) []int {
	var r []int
	for i := 0; pid > 1 && i < 32; i++ {
		r = append(r, pid)
		pid = pai(pid)
	}
	return r
}

// shells são os intérpretes que os agentes usam para correr um hook («sh -c 'towerman hook'»).
// No Windows, o nome vem sem «.exe» e em minúsculas (cmd, powershell, pwsh, bash do Git Bash).
var shells = map[string]bool{"sh": true, "bash": true, "dash": true, "zsh": true, "fish": true,
	"cmd": true, "powershell": true, "pwsh": true}

// SemShell sobe de pid enquanto ele for um shell e devolve o primeiro processo que não é.
// O pai de um hook é muitas vezes o «sh -c» que o lançou, e esse morre logo: a sessão tem de
// ficar com o pid do agente (agy, codex), senão é dada como terminada.
func SemShell(pid int) int {
	for i := 0; pid > 1 && i < 4; i++ {
		if !shells[nome(pid)] {
			return pid
		}
		p := pai(pid)
		if p <= 1 {
			break
		}
		pid = p
	}
	return pid
}

func nome(pid int) string {
	if runtime.GOOS == "linux" {
		b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/comm")
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(b))
	}
	if runtime.GOOS == "windows" {
		return nomeSO(pid)
	}
	out, err := exec.Command("ps", "-o", "comm=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(filepath.Base(strings.TrimSpace(string(out))), "-")
}

func pai(pid int) int {
	switch runtime.GOOS {
	case "linux":
		b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
		if err != nil {
			return 0
		}
		s := string(b)
		i := strings.LastIndexByte(s, ')')
		if i < 0 {
			return 0
		}
		campos := strings.Fields(s[i+1:])
		if len(campos) < 2 {
			return 0
		}
		p, _ := strconv.Atoi(campos[1])
		return p
	case "windows":
		return paiSO(pid)
	default:
		out, err := exec.Command("ps", "-o", "ppid=", "-p", strconv.Itoa(pid)).Output()
		if err != nil {
			return 0
		}
		p, _ := strconv.Atoi(strings.TrimSpace(string(out)))
		return p
	}
}

// AppMac devolve o caminho do .app (macOS) que é antecessor de um destes pids, ou "".
// O processo mais distante dentro de um bundle é o app principal (os Helper.app vivem dentro dele).
func AppMac(pids []int) string {
	if runtime.GOOS != "darwin" {
		return ""
	}
	app := ""
	for _, pid := range pids {
		out, err := exec.Command("ps", "-o", "comm=", "-p", strconv.Itoa(pid)).Output()
		if err != nil {
			continue
		}
		if i := strings.Index(string(out), ".app/"); i >= 0 {
			app = string(out)[:i+4]
		}
	}
	return app
}
