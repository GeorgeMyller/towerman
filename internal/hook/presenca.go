package hook

// Presença: há quanto tempo o teclado e o rato estão parados e se a janela da sessão está à
// frente. Serve para decidir se um pedido do Antigravity vai para o telemóvel (está longe ou
// a trabalhar noutra janela) ou fica no terminal (está a olhar para a sessão).

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/GeorgeMyller/towerman/internal/config"
	"github.com/GeorgeMyller/towerman/internal/processo"
)

var idleIOReg = regexp.MustCompile(`"HIDIdleTime" = (\d+)`)

// Inatividade devolve o tempo sem teclado nem rato; ok é falso quando não há forma de saber.
func Inatividade() (time.Duration, bool) {
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command("ioreg", "-c", "IOHIDSystem", "-d", "4").Output()
		if err != nil {
			return 0, false
		}
		m := idleIOReg.FindSubmatch(out)
		if m == nil {
			return 0, false
		}
		ns, err := strconv.ParseInt(string(m[1]), 10, 64)
		return time.Duration(ns), err == nil
	case "linux":
		return inatividadeLinux()
	case "windows":
		return inatividadeSO()
	}
	return 0, false
}

// limiteAusencia: TOWERMAN_AUSENTE_S (padrão 60 s).
func limiteAusencia() time.Duration {
	if v, err := strconv.Atoi(os.Getenv("TOWERMAN_AUSENTE_S")); err == nil && v >= 0 {
		return time.Duration(v) * time.Second
	}
	return 60 * time.Second
}

// Ausente diz se os pedidos da sessão em cwd devem ir para o telemóvel: teclado e rato parados,
// ou a pessoa a trabalhar noutra janela. TOWERMAN_AGY_REMOTO=sempre força, =nunca desliga;
// sem forma de medir, conta como presente (nada muda).
func Ausente(cwd string) bool {
	switch os.Getenv("TOWERMAN_AGY_REMOTO") {
	case "sempre":
		return true
	case "nunca":
		return false
	}
	if d, ok := Inatividade(); ok && d >= limiteAusencia() {
		return true
	}
	return !sessaoAFrente(cwd)
}

// esperaComando é o tempo máximo de cada comando de medição no Linux: o pedido de aprovação não
// pode ficar à espera de um xdotool ou de um gdbus encravado.
const esperaComando = 200 * time.Millisecond

// correrMedicao corre um comando de medição com o tempo limite.
func correrMedicao(nome string, args ...string) ([]byte, error) {
	ctx, cancelar := context.WithTimeout(context.Background(), esperaComando)
	defer cancelar()
	return exec.CommandContext(ctx, nome, args...).Output()
}

// inatividadeLinux: xprintidle (X11, ms); sem ele, o IdleMonitor do Mutter (GNOME, também em
// Wayland, ms) e o org.freedesktop.ScreenSaver (s), pelo gdbus.
func inatividadeLinux() (time.Duration, bool) {
	if out, err := correrMedicao("xprintidle"); err == nil {
		if ms, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64); err == nil {
			return time.Duration(ms) * time.Millisecond, true
		}
	}
	if out, err := correrMedicao("gdbus", "call", "--session", "--dest", "org.gnome.Mutter.IdleMonitor",
		"--object-path", "/org/gnome/Mutter/IdleMonitor/Core", "--method", "org.gnome.Mutter.IdleMonitor.GetIdletime"); err == nil {
		if ms, ok := lerIdleGdbus(string(out)); ok {
			return time.Duration(ms) * time.Millisecond, true
		}
	}
	if out, err := correrMedicao("gdbus", "call", "--session", "--dest", "org.freedesktop.ScreenSaver",
		"--object-path", "/org/freedesktop/ScreenSaver", "--method", "org.freedesktop.ScreenSaver.GetSessionIdleTime"); err == nil {
		if s, ok := lerIdleGdbus(string(out)); ok {
			return time.Duration(s) * time.Second, true
		}
	}
	return 0, false
}

var idleGdbusRegex = regexp.MustCompile(`^\(uint(?:32|64) (\d+),\)$`)

// lerIdleGdbus lê a resposta do gdbus a um tempo de inatividade: «(uint64 1234,)» ou «(uint32 5,)».
func lerIdleGdbus(saida string) (uint64, bool) {
	m := idleGdbusRegex.FindStringSubmatch(strings.TrimSpace(saida))
	if m == nil {
		return 0, false
	}
	n, err := strconv.ParseUint(m[1], 10, 64)
	return n, err == nil
}

var appFrenteRegex = regexp.MustCompile(`bundle path="([^"]+)"`)

// sessaoAFrente diz se a janela desta sessão é a que tem o foco. No macOS compara o app da
// frente com o app que corre o hook e, quando é um IDE com a extensão, pergunta à janela da
// sessão se tem o foco. Sem forma de saber, conta como à frente.
func sessaoAFrente(cwd string) bool {
	if runtime.GOOS == "linux" {
		return sessaoAFrenteLinux(cwd)
	}
	if runtime.GOOS != "darwin" {
		return true
	}
	pids := processo.Ancestrais(os.Getpid())
	app := processo.AppMac(pids)
	if app == "" {
		return true
	}
	asn, err := exec.Command("lsappinfo", "front").Output()
	if err != nil {
		return true
	}
	out, err := exec.Command("lsappinfo", "info", "-only", "bundlepath", strings.TrimSpace(string(asn))).Output()
	if err != nil {
		return true
	}
	m := appFrenteRegex.FindSubmatch(out)
	if m == nil {
		return true
	}
	if string(m[1]) != app {
		return false
	}
	focada, sabe := janelaFocada(pids, cwd)
	return !sabe || focada
}

// sessaoAFrenteLinux pergunta primeiro à extensão do IDE; sem resposta, em X11, compara o
// processo da janela ativa com os ancestrais do hook (o emulador de terminal ou o IDE que corre a
// sessão). Wayland sem extensão, ou sem xdotool: conta como à frente.
func sessaoAFrenteLinux(cwd string) bool {
	pids := processo.Ancestrais(os.Getpid())
	if focada, sabe := janelaFocada(pids, cwd); sabe {
		return focada
	}
	if os.Getenv("DISPLAY") == "" {
		return true
	}
	out, err := correrMedicao("xdotool", "getactivewindow", "getwindowpid")
	if err != nil {
		return true
	}
	ativa, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return true
	}
	return janelaDaSessao(ativa, pids)
}

// janelaDaSessao diz se o processo da janela ativa é um dos ancestrais do hook.
func janelaDaSessao(pidJanela int, ancestrais []int) bool {
	for _, p := range ancestrais {
		if p == pidJanela {
			return true
		}
	}
	return false
}

// janelaFocada pergunta ao serviço se a janela do IDE com o terminal (ou a pasta) da sessão tem
// o foco; sabe é falso sem extensão ligada ou sem resposta.
func janelaFocada(pids []int, cwd string) (focada, sabe bool) {
	token, err := config.LerToken()
	if err != nil {
		return false, false
	}
	corpo, _ := json.Marshal(map[string]any{"pids": pids, "cwd": cwd})
	url := fmt.Sprintf("http://127.0.0.1:%d/v1/presenca", config.Porta())
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(corpo))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		return false, false
	}
	defer resp.Body.Close()
	var r struct {
		Sabe   bool `json:"sabe"`
		Focada bool `json:"focada"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&r) != nil {
		return false, false
	}
	return r.Focada, r.Sabe
}
