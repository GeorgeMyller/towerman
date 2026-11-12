// Package transcricao lê o fim do jsonl de uma sessão do Claude Code (ou do Antigravity, ou do
// Codex) e devolve as mensagens legíveis.
// O formato não é documentado: linhas e blocos desconhecidos são ignorados, nunca são erro.
package transcricao

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
)

const LeituraMaxima = 2 << 20

type Mensagem struct {
	Papel string `json:"papel"`
	Texto string `json:"texto"`
	Em    string `json:"em"`
}

type linha struct {
	Type        string `json:"type"`
	Timestamp   string `json:"timestamp"`
	IsMeta      bool   `json:"isMeta"`
	IsSidechain bool   `json:"isSidechain"`
	Message     struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	// Antigravity (transcript_full.jsonl): source/type/content por passo, sem "message".
	Source   string `json:"source"`
	Conteudo string `json:"content"`
	Criado   string `json:"created_at"`
	// Codex (rollout-*.jsonl): {"type":"response_item","payload":{"type":"message","role":…}}.
	Payload struct {
		Type    string `json:"type"`
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"payload"`
}

type bloco struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// Ultimas devolve até n mensagens, da mais antiga para a mais recente.
func Ultimas(caminho string, n int) ([]Mensagem, error) {
	f, err := os.Open(caminho)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	inicio := int64(0)
	if st.Size() > LeituraMaxima {
		inicio = st.Size() - LeituraMaxima
	}
	if _, err := f.Seek(inicio, io.SeekStart); err != nil {
		return nil, err
	}
	dados, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	if inicio > 0 {
		if i := bytes.IndexByte(dados, '\n'); i >= 0 {
			dados = dados[i+1:]
		}
	}
	return Extrair(dados, n), nil
}

func Extrair(dados []byte, n int) []Mensagem {
	var r []Mensagem
	for _, l := range bytes.Split(dados, []byte("\n")) {
		if len(bytes.TrimSpace(l)) == 0 {
			continue
		}
		var ln linha
		if json.Unmarshal(l, &ln) != nil || ln.IsMeta || ln.IsSidechain {
			continue
		}
		if ln.Source != "" {
			if m, ok := mensagemAgy(ln); ok {
				r = append(r, m)
			}
			continue
		}
		if ln.Type == "response_item" {
			r = append(r, mensagensCodex(ln)...)
			continue
		}
		if ln.Type != "user" && ln.Type != "assistant" {
			continue
		}
		r = append(r, mensagens(ln)...)
	}
	if len(r) > n {
		r = r[len(r)-n:]
	}
	return r
}

func mensagens(ln linha) []Mensagem {
	papel := "utilizador"
	if ln.Type == "assistant" {
		papel = "assistente"
	}
	var texto string
	if json.Unmarshal(ln.Message.Content, &texto) == nil {
		if m, ok := legivel(papel, texto, ln.Timestamp); ok {
			return []Mensagem{m}
		}
		return nil
	}
	var blocos []bloco
	if json.Unmarshal(ln.Message.Content, &blocos) != nil {
		return nil
	}
	var r []Mensagem
	for _, b := range blocos {
		switch b.Type {
		case "text":
			if m, ok := legivel(papel, b.Text, ln.Timestamp); ok {
				r = append(r, m)
			}
		case "tool_use":
			r = append(r, Mensagem{Papel: "ferramenta", Texto: descreverFerramenta(b), Em: ln.Timestamp})
		}
	}
	return r
}

// mensagemAgy lê um passo do Antigravity: o pedido vem dentro de <USER_REQUEST>, a resposta
// é o texto de um PLANNER_RESPONSE (os passos só com chamadas a ferramentas não têm texto).
func mensagemAgy(ln linha) (Mensagem, bool) {
	switch {
	case ln.Source == "USER_EXPLICIT" && ln.Type == "USER_INPUT":
		t := ln.Conteudo
		if i := strings.Index(t, "<USER_REQUEST>"); i >= 0 {
			t = t[i+len("<USER_REQUEST>"):]
			if j := strings.Index(t, "</USER_REQUEST>"); j >= 0 {
				t = t[:j]
			}
		}
		return legivel("utilizador", t, ln.Criado)
	case ln.Source == "MODEL" && ln.Type == "PLANNER_RESPONSE":
		return legivel("assistente", ln.Conteudo, ln.Criado)
	}
	return Mensagem{}, false
}

// mensagensCodex lê uma mensagem do Codex; as de "developer" e o contexto injetado como
// pedido (<environment_context>…) ficam de fora.
func mensagensCodex(ln linha) []Mensagem {
	papel := map[string]string{"user": "utilizador", "assistant": "assistente"}[ln.Payload.Role]
	if ln.Payload.Type != "message" || papel == "" {
		return nil
	}
	var r []Mensagem
	for _, c := range ln.Payload.Content {
		if c.Type != "input_text" && c.Type != "output_text" {
			continue
		}
		if m, ok := legivel(papel, c.Text, ln.Timestamp); ok {
			r = append(r, m)
		}
	}
	return r
}

// legivel descarta o que o harness injeta como pedido do utilizador (<task-notification>, <system-reminder>…).
func legivel(papel, texto, em string) (Mensagem, bool) {
	t := strings.TrimSpace(texto)
	if t == "" || (papel == "utilizador" && strings.HasPrefix(t, "<")) {
		return Mensagem{}, false
	}
	return Mensagem{Papel: papel, Texto: t, Em: em}, true
}

func descreverFerramenta(b bloco) string {
	var in map[string]any
	json.Unmarshal(b.Input, &in)
	for _, k := range []string{"file_path", "notebook_path", "command", "pattern", "url", "description", "prompt"} {
		if v, ok := in[k].(string); ok && v != "" {
			v = strings.Join(strings.Fields(v), " ")
			if r := []rune(v); len(r) > 120 {
				v = string(r[:119]) + "…"
			}
			return b.Name + " " + v
		}
	}
	return b.Name
}
