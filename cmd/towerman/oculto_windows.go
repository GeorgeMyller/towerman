//go:build windows

package main

import (
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

var getConsoleWindow = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleWindow")

// relancarSemConsola é o «serve --oculto» da tarefa agendada: o Windows dá uma janela de consola
// a cada programa de consola lançado no login. Com janela, o serve relança-se sem ela
// (CREATE_NO_WINDOW), sem o «--oculto», e devolve true para este processo sair.
func relancarSemConsola(args []string) bool {
	if janela, _, _ := getConsoleWindow.Call(); janela == 0 {
		return false
	}
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	var resto []string
	for _, a := range args {
		if a != "--oculto" && a != "-oculto" {
			resto = append(resto, a)
		}
	}
	cmd := exec.Command(exe, append([]string{"serve"}, resto...)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	return cmd.Start() == nil
}
