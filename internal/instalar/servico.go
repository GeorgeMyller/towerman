package instalar

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

var executar = func(nome string, args ...string) error {
	out, err := exec.Command(nome, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", nome, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// variaveisServico são as variáveis do ambiente do install que o serviço tem de herdar.
func variaveisServico() [][2]string {
	var r [][2]string
	for _, k := range []string{"TOWERMAN_HOME", "TOWERMAN_PORTA", "TOWERMAN_CLAUDE_DIR", "TOWERMAN_PORTA_REDE", "TOWERMAN_TUNEL"} {
		if v := os.Getenv(k); v != "" {
			r = append(r, [2]string{k, v})
		}
	}
	return r
}
