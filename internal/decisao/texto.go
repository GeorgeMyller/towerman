package decisao

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// janela: só o fim da resposta conta; uma pergunta a meio já foi ultrapassada pelo resto do texto.
const janela = 4096

// linhasFinais: o bloco final analisado (o pedido de escolha vive nas últimas linhas).
const linhasFinais = 14

var (
	reNegrito = regexp.MustCompile(`\*\*|__`)
	reCodigo  = regexp.MustCompile("`+")
	reCitacao = regexp.MustCompile(`^\s*>\s?`)
	reTitulo  = regexp.MustCompile(`^\s*#{1,6}\s+`)
)

// preparar devolve as últimas linhas não vazias do texto, depois do último bloco de código e sem
// Markdown de ênfase. As linhas de marcadores de lista mantêm-se (o menu precisa delas).
func preparar(texto string) []string {
	if len(texto) > janela {
		texto = texto[len(texto)-janela:]
		for !utf8.ValidString(texto) && len(texto) > 0 {
			texto = texto[1:]
		}
	}
	if i := strings.LastIndex(texto, "```"); i >= 0 {
		// Um bloco aberto e não fechado no fim é código, não pergunta: fica o que vem depois do último ```.
		texto = texto[i+3:]
		if j := strings.IndexByte(texto, '\n'); j >= 0 {
			texto = texto[j+1:]
		} else {
			texto = ""
		}
	}
	var linhas []string
	for _, l := range strings.Split(texto, "\n") {
		l = limpar(l)
		if l != "" {
			linhas = append(linhas, l)
		}
	}
	if len(linhas) > linhasFinais {
		linhas = linhas[len(linhas)-linhasFinais:]
	}
	return linhas
}

func limpar(l string) string {
	l = reCitacao.ReplaceAllString(l, "")
	l = reTitulo.ReplaceAllString(l, "")
	l = reNegrito.ReplaceAllString(l, "")
	l = reCodigo.ReplaceAllString(l, "")
	return strings.TrimSpace(l)
}

var (
	rePalavrasPT = regexp.MustCompile(`(?i)\b(que|não|nao|para|você|voce|com|uma|quer|posso|os|as|está|esta|do|da)\b`)
	rePalavrasEN = regexp.MustCompile(`(?i)\b(the|you|to|and|is|with|should|want|can|this|that|of|it)\b`)
)

// detetarLingua escolhe entre português e inglês pela frequência de palavras funcionais.
func detetarLingua(linhas []string) string {
	t := strings.Join(linhas, " ")
	if len(rePalavrasEN.FindAllString(t, -1)) > len(rePalavrasPT.FindAllString(t, -1)) {
		return "en"
	}
	return "pt"
}

func cortar(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:n-1])) + "…"
}

// frases parte uma linha nas frases que a compõem, mantendo a pontuação final.
func frases(l string) []string {
	var r []string
	inicio := 0
	for i, c := range l {
		if c == '.' || c == '!' || c == '?' {
			fim := i + 1
			if fim == len(l) || l[fim] == ' ' {
				if f := strings.TrimSpace(l[inicio:fim]); f != "" {
					r = append(r, f)
				}
				inicio = fim
			}
		}
	}
	if f := strings.TrimSpace(l[inicio:]); f != "" {
		r = append(r, f)
	}
	return r
}
