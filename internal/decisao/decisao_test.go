package decisao

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

// caso é um fim de turno real (anonimizado) com a decisão esperada; padrao vazio = sem decisão.
type caso struct {
	Nome   string   `json:"nome"`
	Texto  string   `json:"texto"`
	Padrao string   `json:"padrao"`
	Envios []string `json:"envios"`
}

func lerCasos(t *testing.T) []caso {
	t.Helper()
	b, err := os.ReadFile("testdata/fins.json")
	if err != nil {
		t.Fatal(err)
	}
	var cs []caso
	if err := json.Unmarshal(b, &cs); err != nil {
		t.Fatal(err)
	}
	if len(cs) < 30 {
		t.Fatalf("o conjunto tem %d fins de turno; são precisos pelo menos 30", len(cs))
	}
	return cs
}

func envios(d Decisao) []string {
	var r []string
	for _, o := range d.Opcoes {
		r = append(r, o.Envio)
	}
	return r
}

// TestPrecisao aplica as metas do requirements (RNF de precisão): menus explícitos (comando e
// menu) detetados em ≥ 95% dos casos, falsos positivos < 5%. Os outros casos são relatados.
func TestPrecisao(t *testing.T) {
	var menus, menusCertos, semDecisao, falsos int
	for _, c := range lerCasos(t) {
		d, ok := Detetar(c.Texto)
		padrao := ""
		if ok {
			padrao = d.Padrao
		}
		certo := padrao == c.Padrao && (c.Envios == nil || slices.Equal(envios(d), c.Envios))
		switch c.Padrao {
		case Comando, Menu:
			menus++
			if certo {
				menusCertos++
			}
		case "":
			semDecisao++
			if ok {
				falsos++
			}
		}
		if !certo {
			t.Logf("%s: esperava %q %v, veio %q %v (pergunta %q)", c.Nome, c.Padrao, c.Envios, padrao, envios(d), d.Pergunta)
		}
	}
	if menus == 0 || semDecisao == 0 {
		t.Fatal("o conjunto precisa de menus explícitos e de fins sem decisão")
	}
	if taxa := float64(menusCertos) / float64(menus); taxa < 0.95 {
		t.Errorf("menus explícitos detetados: %.0f%% (%d de %d); meta ≥ 95%%", taxa*100, menusCertos, menus)
	}
	if taxa := float64(falsos) / float64(semDecisao); taxa >= 0.05 {
		t.Errorf("falsos positivos: %.0f%% (%d de %d); meta < 5%%", taxa*100, falsos, semDecisao)
	}
}

func TestCasos(t *testing.T) {
	for _, c := range lerCasos(t) {
		t.Run(c.Nome, func(t *testing.T) {
			d, ok := Detetar(c.Texto)
			padrao := ""
			if ok {
				padrao = d.Padrao
			}
			if padrao != c.Padrao {
				t.Fatalf("padrão %q, esperava %q (pergunta %q)", padrao, c.Padrao, d.Pergunta)
			}
			if c.Envios != nil && !slices.Equal(envios(d), c.Envios) {
				t.Fatalf("envios %v, esperava %v", envios(d), c.Envios)
			}
			if ok && d.Pergunta == "" {
				t.Fatal("decisão sem pergunta")
			}
		})
	}
}

func TestLinguaDosBotoes(t *testing.T) {
	d, _ := Detetar("Should I push the branch?")
	if !slices.Equal(envios(d), []string{"Yes", "No"}) || d.Lingua != "en" {
		t.Fatalf("inglês: %v %s", envios(d), d.Lingua)
	}
	d, _ = Detetar("Posso fazer o push da branch?")
	if !slices.Equal(envios(d), []string{"Sim", "Não"}) || d.Lingua != "pt" {
		t.Fatalf("português: %v %s", envios(d), d.Lingua)
	}
}

func TestLimites(t *testing.T) {
	if _, ok := Detetar(""); ok {
		t.Fatal("texto vazio não é decisão")
	}
	// Só os últimos 4 KB contam: uma pergunta antiga, seguida de muito texto, já não está pendente.
	longo := "Posso começar?\n" + strings.Repeat("Linha de relatório sem pergunta.\n", 200)
	if _, ok := Detetar(longo); ok {
		t.Fatal("pergunta fora da janela final detetada")
	}
	d, ok := Detetar(strings.Repeat("palavra ", 100) + "posso continuar?")
	if !ok || len([]rune(d.Pergunta)) > maxPergunta {
		t.Fatalf("pergunta longa: %d runas", len([]rune(d.Pergunta)))
	}
	var itens []string
	for i := 1; i <= 9; i++ {
		itens = append(itens, strings.Repeat("x", 3)+" opção")
		itens[i-1] = string(rune('0'+i)) + ". " + itens[i-1]
	}
	d, ok = Detetar("Qual escolhe?\n" + strings.Join(itens, "\n"))
	if !ok || len(d.Opcoes) != maxOpcoes {
		t.Fatalf("menu com 9 itens deu %d opções; máximo %d", len(d.Opcoes), maxOpcoes)
	}
	for _, o := range d.Opcoes {
		if len([]rune(o.Rotulo)) > maxRotulo {
			t.Fatalf("rótulo com %d runas", len([]rune(o.Rotulo)))
		}
	}
}
