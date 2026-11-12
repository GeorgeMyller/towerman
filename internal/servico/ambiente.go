package servico

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// O serviço do Linux arranca no boot (linger), antes do login gráfico, e fica sem DISPLAY: o
// xdotool e os terminais falhavam todos em silêncio até reiniciar o serviço. A sessão gráfica
// exporta o seu ambiente para o systemd do utilizador; vamos buscá-lo quando faz falta.
var variaveisGraficas = []string{"DISPLAY", "XAUTHORITY", "WAYLAND_DISPLAY"}

// garantirAmbienteGrafico copia para o processo o DISPLAY (e afins) do systemd do utilizador,
// quando o serviço ainda não o tem. Os comandos lançados depois herdam-no.
func garantirAmbienteGrafico() {
	if runtime.GOOS != "linux" || os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != "" {
		return
	}
	out, err := exec.Command("systemctl", "--user", "show-environment").Output()
	if err != nil {
		return
	}
	for k, v := range lerAmbienteGrafico(string(out)) {
		os.Setenv(k, v)
	}
}

// lerAmbienteGrafico extrai as variáveis gráficas da saída de «systemctl --user show-environment».
func lerAmbienteGrafico(saida string) map[string]string {
	r := map[string]string{}
	for _, linha := range strings.Split(saida, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(linha), "=")
		if !ok || v == "" {
			continue
		}
		for _, g := range variaveisGraficas {
			if k == g {
				r[k] = strings.Trim(v, `"'`)
			}
		}
	}
	return r
}
