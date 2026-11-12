package servico

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/GeorgeMyller/towerman/internal/relay"
)

// Sem config.json nem variáveis, não há relay: o acesso fora de casa fica desligado.
func TestRelayDesligadoPorOmissao(t *testing.T) {
	t.Setenv("TOWERMAN_RELAY_URL", "")
	t.Setenv("TOWERMAN_RELAY_TOKEN", "")
	h := t.TempDir()
	if u := enderecoRelay(h); u != "" {
		t.Fatalf("esperava relay desligado, obteve %q", u)
	}
	if tk := tokenRelay(h); tk != "" {
		t.Fatalf("esperava token vazio, obteve %q", tk)
	}

	os.WriteFile(filepath.Join(h, "config.json"), []byte(`{"relay_url":"https://r.example.com","relay_token":"do-config"}`), 0o600)
	if enderecoRelay(h) != "https://r.example.com" || tokenRelay(h) != "do-config" {
		t.Fatalf("config.json ignorado: %q %q", enderecoRelay(h), tokenRelay(h))
	}
	t.Setenv("TOWERMAN_RELAY_URL", "https://env.example.com")
	t.Setenv("TOWERMAN_RELAY_TOKEN", "do-env")
	if enderecoRelay(h) != "https://env.example.com" || tokenRelay(h) != "do-env" {
		t.Fatalf("variáveis deviam ter precedência: %q %q", enderecoRelay(h), tokenRelay(h))
	}
}

func TestEsperasRelayComTokenRecusado(t *testing.T) {
	recusa := &relay.ErroHandshake{Status: http.StatusUnauthorized, Texto: "401 Unauthorized"}
	agora, depois := esperasRelay(esperaRelayMin, recusa)
	if agora != esperaTokenRecusado || depois != 2*esperaTokenRecusado {
		t.Fatalf("401: agora=%s depois=%s", agora, depois)
	}
	if agora, depois = esperasRelay(40*time.Minute, recusa); agora != 40*time.Minute || depois != esperaTokenRecusadoMax {
		t.Fatalf("401 com teto: agora=%s depois=%s", agora, depois)
	}
	outro := &relay.ErroHandshake{Status: http.StatusBadGateway, Texto: "502"}
	if agora, depois = esperasRelay(esperaRelayMin, outro); agora != esperaRelayMin || depois != 2*esperaRelayMin {
		t.Fatalf("502: agora=%s depois=%s", agora, depois)
	}
	if _, depois = esperasRelay(esperaRelayMax, errors.New("rede")); depois != esperaRelayMax {
		t.Fatalf("teto normal: %s", depois)
	}
}

// O cliente manda o token no header X-Towerman-Relay-Token; sem ele, o relay fechado devolve 401.
func TestClienteRelayEnviaToken(t *testing.T) {
	a := novoAmbiente(t, nil)
	srvRelay := relay.NovoServidor(nil)
	srvRelay.ExigirTokens([]string{"tk-certo"})
	ts := httptest.NewServer(srvRelay.Handler())
	defer ts.Close()
	urlSala := normalizarURLRelay(ts.URL, a.srv.aparelhos.servidor)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for _, tk := range []string{"", "tk-errado"} {
		if err := a.srv.correrSessaoRelay(ctx, urlSala, "segredo-do-host-com-32-caracteres", tk); !tokenRecusado(err) {
			t.Fatalf("token %q: esperava 401, obteve %v", tk, err)
		}
	}

	feito := make(chan error, 1)
	go func() {
		feito <- a.srv.correrSessaoRelay(ctx, urlSala, "segredo-do-host-com-32-caracteres", "tk-certo")
	}()
	for enderecoTunel() != urlSala {
		select {
		case err := <-feito:
			t.Fatalf("token certo recusado: %v", err)
		case <-ctx.Done():
			t.Fatal("o host não ligou ao relay")
		case <-time.After(10 * time.Millisecond):
		}
	}
	// Um segundo host com o mesmo segredo e token substitui o primeiro, que sai da sessão.
	outro, err := relay.LigarWS(ctx, urlSala+"/ws?role=host", map[string]string{
		"X-Towerman-Host-Secret":  "segredo-do-host-com-32-caracteres",
		relay.CabecalhoTokenRelay: "tk-certo",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer outro.Fechar()
	if err := <-feito; err != nil {
		t.Fatalf("sessão terminou com erro: %v", err)
	}
}
