package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	PortaPadrao        = 7777
	TimeoutHookPadrao  = 5 * time.Millisecond
	Versao             = "2.0.1"
	tamanhoTokenBytes  = 32
	permissaoFicheiros = 0o600
)

func Home() (string, error) {
	if h := os.Getenv("TOWERMAN_HOME"); h != "" {
		return h, nil
	}
	u, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(u, ".claude", "towerman"), nil
}

func ClaudeDir() (string, error) {
	if d := os.Getenv("TOWERMAN_CLAUDE_DIR"); d != "" {
		return d, nil
	}
	u, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(u, ".claude"), nil
}

func caminho(nome string) (string, error) {
	h, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, nome), nil
}

func CaminhoToken() (string, error)  { return caminho("token") }
func CaminhoQuadro() (string, error) { return caminho("quadro.db") }
func CaminhoDiario() (string, error) { return caminho("diario.jsonl") }

func Porta() int {
	if v := os.Getenv("TOWERMAN_PORTA"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 && p < 65536 {
			return p
		}
	}
	return PortaPadrao
}

// PortaRedePadrao é a porta onde o iPhone e o relógio ligam (todas as interfaces).
const PortaRedePadrao = 7352

// PortaRede devolve a porta de rede; TOWERMAN_PORTA_REDE=0 desliga-a.
func PortaRede() int {
	if v := os.Getenv("TOWERMAN_PORTA_REDE"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p >= 0 && p < 65536 {
			return p
		}
	}
	return PortaRedePadrao
}

func TimeoutHook() time.Duration {
	if d, ok := TimeoutHookDefinido(); ok {
		return d
	}
	return TimeoutHookPadrao
}

// TimeoutHookDefinido devolve o TOWERMAN_TIMEOUT_MS, quando está definido e é válido.
func TimeoutHookDefinido() (time.Duration, bool) {
	if v := os.Getenv("TOWERMAN_TIMEOUT_MS"); v != "" {
		if ms, err := strconv.Atoi(v); err == nil && ms > 0 {
			return time.Duration(ms) * time.Millisecond, true
		}
	}
	return 0, false
}

func LerToken() (string, error) {
	p, err := CaminhoToken()
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	t := strings.TrimSpace(string(b))
	if t == "" {
		return "", errors.New("token vazio")
	}
	return t, nil
}

// GarantirToken devolve o token existente ou gera um novo; o booleano indica se foi gerado.
func GarantirToken() (string, bool, error) {
	if t, err := LerToken(); err == nil {
		return t, false, nil
	}
	p, err := CaminhoToken()
	if err != nil {
		return "", false, err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return "", false, err
	}
	b := make([]byte, tamanhoTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", false, err
	}
	t := hex.EncodeToString(b)
	if err := EscreverAtomico(p, []byte(t+"\n"), permissaoFicheiros); err != nil {
		return "", false, fmt.Errorf("gravar token: %w", err)
	}
	return t, true, nil
}

func EscreverAtomico(destino string, dados []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(destino), "."+filepath.Base(destino)+".*.tmp")
	if err != nil {
		return err
	}
	nome := tmp.Name()
	defer os.Remove(nome)
	if _, err := tmp.Write(dados); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(nome, perm); err != nil {
		return err
	}
	return os.Rename(nome, destino)
}

const (
	Regra2Pedir   = "ask"
	Regra2Recusar = "deny"
	Regra2Off     = "off"
	// RelayURLPadrao vazio: sem relay_url no config.json nem TOWERMAN_RELAY_URL, o acesso fora
	// de casa fica desligado. Cada pessoa aponta para o seu próprio relay (cmd/towerman-relay).
	RelayURLPadrao = ""
)

// Preferencias vivem em config.json no TOWERMAN_HOME; campos ausentes usam o padrão.
type Preferencias struct {
	Regra2   string `json:"regra2"`
	RelayURL string `json:"relay_url,omitempty"`
	// RelayToken vai no header X-Towerman-Relay-Token ao ligar como host; o relay exige-o quando
	// tem TOWERMAN_RELAY_TOKENS. A variável TOWERMAN_RELAY_TOKEN tem precedência.
	RelayToken string `json:"relay_token,omitempty"`
	// Decisoes desliga a deteção de decisões em texto livre no fim do turno; ausente vale ligada.
	Decisoes *bool `json:"decisoes,omitempty"`
}

// DecisoesLigadas diz se o Stop deve procurar decisões pendentes na última resposta.
func (p Preferencias) DecisoesLigadas() bool { return p.Decisoes == nil || *p.Decisoes }

func CaminhoPreferencias() (string, error) { return caminho("config.json") }

func LerPreferencias() Preferencias {
	h, err := Home()
	if err != nil {
		return Preferencias{Regra2: Regra2Pedir, RelayURL: RelayURLPadrao}
	}
	return LerPreferenciasEm(h)
}

func LerPreferenciasEm(home string) Preferencias {
	p := Preferencias{Regra2: Regra2Pedir, RelayURL: RelayURLPadrao}
	b, err := os.ReadFile(filepath.Join(home, "config.json"))
	if err != nil {
		return p
	}
	var lido Preferencias
	if json.Unmarshal(b, &lido) == nil {
		switch lido.Regra2 {
		case Regra2Pedir, Regra2Recusar, Regra2Off:
			p.Regra2 = lido.Regra2
		}
		if strings.TrimSpace(lido.RelayURL) != "" {
			p.RelayURL = strings.TrimSpace(lido.RelayURL)
		}
		p.RelayToken = strings.TrimSpace(lido.RelayToken)
		p.Decisoes = lido.Decisoes
	}
	return p
}

func GuardarPreferenciasEm(home string, p Preferencias) error {
	c := filepath.Join(home, "config.json")
	if err := os.MkdirAll(filepath.Dir(c), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(p, "", "  ")
	return EscreverAtomico(c, append(b, '\n'), permissaoFicheiros)
}
