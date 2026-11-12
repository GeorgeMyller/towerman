package servico

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/GeorgeMyller/towerman/internal/quadro"
)

func TestRitmoAbrandaEVolta(t *testing.T) {
	r := novoRitmo(time.Minute, 5*time.Minute)
	var vistos []time.Duration
	for i := 0; i < 4; i++ {
		vistos = append(vistos, r.seguinte(false))
	}
	if fmt.Sprint(vistos) != "[2m0s 4m0s 5m0s 5m0s]" {
		t.Fatalf("abrandamento inesperado: %v", vistos)
	}
	if !r.acordar() || r.atual != time.Minute {
		t.Fatalf("acordar não voltou à base: %v", r.atual)
	}
	if r.acordar() {
		t.Fatal("acordar na base não devia pedir para rearmar")
	}
	r.seguinte(false)
	if d := r.seguinte(true); d != time.Minute {
		t.Fatalf("com trabalho devia voltar à base, deu %v", d)
	}
	if r := novoRitmo(time.Minute, time.Second); r.seguinte(false) != time.Minute {
		t.Fatal("um teto abaixo da base devia ficar na base")
	}
}

func TestDiarioPendente(t *testing.T) {
	home := t.TempDir()
	if diarioPendente(home) {
		t.Fatal("sem diário não há nada pendente")
	}
	atual := filepath.Join(home, "diario.jsonl")
	os.WriteFile(atual, nil, 0o600)
	os.WriteFile(filepath.Join(home, "diario.rejeitado.jsonl"), []byte("x\n"), 0o600)
	if diarioPendente(home) {
		t.Fatal("um diário vazio e o rejeitado não contam como pendentes")
	}
	os.WriteFile(atual, []byte("{}\n"), 0o600)
	if !diarioPendente(home) {
		t.Fatal("o diário com linhas devia contar")
	}
	os.Remove(atual)
	os.WriteFile(filepath.Join(home, "diario.123.jsonl"), []byte("{}\n"), 0o600)
	if !diarioPendente(home) {
		t.Fatal("um resto renomeado devia contar")
	}
}

func TestDiarioVazioNaoERenomeado(t *testing.T) {
	home := t.TempDir()
	os.WriteFile(filepath.Join(home, "diario.jsonl"), nil, 0o600)
	novoAmbiente(t, func(op *Opcoes) { op.Home = home; op.IntervaloDiario = 10 * time.Millisecond })
	time.Sleep(200 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(home, "diario.jsonl")); err != nil {
		t.Fatalf("o diário vazio não devia ser mexido: %v", err)
	}
	if restos, _ := filepath.Glob(filepath.Join(home, "diario.[0-9]*.jsonl")); len(restos) != 0 {
		t.Fatalf("não devia haver renomeados: %v", restos)
	}
}

// Depois de ~1,5 s parado, o próximo disparo fica a ~1 s; só o evento o traz antes do prazo.
const ocioso, prazoAcordado = 1500 * time.Millisecond, 500 * time.Millisecond

func TestEventoAcordaDiarioAbrandado(t *testing.T) {
	a := novoAmbiente(t, func(op *Opcoes) {
		op.IntervaloDiario = 20 * time.Millisecond
		op.IntervaloDiarioMax = time.Hour
	})
	time.Sleep(ocioso)
	linha := envelope(map[string]any{"session_id": "dia", "hook_event_name": "Stop"}, "2026-10-07T11:00:00Z")
	os.WriteFile(filepath.Join(a.home, "diario.jsonl"), append(linha, '\n'), 0o600)
	a.enviar(map[string]any{"session_id": "outra", "hook_event_name": "Stop"})
	prazo := time.Now().Add(prazoAcordado)
	for {
		a.esperar()
		if _, ok, _ := a.srv.Quadro().ObterSessao("dia"); ok {
			return
		}
		if time.Now().After(prazo) {
			t.Fatal("o evento não acordou a ingestão do diário")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestEventoAcordaVigiaAbrandada(t *testing.T) {
	a := novoAmbiente(t, func(op *Opcoes) {
		op.IntervaloVigia = 20 * time.Millisecond
		op.IntervaloVigiaMax = time.Hour
	})
	time.Sleep(ocioso)
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Run()
	morto := cmd.Process.Pid
	reg, _ := json.Marshal(map[string]any{"pid": morto, "sessionId": "m"})
	os.WriteFile(filepath.Join(a.claude, "sessions", fmt.Sprint(morto)+".json"), reg, 0o600)
	a.enviar(map[string]any{"session_id": "m", "hook_event_name": "SessionStart", "cwd": t.TempDir()})
	prazo := time.Now().Add(prazoAcordado)
	a.esperar()
	for a.sessao("m").Estado != quadro.Terminada {
		if time.Now().After(prazo) {
			t.Fatal("o evento não acordou a vigia abrandada")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
