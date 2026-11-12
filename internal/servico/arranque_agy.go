package servico

import (
	"encoding/json"

	"github.com/GeorgeMyller/towerman/internal/papeis"
)

// arrancadorAgy corre o Antigravity CLI em modo -p. O agy escolhe o id da conversa e anuncia-o no
// evento init; os hooks dele usam o mesmo id (traduzirAgy), por isso o cartão é o mesmo.
type arrancadorAgy struct{}

func (arrancadorAgy) ID() string   { return "antigravity" }
func (arrancadorAgy) Nome() string { return "Antigravity" }

// Em headless, o agy nega logo o que não está em permissions.allow: o modo default não pede ao aparelho.
func (arrancadorAgy) Modos() []string { return []string{"default", "acceptEdits", "plan"} }

func (arrancadorAgy) Localizar() (string, error) {
	return localizarCLI("agy", ".local/bin/agy", ".antigravity/bin/agy")
}

func (arrancadorAgy) IDProprio() bool                 { return true }
func (arrancadorAgy) PorTurno() bool                  { return false }
func (arrancadorAgy) Retomar(string, string) []string { return nil }

func (arrancadorAgy) IDNaSaida(linha []byte) (string, bool) {
	var ev struct {
		Event          string `json:"event"`
		ConversationID string `json:"conversation_id"`
	}
	if json.Unmarshal(linha, &ev) != nil || ev.Event != "init" || ev.ConversationID == "" {
		return "", false
	}
	return ev.ConversationID, true
}

var modosAgy = map[string]string{"acceptEdits": "accept-edits", "plan": "plan"}

// Argumentos: o -p tem de ir colado (-p=), senão o agy toma a flag seguinte como prompt.
func (arrancadorAgy) Argumentos(_, _ string, _ *papeis.Papel, modo string) []string {
	args := []string{"--input-format", "stream-json", "--output-format", "stream-json"}
	if m := modosAgy[modo]; m != "" {
		args = append(args, "--mode", m)
	}
	return append(args, "-p=")
}

// Tarefa: o agy não tem prompt de sistema por argumento; o papel entra no início da primeira mensagem.
func (arrancadorAgy) Tarefa(papel *papeis.Papel, tarefa string) string {
	if papel == nil || papel.Prompt == "" {
		return tarefa
	}
	return "Papel «" + papel.Nome + "»:\n" + papel.Prompt + "\n\nTarefa:\n" + tarefa
}

func (arrancadorAgy) Linha(texto string) []byte {
	b, _ := json.Marshal(map[string]any{"event": "user", "message": map[string]any{"content": texto}})
	return b
}
