package decisao

import (
	"regexp"
	"strconv"
	"strings"
)

// reItem: «1. texto», «1) texto», «[1] texto», «a) texto». O negrito já foi tirado em preparar.
var reItem = regexp.MustCompile(`^(?:\[(\d{1,2})\]|(\d{1,2})[.)]|([a-f])\))\s+(.+)$`)

// Pedido de escolha antes da lista: uma pergunta ou um verbo de escolha no imperativo.
var reEscolhaAntes = regexp.MustCompile(`(?i)\?\s*$|\b(escolha|escolhe|digite|pressione|selecione|responda com|choose|pick|select|type|press)\b`)

// Pedido de escolha depois da lista: uma pergunta com pronome interrogativo ou «opção»
// («Qual opção prefere?»), ou um verbo de escolha no imperativo («Responda com a letra»).
// «Recomendo a opção 1.» não pede nada.
var (
	reEscolhaDepois = regexp.MustCompile(`(?i)\b(qual|quais|escolha|escolhe|prefere|preferes|opç[ãa]o|opç[õo]es|algum dos|alguma das|which|option|prefer)\b`)
	reImperativo    = regexp.MustCompile(`(?i)\b(escolha|escolhe|digite|pressione|selecione|responda com|choose|pick|select|type|press)\b`)
)

// pedeEscolha diz se a linha depois da lista pede uma escolha. Uma palavra-comando («Digite
// CONTINUAR») não escolhe entre os itens: tira-se antes de procurar o verbo.
func pedeEscolha(l string) bool {
	l = rePalavraComando.ReplaceAllString(l, "")
	return reImperativo.MatchString(l) || strings.Contains(l, "?") && reEscolhaDepois.MatchString(l)
}

// reNumerosPedidos: «Digite 1, 2 ou 3», «Pressione 1 ou 2», «Type 1 or 2» sem lista visível.
var reNumerosPedidos = regexp.MustCompile(`(?i)\b(?:digite|pressione|escolha|responda|type|press|choose|reply)\s+((?:\d{1,2}\s*,\s*)*\d{1,2}\s+(?:ou|or)\s+\d{1,2})\b`)
var reNumero = regexp.MustCompile(`\d{1,2}`)

type item struct {
	chave, texto string
	indice       int // posição em linhas
}

// menu procura a última lista numerada (ou com letras) do bloco final com um pedido de escolha
// junto dela. Devolve um botão por item: rótulo «n — texto», envio «n».
func menu(linhas []string) ([]Opcao, string, bool) {
	itens := ultimaLista(linhas)
	if len(itens) >= 2 {
		depois := linhas[itens[len(itens)-1].indice+1:]
		// Depois da lista só cabem 3 linhas: mais do que isso e a lista já não é o fim da resposta.
		if len(depois) <= 3 {
			if perguntas := contarPerguntas(itens); perguntas >= 2 {
				return nil, "", false // várias perguntas numeradas: não cabem em botões
			}
			pergunta := ""
			for _, l := range depois {
				if pedeEscolha(l) {
					pergunta = l
				}
			}
			if pergunta == "" && itens[0].indice > 0 {
				if antes := linhas[itens[0].indice-1]; reEscolhaAntes.MatchString(antes) {
					pergunta = antes
				}
			}
			if pergunta != "" {
				var opcoes []Opcao
				for _, it := range itens {
					opcoes = append(opcoes, Opcao{Rotulo: cortar(it.chave+" — "+tituloItem(it.texto), maxRotulo), Envio: it.chave})
				}
				return opcoes, pergunta, true
			}
		}
	}
	// Sem lista: «Digite 1, 2 ou 3» nas duas últimas linhas ainda é um menu, só com números.
	for i := len(linhas) - 1; i >= 0 && i >= len(linhas)-2; i-- {
		if m := reNumerosPedidos.FindStringSubmatch(linhas[i]); m != nil {
			var opcoes []Opcao
			for _, n := range reNumero.FindAllString(m[1], -1) {
				opcoes = juntar(opcoes, Opcao{n, n})
			}
			return opcoes, linhas[i], true
		}
	}
	return nil, "", false
}

// ultimaLista devolve a última sequência de itens consecutivos (1, 2, 3… ou a, b, c…). Linhas de
// continuação entre itens são toleradas; a lista acaba no último item.
func ultimaLista(linhas []string) []item {
	var atual, ultima []item
	for i, l := range linhas {
		m := reItem.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		chave := m[1] + m[2] + m[3]
		if len(atual) > 0 && !segue(atual[len(atual)-1].chave, chave) {
			atual = nil
		}
		if len(atual) == 0 && chave != "1" && chave != "a" {
			continue
		}
		atual = append(atual, item{chave: chave, texto: m[4], indice: i})
		ultima = atual
	}
	return ultima
}

func segue(anterior, chave string) bool {
	a, errA := strconv.Atoi(anterior)
	b, errB := strconv.Atoi(chave)
	if errA == nil && errB == nil {
		return b == a+1
	}
	return len(anterior) == 1 && len(chave) == 1 && chave[0] == anterior[0]+1
}

func contarPerguntas(itens []item) int {
	n := 0
	for _, it := range itens {
		if strings.HasSuffix(strings.TrimSpace(it.texto), "?") {
			n++
		}
	}
	return n
}

// tituloItem: num item «Sincronizar parcial: gero agora o adendo…», o título é o que vem antes dos «:».
func tituloItem(t string) string {
	if i := strings.Index(t, ":"); i > 0 && i <= maxRotulo {
		return strings.TrimSpace(t[:i])
	}
	return t
}
