package instalar

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/GeorgeMyller/towerman/internal/config"
	"github.com/GeorgeMyller/towerman/internal/jsonord"
)

// ComandoHook identifica as entradas do Towerman: o uninstall só remove hooks com este comando
// (ou com ele seguido de argumentos, como «--agente codex»).
const ComandoHook = "towerman hook"

// ComandoHookCodex marca as sessões vindas do Codex.
const ComandoHookCodex = ComandoHook + " --agente codex"

type eventoHook struct {
	nome     string
	matcher  string
	segundos string
}

// O PermissionRequest espera até 10 min pela decisão no painel ou na app, por isso o timeout é maior.
// O PostToolUse corre em todas as ferramentas para libertar pedidos já respondidos no terminal.
var eventos = []eventoHook{
	{"SessionStart", "", "5"},
	{"UserPromptSubmit", "", "5"},
	{"PreToolUse", "Edit|Write|MultiEdit|NotebookEdit", "5"},
	{"PostToolUse", "", "5"},
	{"Notification", "", "5"},
	{"PermissionRequest", "", "610"},
	{"Stop", "", "5"},
	{"SessionEnd", "", "5"},
}

// eventosCodex: o Codex fala o mesmo contrato de hooks do Claude Code (PermissionRequest, Stop
// com decision block), sem Notification; as edições passam pelo apply_patch.
var eventosCodex = []eventoHook{
	{"SessionStart", "", "5"},
	{"UserPromptSubmit", "", "5"},
	{"PreToolUse", "apply_patch|Edit|Write", "5"},
	{"PostToolUse", "", "5"},
	{"PermissionRequest", "", "610"},
	{"Stop", "", "5"},
	{"SessionEnd", "", "5"},
}

type Resultado struct {
	Acrescentados []string
	Atualizados   []string
	Existentes    []string
	Removidos     []string
	Escrito       bool
}

func CaminhoSettingsPadrao() (string, error) {
	d, err := config.ClaudeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "settings.json"), nil
}

func lerSettings(caminho string) (*jsonord.Objeto, []byte, fs.FileMode, error) {
	dados, err := os.ReadFile(caminho)
	if errors.Is(err, fs.ErrNotExist) {
		return &jsonord.Objeto{}, nil, 0o644, nil
	}
	if err != nil {
		return nil, nil, 0, err
	}
	modo := fs.FileMode(0o644)
	if st, err := os.Stat(caminho); err == nil {
		modo = st.Mode().Perm()
	}
	v, err := jsonord.Descodificar(dados)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("%s: %w", caminho, err)
	}
	raiz, ok := v.(*jsonord.Objeto)
	if !ok {
		return nil, nil, 0, fmt.Errorf("%s: o topo não é um objeto JSON", caminho)
	}
	return raiz, dados, modo, nil
}

func gravarSettings(caminho string, raiz *jsonord.Objeto, original []byte, modo fs.FileMode) error {
	saida, err := jsonord.Codificar(raiz)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(caminho), 0o755); err != nil {
		return err
	}
	if original != nil {
		if err := config.EscreverAtomico(caminho+".towerman.bak", original, modo); err != nil {
			return fmt.Errorf("cópia de segurança: %w", err)
		}
	}
	return config.EscreverAtomico(caminho, saida, modo)
}

func Instalar(caminho string) (Resultado, error) {
	return instalarEm(caminho, ComandoHook, eventos)
}

// InstalarCodex escreve os hooks do Towerman em ~/.codex/hooks.json (mesma estrutura «hooks»).
func InstalarCodex(caminho string) (Resultado, error) {
	return instalarEm(caminho, ComandoHookCodex, eventosCodex)
}

func instalarEm(caminho, comando string, lista []eventoHook) (Resultado, error) {
	var r Resultado
	raiz, original, modo, err := lerSettings(caminho)
	if err != nil {
		return r, err
	}
	hooks, err := objetoFilho(raiz, "hooks")
	if err != nil {
		return r, err
	}
	for _, ev := range lista {
		grupos, err := listaFilha(hooks, ev.nome)
		if err != nil {
			return r, err
		}
		if contemTowerman(grupos) {
			if atual(grupos, ev, comando) {
				r.Existentes = append(r.Existentes, ev.nome)
				continue
			}
			// Instalação antiga (outro matcher ou timeout): troca só o grupo do towerman.
			restantes, _ := semTowerman(grupos)
			hooks.Definir(ev.nome, append(restantes, novoGrupo(comando, ev.matcher, ev.segundos)))
			r.Atualizados = append(r.Atualizados, ev.nome)
			continue
		}
		hooks.Definir(ev.nome, append(grupos, novoGrupo(comando, ev.matcher, ev.segundos)))
		r.Acrescentados = append(r.Acrescentados, ev.nome)
	}
	if len(r.Acrescentados) == 0 && len(r.Atualizados) == 0 {
		return r, nil
	}
	if err := gravarSettings(caminho, raiz, original, modo); err != nil {
		return r, err
	}
	r.Escrito = true
	return r, nil
}

