package decisao

import (
	"regexp"
	"strings"
)

// reFechadaPT e reFechadaEN: perguntas a que se responde com sim ou não, no início da frase.
var (
	reFechadaPT = regexp.MustCompile(`(?i)^(?:e\s+)?(?:posso|podemos|pode|quer|queres|deseja|devo|faço|faco|fazemos|avanço|avanco|sigo|continuo|aprova|aprovas|autoriza|confirma|prefere que|vamos|está bem|esta bem|ok)\b`)
	reFechadaEN = regexp.MustCompile(`(?i)^(?:so\s+|and\s+)?(?:should i|shall i|can i|may i|do you want|would you like|want me to|is it ok|is that ok|ok to|proceed|go ahead)\b`)
	// Uma pergunta com interrogativo ou alternativas não é sim/não: «Quer usar estes IDs e quantos…?»,
	// «Aprova a proposta, a alternativa, ou só a limpeza?».
	reAberta = regexp.MustCompile(`(?i)\b(qual|quais|quanto|quantos|quantas|como|onde|quando|que nome|what|which|how|where|when|who)\b|\s(ou|or)\s`)
)

// perguntaFinal devolve a última frase se a última linha terminar em «?»; fechada diz se é sim/não.
func perguntaFinal(linhas []string) (pergunta string, fechada, ok bool) {
	ultima := linhas[len(linhas)-1]
	if !strings.HasSuffix(ultima, "?") || reItem.MatchString(ultima) {
		return "", false, false
	}
	fs := frases(ultima)
	pergunta = fs[len(fs)-1]
	if reAberta.MatchString(pergunta) && !strings.Contains(strings.ToLower(pergunta), "ou não") {
		return pergunta, false, true
	}
	fechada = reFechadaPT.MatchString(pergunta) || reFechadaEN.MatchString(pergunta)
	return pergunta, fechada, true
}

// variasPerguntas: um bloco final com duas ou mais perguntas numeradas («Confirme três pontos:
// 1. …? 2. …?») pede resposta livre.
func variasPerguntas(linhas []string) (string, bool) {
	itens := ultimaLista(linhas)
	if contarPerguntas(itens) < 2 || len(linhas)-1-itens[len(itens)-1].indice > 3 {
		return "", false
	}
	if itens[0].indice > 0 {
		return linhas[itens[0].indice-1], true
	}
	return itens[0].texto, true
}
