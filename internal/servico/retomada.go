package servico

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"time"

	"github.com/GeorgeMyller/towerman/internal/quadro"
)

// limiteRetomada: as linhas copiadas da conversa antiga vêm no início da transcrição.
const limiteRetomada = 4 << 20

// origemRetomada devolve a sessão de onde esta conversa foi retomada com --fork-session. As linhas
// copiadas da conversa antiga guardam o id dela em «session_id» (campo interno do Claude Code, sem
// documentação). A primeira linha com esse campo decide; sem nenhuma, ainda não dá para saber.
func origemRetomada(transcricao, sessao string) (origem string, decidido bool) {
	f, err := os.Open(transcricao)
	if err != nil {
		return "", false
	}
	defer f.Close()
	sc := bufio.NewScanner(io.LimitReader(f, limiteRetomada))
	sc.Buffer(make([]byte, 0, 64*1024), limiteRetomada)
	for sc.Scan() {
		if !bytes.Contains(sc.Bytes(), []byte(`"session_id"`)) {
			continue
		}
		var l struct {
			SessionID string `json:"sessionId"`
			Origem    string `json:"session_id"`
		}
		if json.Unmarshal(sc.Bytes(), &l) != nil || l.SessionID != sessao || l.Origem == "" {
			continue
		}
		if l.Origem == sessao {
			return "", true
		}
		return l.Origem, true
	}
	return "", false
}

// terminarCopiaAntiga: quando a conversa continua noutro processo, a cópia antiga fica parada no
// terminal de onde saiu e confundia o relógio (uma resposta ia para ela e nunca chegava). Passa a
// terminada, a decisão dela fecha e as mensagens em fila seguem para a conversa nova. Se a pessoa
// voltar a usar a cópia antiga, o próximo evento dela reabre-a.
func (p *Processador) terminarCopiaAntiga(s quadro.Sessao, agora time.Time) error {
	if s.TranscriptPath == "" {
		return nil
	}
	p.mu.Lock()
	visto := p.retomadas[s.SessionID]
	p.mu.Unlock()
	if visto {
		return nil
	}
	origem, decidido := origemRetomada(s.TranscriptPath, s.SessionID)
	if !decidido {
		return nil
	}
	p.mu.Lock()
	if p.retomadas == nil {
		p.retomadas = map[string]bool{}
	}
	p.retomadas[s.SessionID] = true
	p.mu.Unlock()
	if origem == "" {
		return nil
	}
	antiga, ok, err := p.Quadro.ObterSessao(origem)
	if err != nil || !ok || antiga.Estado == quadro.Terminada {
		return err
	}
	if err := p.Quadro.TerminarSessao(origem, agora); err != nil {
		return err
	}
	if err := p.fecharDecisao(origem, quadro.DecisaoTerminada, quadro.Resposta{}, agora); err != nil {
		return err
	}
	return p.Quadro.MoverMensagensPendentes(origem, s.SessionID)
}
