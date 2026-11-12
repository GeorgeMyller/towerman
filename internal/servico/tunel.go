package servico

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/GeorgeMyller/towerman/internal/config"
)

// Acesso fora de casa (desligado por omissão):
//  1. Relay WebSocket próprio, com criptografia ponta a ponta (X25519 + AES-256-GCM) e economia de
//     cota (silêncio com o ecrã fechado). Liga-se só quando há um endereço: TOWERMAN_RELAY_URL ou
//     relay_url no config.json, por esta ordem. Não há relay público por omissão; cada pessoa
//     publica o seu (cmd/towerman-relay). O token do host vem de TOWERMAN_RELAY_TOKEN ou de
//     relay_token no config.json.
//  2. Sem relay, o túnel rápido da Cloudflare (trycloudflare.com) só arranca com
//     TOWERMAN_TUNEL=cloudflare.
//  3. Sem nenhum dos dois, o servidor fica só na rede local.

// urlTunel é o endereço público atual ("" sem túnel/relay).
var urlTunel atomic.Value

func enderecoTunel() string {
	u, _ := urlTunel.Load().(string)
	return u
}

var reURLTunel = regexp.MustCompile(`https://[a-z0-9-]+\.trycloudflare\.com`)

func (s *Servico) iniciarTunel(ctx context.Context) {
	if os.Getenv("TOWERMAN_TUNEL") == "nao" || s.op.PortaRede <= 0 {
		return
	}
	if relayURL := enderecoRelay(s.op.Home); relayURL != "" {
		s.iniciarRelay(ctx, relayURL)
		return
	}
	if os.Getenv("TOWERMAN_TUNEL") != "cloudflare" {
		log.Printf("towerman: acesso fora de casa desligado (defina relay_url no config.json ou TOWERMAN_RELAY_URL)")
		return
	}
	go func() {
		espera := 5 * time.Second
		for ctx.Err() == nil {
			bin, err := cloudflared(s.op.Home)
			if err != nil {
				log.Printf("towerman: túnel desligado: %v", err)
			} else {
				inicio := time.Now()
				correrTunel(ctx, bin, s.PortaRede())
				if time.Since(inicio) > 5*time.Minute {
					espera = 5 * time.Second
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(espera):
			}
			espera = min(espera*2, 5*time.Minute)
		}
	}()
}

// enderecoRelay devolve o relay configurado ("" quando o acesso pelo relay está desligado).
func enderecoRelay(home string) string {
	if u := strings.TrimSpace(os.Getenv("TOWERMAN_RELAY_URL")); u != "" {
		return u
	}
	return config.LerPreferenciasEm(home).RelayURL
}

// tokenRelay devolve o token do host para o relay: TOWERMAN_RELAY_TOKEN ou relay_token.
func tokenRelay(home string) string {
	if t := strings.TrimSpace(os.Getenv("TOWERMAN_RELAY_TOKEN")); t != "" {
		return t
	}
	return config.LerPreferenciasEm(home).RelayToken
}

// correrTunel corre o cloudflared até ele sair e publica o endereço que ele anuncia.
func correrTunel(ctx context.Context, bin string, porta int) {
	cmd := exec.CommandContext(ctx, bin, "tunnel", "--no-autoupdate", "--url", "http://127.0.0.1:"+strconv.Itoa(porta))
	saida, err := cmd.StderrPipe()
	if err != nil {
		return
	}
	cmd.Stdout = io.Discard
	if err := cmd.Start(); err != nil {
		log.Printf("towerman: túnel não arrancou: %v", err)
		return
	}
	sc := bufio.NewScanner(saida)
	for sc.Scan() {
		if u := reURLTunel.FindString(sc.Text()); u != "" && u != enderecoTunel() {
			urlTunel.Store(u)
			esquecerEnderecos()
			log.Printf("towerman: túnel ativo em %s", u)
		}
	}
	cmd.Wait()
	urlTunel.Store("")
	esquecerEnderecos()
}

// PrepararTunel deixa o cloudflared pronto quando o fallback TOWERMAN_TUNEL=cloudflare está ativo.
func PrepararTunel(home string) (string, error) {
	if os.Getenv("TOWERMAN_TUNEL") != "cloudflare" {
		return "", nil
	}
	return cloudflared(home)
}

// cloudflared encontra o comando (PATH, Homebrew) ou descarrega-o uma vez para <home>/bin.
func cloudflared(home string) (string, error) {
	if p, err := exec.LookPath("cloudflared"); err == nil {
		return p, nil
	}
	for _, p := range []string{"/opt/homebrew/bin/cloudflared", "/usr/local/bin/cloudflared"} {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	dest := filepath.Join(home, "bin", "cloudflared")
	if _, err := os.Stat(dest); err == nil {
		return dest, nil
	}
	return dest, descarregarCloudflared(dest)
}

// descarregarCloudflared baixa a versão mais recente do GitHub da Cloudflare: um .tgz no macOS,
// o binário simples no Linux.
func descarregarCloudflared(dest string) error {
	nome := fmt.Sprintf("cloudflared-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "darwin" {
		nome += ".tgz"
	}
	cli := &http.Client{Timeout: 3 * time.Minute}
	resp, err := cli.Get("https://github.com/cloudflare/cloudflared/releases/latest/download/" + nome)
	if err != nil {
		return fmt.Errorf("descarregar cloudflared: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("descarregar cloudflared: %s", resp.Status)
	}
	var corpo io.Reader = resp.Body
	if runtime.GOOS == "darwin" {
		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			return err
		}
		tr := tar.NewReader(gz)
		for {
			h, err := tr.Next()
			if err != nil {
				return fmt.Errorf("cloudflared não veio no pacote: %w", err)
			}
			if filepath.Base(h.Name) == "cloudflared" {
				break
			}
		}
		corpo = tr
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp := dest + ".parcial"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, corpo); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	f.Close()
	log.Printf("towerman: cloudflared descarregado para %s", dest)
	return os.Rename(tmp, dest)
}
