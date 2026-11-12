package instalar

// Hooks do Antigravity (agy e agente do IDE) em ~/.gemini/config/hooks.json, o ficheiro de
// hooks do utilizador partilhado pelo CLI e pelo backend. O Towerman ocupa só a entrada
// "towerman"; os outros hooks ficam como estavam.

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/GeorgeMyller/towerman/internal/jsonord"
)

const nomeHookAgy = "towerman"

// CaminhoHooksCodex devolve ~/.codex/hooks.json, ou "" quando o Codex não está instalado.
func CaminhoHooksCodex() string {
	casa, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	if _, err := os.Stat(filepath.Join(casa, ".codex")); err != nil {
		return ""
	}
	return filepath.Join(casa, ".codex", "hooks.json")
}

// CaminhoHooksAgy devolve ~/.gemini/config/hooks.json, ou "" quando o Antigravity não está instalado.
func CaminhoHooksAgy() string {
	casa, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	if _, err := os.Stat(filepath.Join(casa, ".gemini")); err != nil {
		return ""
	}
	return filepath.Join(casa, ".gemini", "config", "hooks.json")
}

// entradaAgy é o grupo do Towerman: o evento vai no argumento, porque o Antigravity não o diz no stdin.
func entradaAgy() *jsonord.Objeto {
	cmd := func(evento string) *jsonord.Objeto {
		// O PreToolUse espera pela aprovação no telemóvel (até 10 min) só quando o agy, em modo
		// normal, ia esperar pela pessoa, e ela está longe; larga logo que ela volta. Nos outros
		// casos e eventos, o hook responde em milissegundos.
		segundos := "5"
		if evento == "PreToolUse" {
			segundos = "610"
		}
		return &jsonord.Objeto{Pares: []jsonord.Par{
			{Chave: "type", Valor: "command"},
			{Chave: "command", Valor: ComandoHook + " --agente agy " + evento},
			{Chave: "timeout", Valor: json.Number(segundos)},
		}}
	}
	ferramenta := func(evento string) []any {
		g := &jsonord.Objeto{}
		g.Definir("matcher", ".*")
		g.Definir("hooks", []any{cmd(evento)})
		return []any{g}
	}
	e := &jsonord.Objeto{}
	e.Definir("SessionStart", []any{cmd("SessionStart")})
	e.Definir("PreInvocation", []any{cmd("PreInvocation")})
	e.Definir("PreToolUse", ferramenta("PreToolUse"))
	e.Definir("PostToolUse", ferramenta("PostToolUse"))
	e.Definir("Stop", []any{cmd("Stop")})
	return e
}

// InstalarAgy acrescenta (ou atualiza) a entrada do Towerman; devolve se escreveu.
func InstalarAgy(caminho string) (bool, error) {
	raiz, original, modo, err := lerSettings(caminho)
	if err != nil {
		return false, err
	}
	nova := entradaAgy()
	if atual, ok := raiz.Obter(nomeHookAgy); ok {
		a, _ := jsonord.Codificar(atual)
		b, _ := jsonord.Codificar(nova)
		if string(a) == string(b) {
			return false, nil
		}
	}
	raiz.Definir(nomeHookAgy, nova)
	if original == nil {
		modo = 0o600
	}
	return true, gravarSettings(caminho, raiz, original, modo)
}

// DesinstalarAgy remove só a entrada do Towerman.
func DesinstalarAgy(caminho string) (bool, error) {
	raiz, original, modo, err := lerSettings(caminho)
	if err != nil || original == nil {
		return false, err
	}
	if _, ok := raiz.Obter(nomeHookAgy); !ok {
		return false, nil
	}
	raiz.Remover(nomeHookAgy)
	return true, gravarSettings(caminho, raiz, original, modo)
}
