package instalar

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

const fixtureVizinhos = `{
  "env": {
    "X": "1"
  },
  "hooks": {
    "SessionStart": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "python3 /buddy/claude_hook.py",
            "timeout": 10
          }
        ]
      },
      {
        "matcher": "",
        "hooks": [
          {
            "type": "command",
            "command": "node \"/pixel/claude-hook.js\"",
            "timeout": 5
          }
        ]
      }
    ],
    "PreToolUse": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "python3 /buddy/claude_hook.py",
            "timeout": 60
          }
        ]
      }
    ]
  },
  "model": "opus"
}
`

func escreverFixture(t *testing.T, conteudo string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(p, []byte(conteudo), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func valor(t *testing.T, dados []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(dados, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestInstalarPreservaVizinhosEOrdem(t *testing.T) {
	p := escreverFixture(t, fixtureVizinhos)
	r, err := Instalar(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Acrescentados) != 8 || !r.Escrito {
		t.Fatalf("resultado inesperado: %+v", r)
	}
	dados, _ := os.ReadFile(p)
	s := string(dados)
	if strings.Index(s, `"env"`) > strings.Index(s, `"hooks"`) || strings.Index(s, `"hooks"`) > strings.Index(s, `"model"`) {
		t.Fatal("a ordem das chaves de topo mudou")
	}
	if !strings.Contains(s, "python3 /buddy/claude_hook.py") || !strings.Contains(s, `node \"/pixel/claude-hook.js\"`) {
		t.Fatal("hooks vizinhos perdidos")
	}
	v := valor(t, dados).(map[string]any)
	hooks := v["hooks"].(map[string]any)
	ss := hooks["SessionStart"].([]any)
	if len(ss) != 3 {
		t.Fatalf("SessionStart devia ter 3 grupos, tem %d", len(ss))
	}
	ultimo := ss[2].(map[string]any)["hooks"].([]any)[0].(map[string]any)
	if ultimo["command"] != ComandoHook || ultimo["timeout"].(float64) != 5 {
		t.Fatalf("entrada do towerman inesperada: %v", ultimo)
	}
	ptu := hooks["PostToolUse"].([]any)[0].(map[string]any)
	if _, tem := ptu["matcher"]; tem {
		t.Fatalf("matcher inesperado: %v", ptu["matcher"])
	}
	if _, err := os.Stat(p + ".towerman.bak"); err != nil {
		t.Fatal("faltou a cópia de segurança")
	}
	if st, _ := os.Stat(p); runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
		t.Fatalf("permissões alteradas: %v", st.Mode().Perm())
	}
}

func TestInstalarIdempotenteEDesinstalarRepoe(t *testing.T) {
	p := escreverFixture(t, fixtureVizinhos)
	if _, err := Instalar(p); err != nil {
		t.Fatal(err)
	}
	depois1, _ := os.ReadFile(p)
	r, err := Instalar(p)
	if err != nil {
		t.Fatal(err)
	}
	if r.Escrito || len(r.Existentes) != 8 {
		t.Fatalf("segundo install devia ser no-op: %+v", r)
	}
	depois2, _ := os.ReadFile(p)
	if string(depois1) != string(depois2) {
		t.Fatal("segundo install alterou o ficheiro")
	}
	if _, err := Desinstalar(p); err != nil {
		t.Fatal(err)
	}
	final, _ := os.ReadFile(p)
	if !reflect.DeepEqual(valor(t, final), valor(t, []byte(fixtureVizinhos))) {
		t.Fatalf("uninstall não repôs o valor original:\n%s", final)
	}
	if string(final) != fixtureVizinhos {
		t.Fatalf("uninstall não repôs os bytes originais:\n%s", final)
	}
}

func TestDesinstalarGrupoMisto(t *testing.T) {
	p := escreverFixture(t, `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"towerman hook"},{"type":"command","command":"outro"}]}]}}`)
	r, err := Desinstalar(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Removidos) != 1 {
		t.Fatalf("resultado inesperado: %+v", r)
	}
	v := valor(t, mustRead(t, p)).(map[string]any)
	hs := v["hooks"].(map[string]any)["Stop"].([]any)[0].(map[string]any)["hooks"].([]any)
	if len(hs) != 1 || hs[0].(map[string]any)["command"] != "outro" {
		t.Fatalf("grupo misto mal tratado: %v", hs)
	}
}

func TestSettingsInexistenteEJSONInvalido(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "settings.json")
	if _, err := Instalar(p); err != nil {
		t.Fatal(err)
	}
	if _, err := Desinstalar(p); err != nil {
		t.Fatal(err)
	}
	if v := valor(t, mustRead(t, p)); !reflect.DeepEqual(v, map[string]any{}) {
		t.Fatalf("esperava {}, obtive %v", v)
	}

	inv := escreverFixture(t, `{"hooks": [}`)
	if _, err := Instalar(inv); err == nil {
		t.Fatal("esperava erro com JSON inválido")
	}
	if string(mustRead(t, inv)) != `{"hooks": [}` {
		t.Fatal("JSON inválido não devia ser reescrito")
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
