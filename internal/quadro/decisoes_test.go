package quadro

import (
	"path/filepath"
	"testing"
	"time"
)

func TestDecisoes(t *testing.T) {
	q, err := Abrir(filepath.Join(t.TempDir(), "quadro.db"))
	if err != nil {
		t.Fatal(err)
	}
	// No Windows, a pasta temporária só se apaga com a base fechada.
	t.Cleanup(func() { q.Fechar() })
	agora := time.Now()
	nova := Decisao{SessionID: "s1", Pergunta: "Posso começar?", Padrao: "sim_nao", Lingua: "pt",
		Opcoes: []OpcaoDecisao{{"Sim", "Sim"}, {"Não", "Não"}}}

	id1, err := q.AbrirDecisao(nova, agora)
	if err != nil {
		t.Fatal(err)
	}
	d, ok, err := q.DecisaoAberta("s1")
	if err != nil || !ok || d.ID != id1 || len(d.Opcoes) != 2 || d.Pergunta != "Posso começar?" {
		t.Fatalf("decisão aberta: %+v %v %v", d, ok, err)
	}

	// Uma decisão nova na mesma sessão substitui a anterior: só pode haver uma aberta.
	id2, err := q.AbrirDecisao(nova, agora)
	if err != nil {
		t.Fatal(err)
	}
	if d, _, _ := q.DecisaoAberta("s1"); d.ID != id2 {
		t.Fatalf("aberta %d, esperava a nova %d", d.ID, id2)
	}
	var desfecho, pergunta string
	q.db.QueryRow(`SELECT desfecho, pergunta FROM decisoes WHERE id = ?`, id1).Scan(&desfecho, &pergunta)
	if desfecho != DecisaoSubstituida || pergunta != "" {
		t.Fatalf("a anterior ficou %q com pergunta %q", desfecho, pergunta)
	}

	// Fecho condicional: o segundo fecho da mesma decisão não muda nada.
	if fechou, err := q.FecharDecisao(id2, DecisaoRespondida, Resposta{}, agora); err != nil || !fechou {
		t.Fatalf("primeiro fecho: %v %v", fechou, err)
	}
	if fechou, _ := q.FecharDecisao(id2, DecisaoDescartada, Resposta{}, agora); fechou {
		t.Fatal("segundo fecho não devia fechar")
	}
	var opcoes string
	q.db.QueryRow(`SELECT desfecho, pergunta, opcoes FROM decisoes WHERE id = ?`, id2).Scan(&desfecho, &pergunta, &opcoes)
	if desfecho != DecisaoRespondida || pergunta != "" || opcoes != "[]" {
		t.Fatalf("depois de fechar: %q %q %q", desfecho, pergunta, opcoes)
	}
	if _, ok, _ := q.DecisaoAberta("s1"); ok {
		t.Fatal("não devia haver decisão aberta")
	}
	if _, fechou, err := q.FecharDecisaoDaSessao("s1", DecisaoNoTerminal, Resposta{}, agora); err != nil || fechou {
		t.Fatalf("fechar sem decisão aberta: %v %v", fechou, err)
	}

	// A retenção apaga só as fechadas antigas.
	q.AbrirDecisao(Decisao{SessionID: "s2", Padrao: "aberta", Lingua: "pt"}, agora.Add(-48*time.Hour))
	if err := q.Reter(agora.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := q.DecisaoAberta("s2"); !ok {
		t.Fatal("a retenção apagou uma decisão aberta")
	}
}
