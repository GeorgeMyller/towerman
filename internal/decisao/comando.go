package decisao

import (
	"regexp"
	"strings"
)

// rePalavraComando apanha «Digite **CONTINUAR**», «digite NOVA», «Type **PROCEED**»: uma palavra
// em maiúsculas (4+ letras) a seguir ao verbo. As linhas já vêm sem negrito, por isso a palavra
// conta pelo verbo e pelas maiúsculas.
var rePalavraComando = regexp.MustCompile(`(?i:\b(?:digite|escreva|responda(?:\s+com)?|type|reply(?:\s+with)?|enter)\s+)([A-ZÀ-Ý]{4,})\b`)

// rePalavraSolta: «… ou apenas digite CONTINUAR» já está coberto acima; «ou NOVA» a seguir a uma
// palavra-comando também conta (ex.: «digite CONTINUAR ou NOVA»).
var rePalavraSolta = regexp.MustCompile(`\b(?:ou|or)\s+([A-ZÀ-Ý]{4,})\b`)

// reAspas apanha um trecho citado («…», "…", “…”): uma palavra-comando lá dentro é exemplo, não pedido
// (ex.: «Termine com "Digite CONTINUAR para seguir."»).
var reAspas = regexp.MustCompile(`«[^»]*»|"[^"]*"|“[^”]*”`)

// comandos devolve as palavras-comando das últimas linhas e a última linha onde aparecem.
// Uma palavra pedida a meio da resposta (já ultrapassada) não conta: só as 6 últimas linhas.
func comandos(linhas []string) ([]string, string) {
	inicio := 0
	if len(linhas) > 6 {
		inicio = len(linhas) - 6
	}
	var palavras []string
	linha := ""
	for _, l := range linhas[inicio:] {
		fora := reAspas.ReplaceAllString(l, " ")
		achou := false
		for _, m := range rePalavraComando.FindAllStringSubmatch(fora, -1) {
			palavras = acrescentar(palavras, m[1])
			achou = true
		}
		if achou {
			for _, m := range rePalavraSolta.FindAllStringSubmatch(fora, -1) {
				palavras = acrescentar(palavras, m[1])
			}
			linha = l
		}
	}
	return palavras, linha
}

func acrescentar(palavras []string, p string) []string {
	p = strings.ToUpper(p)
	for _, x := range palavras {
		if x == p {
			return palavras
		}
	}
	return append(palavras, p)
}
