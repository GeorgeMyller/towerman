package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/GeorgeMyller/towerman/internal/config"
	"github.com/GeorgeMyller/towerman/internal/servico"
)

// pedidoLocal fala com o serviço em 127.0.0.1 com o token do painel.
func pedidoLocal(metodo, caminho string, destino any) error {
	token, err := config.LerToken()
	if err != nil {
		return fmt.Errorf("sem token: corre «towerman install» primeiro")
	}
	req, _ := http.NewRequest(metodo, "http://127.0.0.1:"+strconv.Itoa(config.Porta())+caminho, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("o serviço não responde (towerman serve está a correr?): %w", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		var e struct {
			Erro string `json:"erro"`
		}
		json.Unmarshal(b, &e)
		if e.Erro == "" {
			e.Erro = resp.Status
		}
		return fmt.Errorf("%s", e.Erro)
	}
	return json.Unmarshal(b, destino)
}

// parear mostra o QR que o iPhone lê para se ligar a este computador.
func parear(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("pair", flag.ExitOnError)
	soLink := fs.Bool("link", false, "imprimir só o link towerman:// (para colar no app do Mac)")
	fs.Parse(args)
	var p struct {
		Codigo    string   `json:"codigo"`
		URL       string   `json:"url"`
		Nome      string   `json:"nome"`
		Porta     int      `json:"porta"`
		Enderecos []string `json:"enderecos"`
	}
	if err := pedidoLocal("POST", "/v1/pareamento", &p); err != nil {
		return err
	}
	if *soLink {
		fmt.Fprintln(out, p.URL)
		return nil
	}
	desenho, err := servico.QR(p.URL)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "\nParear o iPhone com %s\n\n%s\n", p.Nome, desenho)
	fmt.Fprintln(out, "No iPhone: aponte a câmara para o código, ou abra o Towerman › Adicionar computador.")
	fmt.Fprintf(out, "Código %s · vale 2 minutos e só serve uma vez.\n", p.Codigo)
	var locais []string
	tunel := ""
	for _, e := range p.Enderecos {
		if strings.HasPrefix(e, "https://") {
			tunel = e
		} else {
			locais = append(locais, e)
		}
	}
	fmt.Fprintf(out, "Na mesma rede: %s (porta %d)\n", strings.Join(locais, ", "), p.Porta)
	if tunel != "" {
		fmt.Fprintf(out, "Fora de casa: %s (o iPhone usa-o sozinho; pareie na mesma rede)\n\n", tunel)
	} else {
		fmt.Fprint(out, "Fora de casa: túnel ainda a abrir ou desligado (TOWERMAN_TUNEL=nao); corra «towerman pair» de novo daqui a pouco.\n\n")
	}
	return nil
}

// aparelhos lista os aparelhos pareados ou revoga um: «towerman aparelhos revogar <nome|id>».
func aparelhos(args []string, out io.Writer) error {
	if len(args) >= 2 && args[0] == "revogar" {
		var r map[string]any
		if err := pedidoLocal("POST", "/v1/aparelhos/"+args[1]+"/revogar", &r); err != nil {
			return err
		}
		fmt.Fprintf(out, "aparelho %s revogado\n", args[1])
		return nil
	}
	var lista []struct {
		ID       string `json:"id"`
		Nome     string `json:"nome"`
		CriadoEm string `json:"criado_em"`
		VistoEm  string `json:"visto_em"`
	}
	if err := pedidoLocal("GET", "/v1/aparelhos", &lista); err != nil {
		return err
	}
	if len(lista) == 0 {
		fmt.Fprintln(out, "nenhum aparelho pareado: corre «towerman pair»")
		return nil
	}
	for _, a := range lista {
		visto := a.VistoEm
		if visto == "" {
			visto = "nunca"
		}
		fmt.Fprintf(out, "%s  %-20s pareado %s · visto %s\n", a.ID, a.Nome, a.CriadoEm, visto)
	}
	return nil
}
