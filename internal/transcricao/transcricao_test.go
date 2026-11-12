package transcricao

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const amostra = `{"type":"mode"}
{"type":"user","timestamp":"t1","message":{"role":"user","content":"corrige o IVA"}}
{"type":"user","timestamp":"t1b","isMeta":true,"message":{"role":"user","content":[{"type":"text","text":"meta"}]}}
{"type":"assistant","timestamp":"t2","message":{"content":[{"type":"thinking","thinking":"hmm"}]}}
{"type":"assistant","timestamp":"t3","message":{"content":[{"type":"tool_use","name":"Edit","input":{"file_path":"/r/carrinho.py","old_string":"a"}}]}}
{"type":"user","timestamp":"t4","message":{"content":[{"type":"tool_result","content":"ok"}]}}
{"type":"assistant","timestamp":"t5","isSidechain":true,"message":{"content":[{"type":"text","text":"subagente"}]}}
{"type":"user","timestamp":"t6","message":{"content":"<task-notification>\n<status>x</status>"}}
isto não é json
{"type":"assistant","timestamp":"t7","message":{"content":[{"type":"text","text":"Feito."},{"type":"tool_use","name":"Bash","input":{"command":"git   commit\n -m x"}}]}}
`

func TestExtrair(t *testing.T) {
	ms := Extrair([]byte(amostra), 30)
	esperado := []Mensagem{
		{"utilizador", "corrige o IVA", "t1"},
		{"ferramenta", "Edit /r/carrinho.py", "t3"},
		{"assistente", "Feito.", "t7"},
		{"ferramenta", "Bash git commit -m x", "t7"},
	}
	if len(ms) != len(esperado) {
		t.Fatalf("esperava %d mensagens, obtive %d: %+v", len(esperado), len(ms), ms)
	}
	for i := range esperado {
		if ms[i] != esperado[i] {
			t.Errorf("mensagem %d: %+v, esperado %+v", i, ms[i], esperado[i])
		}
	}
	if ult := Extrair([]byte(amostra), 2); len(ult) != 2 || ult[1].Texto != "Bash git commit -m x" {
		t.Fatalf("limite não respeitado: %+v", ult)
	}
}

func TestUltimasLeSoOFim(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	enchimento := strings.Repeat(`{"type":"user","timestamp":"velho","message":{"content":"antigo"}}`+"\n", LeituraMaxima/60)
	os.WriteFile(p, []byte(enchimento+amostra), 0o600)
	ms, err := Ultimas(p, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 3 || ms[2].Texto != "Bash git commit -m x" {
		t.Fatalf("mensagens inesperadas: %+v", ms)
	}
	if _, err := Ultimas(filepath.Join(t.TempDir(), "nao-existe"), 3); err == nil {
		t.Fatal("esperava erro para ficheiro inexistente")
	}
}

func TestExtrairAntigravity(t *testing.T) {
	dados := []byte(`{"step_index":"0","source":"USER_EXPLICIT","type":"USER_INPUT","created_at":"2026-10-27T18:12:02Z","content":"<USER_REQUEST>\nolá\n</USER_REQUEST>\n<ADDITIONAL_METADATA>\nhora\n</ADDITIONAL_METADATA>"}
{"step_index":"1","source":"SYSTEM_SDK","type":"SYSTEM_MESSAGE","content":"The following is a <SYSTEM_MESSAGE>"}
{"step_index":"2","source":"MODEL","type":"PLANNER_RESPONSE","tool_calls":[{"name":"view_file"}]}
{"step_index":"3","source":"MODEL","type":"GENERIC","content":"Created At: 2026-10-27T18:12:06Z"}
{"step_index":"4","source":"MODEL","type":"PLANNER_RESPONSE","created_at":"2026-10-27T18:12:06Z","content":"Olá. Informe a tarefa."}
`)
	ms := Extrair(dados, 10)
	if len(ms) != 2 || ms[0].Papel != "utilizador" || ms[0].Texto != "olá" || ms[1].Papel != "assistente" || ms[1].Texto != "Olá. Informe a tarefa." {
		t.Fatalf("mensagens = %+v", ms)
	}
}

func TestExtrairCodex(t *testing.T) {
	dados := []byte(`{"timestamp":"t0","type":"session_meta","payload":{"id":"x"}}
{"timestamp":"t1","type":"response_item","payload":{"type":"message","role":"developer","content":[{"type":"input_text","text":"You are Codex"}]}}
{"timestamp":"t2","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<environment_context>\n<cwd>/x</cwd>"}]}}
{"timestamp":"t3","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"hi\n"}]}}
{"timestamp":"t4","type":"event_msg","payload":{"type":"item_completed"}}
{"timestamp":"t5","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Hi! How can I help?"}]}}
`)
	ms := Extrair(dados, 10)
	if len(ms) != 2 || ms[0].Papel != "utilizador" || ms[0].Texto != "hi" || ms[1].Papel != "assistente" || ms[1].Texto != "Hi! How can I help?" {
		t.Fatalf("mensagens = %+v", ms)
	}
}