func Desinstalar(caminho string) (Resultado, error) {
	var r Resultado
	raiz, original, modo, err := lerSettings(caminho)
	if err != nil || original == nil {
		return r, err
	}
	v, ok := raiz.Obter("hooks")
	if !ok {
		return r, nil
	}
	hooks, ok := v.(*jsonord.Objeto)
	if !ok {
		return r, nil
	}
	for _, par := range append([]jsonord.Par(nil), hooks.Pares...) {
		grupos, ok := par.Valor.([]any)
		if !ok {
			continue
		}
		restantes, removeu := semTowerman(grupos)
		if !removeu {
			continue
		}
		r.Removidos = append(r.Removidos, par.Chave)
		if len(restantes) == 0 {
			hooks.Remover(par.Chave)
		} else {
			hooks.Definir(par.Chave, restantes)
		}
	}
	if len(r.Removidos) == 0 {
		return r, nil
	}
	if len(hooks.Pares) == 0 {
		raiz.Remover("hooks")
	}
	if err := gravarSettings(caminho, raiz, original, modo); err != nil {
		return r, err
	}
	r.Escrito = true
	return r, nil
}

func objetoFilho(o *jsonord.Objeto, chave string) (*jsonord.Objeto, error) {
	v, ok := o.Obter(chave)
	if !ok {
		n := &jsonord.Objeto{}
		o.Definir(chave, n)
		return n, nil
	}
	f, ok := v.(*jsonord.Objeto)
	if !ok {
		return nil, fmt.Errorf("%q não é um objeto", chave)
	}
	return f, nil
}

func listaFilha(o *jsonord.Objeto, chave string) ([]any, error) {
	v, ok := o.Obter(chave)
	if !ok {
		return nil, nil
	}
	l, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("hooks.%s não é uma lista", chave)
	}
	return l, nil
}

func novoGrupo(comando, matcher, segundos string) *jsonord.Objeto {
	h := &jsonord.Objeto{Pares: []jsonord.Par{
		{Chave: "type", Valor: "command"},
		{Chave: "command", Valor: comando},
		{Chave: "timeout", Valor: json.Number(segundos)},
	}}
	g := &jsonord.Objeto{}
	if matcher != "" {
		g.Definir("matcher", matcher)
	}
	g.Definir("hooks", []any{h})
	return g
}

func ehTowerman(h any) bool {
	o, ok := h.(*jsonord.Objeto)
	if !ok {
		return false
	}
	c, _ := o.Obter("command")
	t, _ := c.(string)
	return t == ComandoHook || strings.HasPrefix(t, ComandoHook+" ")
}

func hooksDoGrupo(g any) ([]any, *jsonord.Objeto) {
	o, ok := g.(*jsonord.Objeto)
	if !ok {
		return nil, nil
	}
	v, _ := o.Obter("hooks")
	l, _ := v.([]any)
	return l, o
}

func contemTowerman(grupos []any) bool {
	for _, g := range grupos {
		hs, _ := hooksDoGrupo(g)
		for _, h := range hs {
			if ehTowerman(h) {
				return true
			}
		}
	}
	return false
}

// atual diz se o grupo do towerman já é exatamente o que o install escreveria agora.
func atual(grupos []any, ev eventoHook, comando string) bool {
	esperado, _ := jsonord.Codificar(novoGrupo(comando, ev.matcher, ev.segundos))
	for _, g := range grupos {
		hs, _ := hooksDoGrupo(g)
		if len(hs) == 1 && ehTowerman(hs[0]) {
			b, _ := jsonord.Codificar(g)
			return string(b) == string(esperado)
		}
	}
	return false
}

func semTowerman(grupos []any) ([]any, bool) {
	removeu := false
	restantes := make([]any, 0, len(grupos))
	for _, g := range grupos {
		hs, o := hooksDoGrupo(g)
		if o == nil {
			restantes = append(restantes, g)
			continue
		}
		ficam := make([]any, 0, len(hs))
		for _, h := range hs {
			if ehTowerman(h) {
				removeu = true
				continue
			}
			ficam = append(ficam, h)
		}
		if len(ficam) == len(hs) {
			restantes = append(restantes, g)
			continue
		}
		if len(ficam) > 0 {
			o.Definir("hooks", ficam)
			restantes = append(restantes, o)
		}
	}
	return restantes, removeu
}
