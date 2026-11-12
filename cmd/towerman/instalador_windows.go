//go:build windows

package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// Instalador por duplo clique: o towerman.exe aberto sem argumentos (do Explorador, das
// Transferências) copia-se para %LOCALAPPDATA%\Programs\Towerman, põe essa pasta no PATH do
// utilizador (os hooks chamam «towerman» sem caminho), corre o install a partir da cópia, pede
// elevação só para a regra de firewall e mostra o QR para parear o iPhone.

func pastaInstalacao() string {
	return filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Towerman")
}

// instaladorDuploClique devolve true quando tratou a execução sem argumentos.
func instaladorDuploClique() bool {
	if os.Getenv("LOCALAPPDATA") == "" {
		return false
	}
	fmt.Println("Towerman: instalar neste computador")
	fmt.Println()
	err := instalarWindows(os.Stdout)
	if err != nil {
		fmt.Println()
		fmt.Println("erro:", err)
	}
	fmt.Println()
	fmt.Print("Prima Enter para fechar.")
	bufio.NewReader(os.Stdin).ReadString('\n')
	return true
}

func instalarWindows(out io.Writer) error {
	origem, err := os.Executable()
	if err != nil {
		return err
	}
	pasta := pastaInstalacao()
	destino := filepath.Join(pasta, "towerman.exe")
	if !strings.EqualFold(filepath.Clean(origem), destino) {
		if err := os.MkdirAll(pasta, 0o755); err != nil {
			return err
		}
		// O serviço antigo prende o .exe: parar a tarefa antes de o substituir.
		exec.Command("schtasks", "/End", "/TN", "Towerman").Run()
		if err := copiarComEspera(origem, destino); err != nil {
			return fmt.Errorf("copiar para %s: %w", destino, err)
		}
		fmt.Fprintln(out, "copiado para", destino)
	}
	if mudou, err := juntarAoPathUtilizador(pasta); err != nil {
		fmt.Fprintln(out, "aviso: PATH do utilizador não atualizado:", err)
	} else if mudou {
		fmt.Fprintln(out, "PATH do utilizador: acrescentada", pasta)
	}
	os.Setenv("PATH", pasta+string(os.PathListSeparator)+os.Getenv("PATH"))

	// O install corre a partir da cópia: a tarefa agendada e os hooks apontam para ela.
	install := exec.Command(destino, "install", "--sem-extensao")
	install.Stdout, install.Stderr = out, out
	if err := install.Run(); err != nil {
		return fmt.Errorf("towerman install: %w", err)
	}
	if exec.Command("netsh", "advfirewall", "firewall", "show", "rule", "name=Towerman").Run() != nil {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Para o iPhone chegar a este PC pelo Wi-Fi, o Windows vai pedir permissão de administrador.")
		if err := abrirFirewallElevado(); err != nil {
			fmt.Fprintln(out, "aviso: firewall por abrir (o acesso fora de casa funciona na mesma):", err)
		}
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, "Feche e volte a abrir o Codex, o Claude Code e o Antigravity para eles verem o towerman.")
	fmt.Fprintln(out, "O deck está no menu Iniciar: «Towerman».")
	// O serviço demora um pouco a arrancar pela tarefa agendada.
	for i := 0; i < 10; i++ {
		pair := exec.Command(destino, "pair")
		pair.Stdout, pair.Stderr = out, io.Discard
		if pair.Run() == nil {
			return nil
		}
		time.Sleep(time.Second)
	}
	fmt.Fprintln(out, "Para parear o iPhone, abra um terminal e corra: towerman pair")
	return nil
}

func copiarComEspera(origem, destino string) error {
	var err error
	for i := 0; i < 10; i++ {
		if err = copiarFicheiro(origem, destino); err == nil {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return err
}

func copiarFicheiro(origem, destino string) error {
	dados, err := os.ReadFile(origem)
	if err != nil {
		return err
	}
	tmp := destino + ".novo"
	if err := os.WriteFile(tmp, dados, 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, destino)
}

// juntarAoPathUtilizador acrescenta pasta ao Path de HKCU\Environment e avisa o Explorador, para
// os programas abertos a seguir (agentes, IDE) já a verem.
func juntarAoPathUtilizador(pasta string) (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, "Environment", registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return false, err
	}
	defer k.Close()
	atual, tipo, err := k.GetStringValue("Path")
	if err != nil && err != registry.ErrNotExist {
		return false, err
	}
	for _, p := range strings.Split(atual, ";") {
		if strings.EqualFold(strings.TrimRight(strings.TrimSpace(p), `\`), pasta) {
			return false, nil
		}
	}
	novo := pasta
	if strings.TrimSpace(atual) != "" {
		novo = strings.TrimRight(atual, ";") + ";" + pasta
	}
	if tipo == registry.EXPAND_SZ {
		err = k.SetExpandStringValue("Path", novo)
	} else {
		err = k.SetStringValue("Path", novo)
	}
	if err != nil {
		return false, err
	}
	avisarMudancaAmbiente()
	return true, nil
}

func avisarMudancaAmbiente() {
	ambiente, _ := syscall.UTF16PtrFromString("Environment")
	const hwndBroadcast, wmSettingChange, smtoAbortIfHung = 0xffff, 0x001A, 0x0002
	var resultado uintptr
	windows.NewLazySystemDLL("user32.dll").NewProc("SendMessageTimeoutW").Call(
		hwndBroadcast, wmSettingChange, 0, uintptr(unsafe.Pointer(ambiente)),
		smtoAbortIfHung, 5000, uintptr(unsafe.Pointer(&resultado)))
}

// abrirFirewallElevado corre o netsh com «executar como administrador» (aviso do UAC).
func abrirFirewallElevado() error {
	args := "advfirewall firewall add rule name=Towerman dir=in action=allow protocol=TCP localport=7352 profile=private"
	if v := os.Getenv("TOWERMAN_PORTA_REDE"); v != "" {
		args = strings.Replace(args, "localport=7352", "localport="+v, 1)
	}
	script := fmt.Sprintf("Start-Process netsh -ArgumentList '%s' -Verb RunAs -WindowStyle Hidden -Wait", args)
	return exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script).Run()
}
