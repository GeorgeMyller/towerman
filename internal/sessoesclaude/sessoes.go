// Package sessoesclaude lê ~/.claude/sessions/<pid>.json, um formato interno e não documentado
// do Claude Code. Toda a dependência desse formato vive aqui; se falhar, quem chama usa o ppid do hook.
package sessoesclaude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Registo struct {
	Pid       int    `json:"pid"`
	SessionID string `json:"sessionId"`
	Cwd       string `json:"cwd"`
	Nome      string `json:"name"`
	Status    string `json:"status"`
}

func Procurar(claudeDir, sessionID string) (Registo, bool) {
	if sessionID == "" {
		return Registo{}, false
	}
	ficheiros, err := filepath.Glob(filepath.Join(claudeDir, "sessions", "*.json"))
	if err != nil {
		return Registo{}, false
	}
	for _, f := range ficheiros {
		r, ok := ler(f)
		if ok && r.SessionID == sessionID {
			return r, true
		}
	}
	return Registo{}, false
}

// DoPid lê o registo de um pid, para confirmar que o pid ainda pertence à mesma sessão.
func DoPid(claudeDir string, pid int) (Registo, bool) {
	return ler(filepath.Join(claudeDir, "sessions", strconv.Itoa(pid)+".json"))
}

func ler(f string) (Registo, bool) {
	b, err := os.ReadFile(f)
	if err != nil {
		return Registo{}, false
	}
	var r Registo
	if json.Unmarshal(b, &r) != nil || r.SessionID == "" {
		return Registo{}, false
	}
	if r.Pid == 0 {
		base := strings.TrimSuffix(filepath.Base(f), ".json")
		r.Pid, _ = strconv.Atoi(base)
	}
	return r, true
}
