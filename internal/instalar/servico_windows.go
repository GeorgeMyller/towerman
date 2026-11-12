//go:build windows

package instalar

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/GeorgeMyller/towerman/internal/config"
)

const nomeTarefa = "Towerman"

// No Agendador de Tarefas não há variáveis de ambiente por tarefa, por isso passam pela linha de comando.
func comandoTarefa(executavel string) string {
	var b strings.Builder
	vs := variaveisServico()
	if len(vs) > 0 {
		b.WriteString(`cmd /c "`)
		for _, kv := range vs {
			fmt.Fprintf(&b, `set "%s=%s" && `, kv[0], kv[1])
		}
		fmt.Fprintf(&b, `"%s" serve --oculto"`, executavel)
		return b.String()
	}
	return fmt.Sprintf(`"%s" serve --oculto`, executavel)
}

// A porta de rede (iPhone e relógio pelo Wi-Fi) precisa de uma regra de entrada na firewall do
// Windows, só no perfil privado. Criar a regra exige elevação: sem ela, diz o comando a correr.
const regraFirewall = "Towerman"

func comandoFirewall() []string {
	return []string{"netsh", "advfirewall", "firewall", "add", "rule", "name=" + regraFirewall,
		"dir=in", "action=allow", "protocol=TCP", "localport=" + strconv.Itoa(config.PortaRede()), "profile=private"}
}

func abrirFirewall() string {
	if config.PortaRede() <= 0 {
		return ""
	}
	// Apagar antes torna o install idempotente (e atualiza a porta, se mudou).
	executar("netsh", "advfirewall", "firewall", "delete", "rule", "name="+regraFirewall)
	cmd := comandoFirewall()
	if err := executar(cmd[0], cmd[1:]...); err != nil {
		return "aviso: regra de firewall não criada (precisa de administrador); para o iPhone chegar pelo Wi-Fi, corra num terminal de administrador: " + strings.Join(cmd, " ")
	}
	return fmt.Sprintf("firewall: porta %d aberta na rede privada (regra %s)", config.PortaRede(), regraFirewall)
}

func RegistarServico(executavel string) (string, error) {
	if err := executar("schtasks", "/Create", "/F", "/SC", "ONLOGON", "/RL", "LIMITED",
		"/TN", nomeTarefa, "/TR", comandoTarefa(executavel)); err != nil {
		return "", err
	}
	executar("schtasks", "/End", "/TN", nomeTarefa)
	if err := executar("schtasks", "/Run", "/TN", nomeTarefa); err != nil {
		return "", err
	}
	msg := "tarefa agendada ativa: " + nomeTarefa
	if f := abrirFirewall(); f != "" {
		msg += "\n" + f
	}
	return msg, nil
}

func RemoverServico() (string, error) {
	if err := executar("schtasks", "/Query", "/TN", nomeTarefa); err != nil {
		return "sem serviço registado", nil
	}
	executar("schtasks", "/End", "/TN", nomeTarefa)
	executar("netsh", "advfirewall", "firewall", "delete", "rule", "name="+regraFirewall)
	if err := executar("schtasks", "/Delete", "/F", "/TN", nomeTarefa); err != nil {
		return "", err
	}
	return "tarefa agendada removida: " + nomeTarefa, nil
}
