package servico

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Entrega de texto no Terminal.app e no iTerm2, por AppleScript. O separador certo é o que tem
// o mesmo tty do processo do agente; não é preciso focar a janela. Na primeira vez, o macOS pede
// a autorização de Automação (Definições › Privacidade e segurança › Automação).

const esperaAppleScript = 3 * time.Second

// Os scripts recebem o tty e o texto por argumento: nada do texto entra no código do script.
const (
	scriptTerminal = `on run argv
	set alvo to item 1 of argv
	tell application "Terminal"
		repeat with w in windows
			try
				repeat with t in tabs of w
					if (tty of t as text) is equal to alvo then
						do script (item 2 of argv) in t
						return "ok"
					end if
				end repeat
			end try
		end repeat
	end tell
	return "sem"
end run`
	scriptITerm = `on run argv
	set alvo to item 1 of argv
	tell application "iTerm2"
		repeat with w in windows
			repeat with t in tabs of w
				repeat with s in sessions of t
					if (tty of s as text) is equal to alvo then
						tell s to write text (item 2 of argv)
						return "ok"
					end if
				end repeat
			end repeat
		end repeat
	end tell
	return "sem"
end run`
	scriptFocarTerminal = `on run argv
	set alvo to item 1 of argv
	tell application "Terminal"
		activate
		repeat with w in windows
			try
				repeat with t in (tabs of w)
					if (tty of t as text) is equal to alvo then
						set miniaturized of w to false
						set selected of t to true
						set index of w to 1
						return "ok"
					end if
				end repeat
			end try
		end repeat
	end tell
	return "sem"
end run`
	scriptFocarITerm = `on run argv
	set alvo to item 1 of argv
	tell application "iTerm2"
		activate
		repeat with w in windows
			try
				repeat with t in tabs of w
					repeat with s in sessions of t
						if (tty of s as text) is equal to alvo then
							tell t to select
							tell w to select
							return "ok"
						end if
					end repeat
				end repeat
			end try
		end repeat
	end tell
	return "sem"
end run`
)

func terminalMac(programa string) bool {
	return programa == "Apple_Terminal" || programa == "iTerm.app"
}

// ttyDe devolve o terminal de controlo do processo («/dev/ttys003»), ou vazio.
func ttyDe(pid int) string {
	if pid <= 0 {
		return ""
	}
	out, err := exec.Command("ps", "-o", "tty=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return ""
	}
	tty := strings.TrimSpace(string(out))
	if tty == "" || strings.HasPrefix(tty, "?") || tty == "??" {
		return ""
	}
	return "/dev/" + strings.TrimPrefix(tty, "/dev/")
}

// escreverNoTerminalMac escreve linha seguida de Enter no separador da sessão. Sem separador
// com esse tty, não escreve nada noutro sítio.
func escreverNoTerminalMac(programa string, pid int, linha string) error {
	tty := ttyDe(pid)
	if tty == "" {
		return errors.New("sem tty para a sessão")
	}
	script := scriptTerminal
	if programa == "iTerm.app" {
		script = scriptITerm
	}
	ctx, cancel := context.WithTimeout(context.Background(), esperaAppleScript)
	defer cancel()
	out, err := exec.CommandContext(ctx, "osascript", "-e", script, tty, linha).CombinedOutput()
	if err != nil {
		return fmt.Errorf("osascript: %v: %s", err, strings.TrimSpace(string(out)))
	}
	if strings.TrimSpace(string(out)) != "ok" {
		return fmt.Errorf("nenhum separador com o tty %s", tty)
	}
	return nil
}

// correrMac corre um comando do macOS (osascript, open) com prazo: um pedido de permissão de
// Automação ou um app sem resposta não pode prender o pedido HTTP.
func correrMac(nome string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), esperaAppleScript)
	defer cancel()
	return exec.CommandContext(ctx, nome, args...).Run()
}

// focarTerminalMac traz para a frente a janela e o separador com o tty da sessão.
func focarTerminalMac(programa string, pid int) error {
	tty := ttyDe(pid)
	if tty == "" {
		return errors.New("sem tty para a sessão")
	}
	script := scriptFocarTerminal
	if programa == "iTerm.app" {
		script = scriptFocarITerm
	}
	ctx, cancel := context.WithTimeout(context.Background(), esperaAppleScript)
	defer cancel()
	out, err := exec.CommandContext(ctx, "osascript", "-e", script, tty).CombinedOutput()
	if err != nil {
		return fmt.Errorf("osascript: %v: %s", err, strings.TrimSpace(string(out)))
	}
	if strings.TrimSpace(string(out)) != "ok" {
		return fmt.Errorf("nenhum separador com o tty %s", tty)
	}
	return nil
}
