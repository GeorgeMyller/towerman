package servico

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/GeorgeMyller/towerman/internal/config"
	"github.com/GeorgeMyller/towerman/internal/quadro"
	"github.com/GeorgeMyller/towerman/internal/visao"
)

const (
	janelaColisao    = 24 * time.Hour
	silencioAvisos   = time.Hour
	regraMesmaArvore = 1
	regraMesmaBranch = 2
	regraOutraBranch = 3
)

// Resposta é o que a sessão recebe de volta do hook.
type Resposta struct {
	Contexto    string
	Bloquear    string
	PermissaoID int64
	// Decisao e Motivo respondem ao PreToolUse (regra 2): "ask" ou "deny".
	Decisao string
	Motivo  string
	// Passagens de fluxos disparadas por este Stop, a entregar fora do escritor.
	Passagens []passagemPendente
	// Fim é o aviso «terminou» para o telemóvel, quando a pessoa não está a olhar para a sessão.
	Fim *fimSessao
}

type fimSessao struct {
	SessionID string
	Titulo    string
	// Resposta é o fim da última resposta; vai cifrada no push, nunca em claro.
	Resposta string
}

func nomeOuID(nome, id string) string {
	if nome != "" {
		return nome
	}
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func curto(c string) string {
	if h, err := os.UserHomeDir(); err == nil && strings.HasPrefix(c, h) {
		return "~" + c[len(h):]
	}
	return c
}

// contextoEntrada aplica a regra 1 e apresenta as outras sessões vivas do mesmo repo.
func (p *Processador) contextoEntrada(s quadro.Sessao) (string, error) {
	if s.Repo == "" {
		return "", nil
	}
	outras, err := p.Quadro.SessoesVivasNoRepo(s.Repo, s.SessionID)
	if err != nil || len(outras) == 0 {
		return "", err
	}
	var b strings.Builder
	for _, o := range outras {
		if s.Worktree != "" && o.Worktree == s.Worktree {
			fmt.Fprintf(&b, "⚠ Towerman: a worktree %s já tem dona, a sessão «%s» (viva). Regra «uma árvore, uma dona»: "+
				"antes de editar ficheiros aqui, fala com ela (SendMessage para «%s») ou trabalha noutra worktree.\n\n",
				curto(s.Worktree), nomeOuID(o.Nome, o.SessionID), nomeOuID(o.Nome, o.SessionID))
		}
	}
	fmt.Fprintf(&b, "Towerman: neste repositório (%s) estão vivas outras sessões:\n", visao.NomeRepo(s.Repo))
	for _, o := range outras {
		fmt.Fprintf(&b, "- «%s» na branch %s (%s)", nomeOuID(o.Nome, o.SessionID), o.Branch, curto(o.Worktree))
		if ts, err := p.Quadro.ToquesRecentes(o.SessionID, 4); err == nil {
			var fs []string
			for _, t := range ts {
				if t.Tipo != "git" {
					fs = append(fs, t.Caminho)
				}
			}
			if len(fs) > 0 {
				fmt.Fprintf(&b, ", mexeu em %s", strings.Join(fs, ", "))
			}
		}
		b.WriteString("\n")
	}
	return b.String(), nil
}

// contextoEdicao aplica a regra 3 (e avisa, sem bloquear, a regra 2) depois de uma edição.
func (p *Processador) contextoEdicao(s quadro.Sessao, caminho string, agora time.Time) (string, error) {
	if s.Repo == "" || caminho == "" {
		return "", nil
	}
	alheios, err := p.Quadro.ToquesAlheios(s.Repo, caminho, s.SessionID, agora.Add(-janelaColisao))
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, t := range alheios {
		nome := nomeOuID(t.Nome, t.SessionID)
		regra, motivo := regraOutraBranch, fmt.Sprintf("Towerman: «%s» também foi alterado pela sessão «%s» na branch %s. "+
			"Não há risco agora, mas vai haver conflito quando as duas branches se juntarem.", caminho, nome, t.Branch)
		if t.Branch == s.Branch {
			regra, motivo = regraMesmaBranch, fmt.Sprintf("⚠ Towerman: «%s» foi editado %s pela sessão «%s», viva e na mesma branch (%s). "+
				"Fala com ela (SendMessage para «%s») antes de continuar a mexer neste ficheiro.", caminho, haTexto(t.Em, agora), nome, t.Branch, nome)
		}
		novo, err := p.Quadro.RegistarAvisoSeNovo(s.SessionID, t.SessionID, regra, caminho, motivo, agora.Add(-silencioAvisos), agora)
		if err != nil {
			return "", err
		}
		if novo {
			b.WriteString(motivo + "\n")
		}
	}
	return strings.TrimSpace(b.String()), nil
}

// decisaoRegra2 trava (ou pede confirmação) a edição de um ficheiro que outra sessão viva
// alterou na mesma branch nas últimas 24 h. Não deduplica: cada tentativa volta a ser travada.
func (p *Processador) decisaoRegra2(s quadro.Sessao, caminho string, agora time.Time) (decisao, motivo string, err error) {
	modo := config.Regra2Pedir
	if p.ModoRegra2 != nil {
		modo = p.ModoRegra2()
	}
	if modo == config.Regra2Off || s.Repo == "" || caminho == "" {
		return "", "", nil
	}
	alheios, err := p.Quadro.ToquesAlheios(s.Repo, caminho, s.SessionID, agora.Add(-janelaColisao))
	if err != nil {
		return "", "", err
	}
	for _, t := range alheios {
		if t.Branch != s.Branch {
			continue
		}
		nome := nomeOuID(t.Nome, t.SessionID)
		motivo = fmt.Sprintf("Towerman (regra 2): «%s» foi editado %s pela sessão «%s», que está viva e na mesma branch (%s). "+
			"Editar agora pode sobrepor o trabalho dela. Fala primeiro com ela (SendMessage para «%s»).",
			caminho, haTexto(t.Em, agora), nome, s.Branch, nome)
		if _, err := p.Quadro.RegistarAvisoSeNovo(s.SessionID, t.SessionID, regraMesmaBranch, caminho, motivo, agora.Add(-silencioAvisos), agora); err != nil {
			return "", "", err
		}
		return modo, motivo, nil
	}
	return "", "", nil
}

func haTexto(em string, agora time.Time) string {
	if h := visao.Ha(em, agora); h != "" && h != "agora" {
		return h
	}
	return "agora mesmo"
}

// mensagensEmFila entrega no Stop tudo o que está pendente para a sessão.
func (p *Processador) mensagensEmFila(s quadro.Sessao, agora time.Time) (string, error) {
	ms, err := p.Quadro.MensagensPendentes(s.SessionID)
	if err != nil || len(ms) == 0 {
		return "", err
	}
	var textos []string
	for _, m := range ms {
		textos = append(textos, m.Texto)
		if err := p.Quadro.MarcarMensagem(m.ID, quadro.MensagemEntregue, "stop", "", agora); err != nil {
			return "", err
		}
	}
	cabeca := "Mensagem do utilizador, enviada pelo painel do Towerman enquanto trabalhavas:"
	if len(textos) > 1 {
		cabeca = "Mensagens do utilizador, enviadas pelo painel do Towerman enquanto trabalhavas:"
	}
	return cabeca + "\n\n" + strings.Join(textos, "\n\n---\n\n"), nil
}
