// Package decisao reconhece a decisão em texto livre com que um agente termina o turno
// («Digite CONTINUAR», um menu numerado, «Posso começar?», «Qual prefere?») e converte-a em
// pergunta com opções para o relógio e o iPhone. É genérico: não conhece agentes nem frameworks,
// só o texto final da resposta. Função pura, sem estado nem I/O.
package decisao

// Padrões reconhecidos, por ordem de confiança.
const (
	Comando = "comando" // «Digite **CONTINUAR**»: um botão por palavra pedida
	Menu    = "menu"    // lista numerada com pedido de escolha: um botão por item
	SimNao  = "sim_nao" // pergunta fechada: Sim e Não (Yes e No)
	Aberta  = "aberta"  // pergunta sem opções reconhecidas: só «Responder»
)

const (
	maxOpcoes   = 6
	maxPergunta = 300
	maxRotulo   = 40
)

// Opcao é um botão: o rótulo vai para o ecrã, o envio é o texto entregue à sessão.
type Opcao struct {
	Rotulo string
	Envio  string
}

// Decisao é o resultado da deteção. Lingua é "pt" ou "en".
type Decisao struct {
	Pergunta string
	Opcoes   []Opcao
	Padrao   string
	Lingua   string
}

// Detetar devolve a decisão pendente no fim de texto, se houver. A ordem é fixa: palavras-comando
// e menus (juntos, quando o menu também aceita uma palavra), depois sim/não, depois pergunta aberta.
func Detetar(texto string) (Decisao, bool) {
	linhas := preparar(texto)
	if len(linhas) == 0 {
		return Decisao{}, false
	}
	lingua := detetarLingua(linhas)
	palavras, linhaComando := comandos(linhas)

	if opcoes, pergunta, ok := menu(linhas); ok {
		for _, p := range palavras {
			opcoes = juntar(opcoes, Opcao{p, p})
		}
		return Decisao{Pergunta: cortar(pergunta, maxPergunta), Opcoes: limitar(opcoes), Padrao: Menu, Lingua: lingua}, true
	}
	if len(palavras) > 0 {
		var opcoes []Opcao
		for _, p := range palavras {
			opcoes = juntar(opcoes, Opcao{p, p})
		}
		return Decisao{Pergunta: cortar(linhaComando, maxPergunta), Opcoes: limitar(opcoes), Padrao: Comando, Lingua: lingua}, true
	}
	if pergunta, fechada, ok := perguntaFinal(linhas); ok {
		d := Decisao{Pergunta: cortar(pergunta, maxPergunta), Padrao: Aberta, Lingua: lingua}
		if fechada {
			d.Padrao = SimNao
			d.Opcoes = simNao(lingua)
		}
		return d, true
	}
	if pergunta, ok := variasPerguntas(linhas); ok {
		return Decisao{Pergunta: cortar(pergunta, maxPergunta), Padrao: Aberta, Lingua: lingua}, true
	}
	return Decisao{}, false
}

func juntar(opcoes []Opcao, o Opcao) []Opcao {
	for _, x := range opcoes {
		if x.Envio == o.Envio {
			return opcoes
		}
	}
	return append(opcoes, o)
}

func limitar(opcoes []Opcao) []Opcao {
	if len(opcoes) > maxOpcoes {
		return opcoes[:maxOpcoes]
	}
	return opcoes
}

func simNao(lingua string) []Opcao {
	if lingua == "en" {
		return []Opcao{{"Yes", "Yes"}, {"No", "No"}}
	}
	return []Opcao{{"Sim", "Sim"}, {"Não", "Não"}}
}
