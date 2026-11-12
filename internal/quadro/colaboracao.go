package quadro

import (
	"database/sql"
	"errors"
	"time"
)

const (
	MensagemPendente = "pendente"
	MensagemEntregue = "entregue"
	MensagemFalhada  = "falhada"
)

// Canal de uma resposta: o terminal, o painel local ou o id do aparelho pareado.
const (
	CanalTerminal = "terminal"
	CanalPainel   = "painel"
)

// Origem de uma resposta dada num aparelho (o relógio responde através do iPhone).
const (
	OrigemIPhone  = "iphone"
	OrigemRelogio = "relogio"
	OrigemMac     = "mac"
)

// Forma da resposta a uma decisão: um botão de opção ou texto livre.
const (
	FormaOpcao = "opcao"
	FormaTexto = "texto"
)

// Resposta diz quem respondeu a uma permissão ou decisão, e como; só alimenta as métricas.
type Resposta struct {
	Canal  string
	Origem string
	Forma  string
}

type Mensagem struct {
	ID         int64  `json:"id"`
	Para       string `json:"para"`
	Texto      string `json:"texto"`
	Caminho    string `json:"caminho"`
	Estado     string `json:"estado"`
	Motivo     string `json:"motivo"`
	CriadaEm   string `json:"criada_em"`
	EntregueEm string `json:"entregue_em"`
}

type Permissao struct {
	ID         int64  `json:"id"`
	SessionID  string `json:"session_id"`
	Ferramenta string `json:"ferramenta"`
	Resumo     string `json:"resumo"`
	Decisao    string `json:"decisao"`
	PedidaEm   string `json:"pedida_em"`
}

// ToqueAlheio é um toque de outra sessão ainda não terminada no mesmo ficheiro.
type ToqueAlheio struct {
	SessionID string
	Nome      string
	Branch    string
	Worktree  string
	Em        string
}

func agoraTxt(t time.Time) string { return t.UTC().Format(FormatoData) }

func (q *Quadro) SessoesVivasNoRepo(repo, exceto string) ([]Sessao, error) {
	return q.listarSessoes(`SELECT `+colunasSessao+` FROM sessoes
		WHERE repo = ? AND session_id != ? AND estado != ? ORDER BY visto_em DESC`, repo, exceto, Terminada)
}

func (q *Quadro) ToquesAlheios(repo, caminho, exceto string, desde time.Time) ([]ToqueAlheio, error) {
	rows, err := q.db.Query(`SELECT t.session_id, s.nome, t.branch, t.worktree, max(t.em)
		FROM toques t JOIN sessoes s USING (session_id)
		WHERE t.repo = ? AND t.caminho = ? AND t.session_id != ? AND t.tipo != 'git' AND t.em >= ? AND s.estado != ?
		GROUP BY t.session_id, t.branch ORDER BY max(t.em) DESC`, repo, caminho, exceto, agoraTxt(desde), Terminada)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var r []ToqueAlheio
	for rows.Next() {
		var t ToqueAlheio
		if err := rows.Scan(&t.SessionID, &t.Nome, &t.Branch, &t.Worktree, &t.Em); err != nil {
			return nil, err
		}
		r = append(r, t)
	}
	return r, rows.Err()
}

// RegistarAvisoSeNovo devolve false quando o mesmo aviso já foi dado depois de desde.
func (q *Quadro) RegistarAvisoSeNovo(de, para string, regra int, caminho, motivo string, desde, em time.Time) (bool, error) {
	var n int
	if err := q.db.QueryRow(`SELECT count(*) FROM avisos WHERE de = ? AND para = ? AND regra = ? AND caminho = ? AND em >= ?`,
		de, para, regra, caminho, agoraTxt(desde)).Scan(&n); err != nil {
		return false, err
	}
	if n > 0 {
		return false, nil
	}
	_, err := q.db.Exec(`INSERT INTO avisos(de, para, regra, caminho, motivo, em) VALUES(?, ?, ?, ?, ?, ?)`,
		de, para, regra, caminho, motivo, agoraTxt(em))
	return err == nil, err
}

func (q *Quadro) CriarMensagem(para, texto, caminho string, em time.Time) (int64, error) {
	res, err := q.db.Exec(`INSERT INTO mensagens(para, texto, caminho, estado, criada_em) VALUES(?, ?, ?, ?, ?)`,
		para, texto, caminho, MensagemPendente, agoraTxt(em))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (q *Quadro) listarMensagens(consulta string, args ...any) ([]Mensagem, error) {
	rows, err := q.db.Query(`SELECT id, para, texto, caminho, estado, motivo, criada_em, COALESCE(entregue_em, '')
		FROM mensagens `+consulta, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var r []Mensagem
	for rows.Next() {
		var m Mensagem
		if err := rows.Scan(&m.ID, &m.Para, &m.Texto, &m.Caminho, &m.Estado, &m.Motivo, &m.CriadaEm, &m.EntregueEm); err != nil {
			return nil, err
		}
		r = append(r, m)
	}
	return r, rows.Err()
}

func (q *Quadro) MensagensPendentes(para string) ([]Mensagem, error) {
	return q.listarMensagens(`WHERE para = ? AND estado = ? ORDER BY id`, para, MensagemPendente)
}

func (q *Quadro) MensagensRecentes(para string, limite int) ([]Mensagem, error) {
	return q.listarMensagens(`WHERE para = ? ORDER BY id DESC LIMIT ?`, para, limite)
}

func (q *Quadro) ObterMensagem(id int64) (Mensagem, error) {
	ms, err := q.listarMensagens(`WHERE id = ?`, id)
	if err != nil {
		return Mensagem{}, err
	}
	if len(ms) == 0 {
		return Mensagem{}, sql.ErrNoRows
	}
	return ms[0], nil
}

func (q *Quadro) MarcarMensagem(id int64, estado, caminho, motivo string, em time.Time) error {
	var entregue any
	if estado == MensagemEntregue {
		entregue = agoraTxt(em)
	}
	_, err := q.db.Exec(`UPDATE mensagens SET estado = ?, caminho = ?, motivo = ?, entregue_em = ? WHERE id = ?`,
		estado, caminho, motivo, entregue, id)
	return err
}

func (q *Quadro) FalharMensagensPendentes(para, motivo string) error {
	_, err := q.db.Exec(`UPDATE mensagens SET estado = ?, motivo = ? WHERE para = ? AND estado = ?`,
		MensagemFalhada, motivo, para, MensagemPendente)
	return err
}

// MoverMensagensPendentes passa a fila de uma sessão para outra (a conversa continuou noutra).
func (q *Quadro) MoverMensagensPendentes(de, para string) error {
	_, err := q.db.Exec(`UPDATE mensagens SET para = ? WHERE para = ? AND estado = ?`, para, de, MensagemPendente)
	return err
}

func (q *Quadro) CriarPermissao(sessionID, ferramenta, resumo string, em time.Time) (int64, error) {
	res, err := q.db.Exec(`INSERT INTO permissoes(session_id, ferramenta, resumo, pedida_em) VALUES(?, ?, ?, ?)`,
		sessionID, ferramenta, resumo, agoraTxt(em))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (q *Quadro) DecidirPermissao(id int64, decisao string, resp Resposta, em time.Time) error {
	res, err := q.db.Exec(`UPDATE permissoes SET decisao = ?, decidida_em = ?, canal = ?, origem = ? WHERE id = ? AND decisao = ''`,
		decisao, agoraTxt(em), resp.Canal, resp.Origem, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("permissão já decidida ou inexistente")
	}
	return nil
}

func (q *Quadro) PermissoesPendentes() ([]Permissao, error) {
	rows, err := q.db.Query(`SELECT id, session_id, ferramenta, resumo, decisao, pedida_em FROM permissoes
		WHERE decisao = '' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var r []Permissao
	for rows.Next() {
		var p Permissao
		if err := rows.Scan(&p.ID, &p.SessionID, &p.Ferramenta, &p.Resumo, &p.Decisao, &p.PedidaEm); err != nil {
			return nil, err
		}
		r = append(r, p)
	}
	return r, rows.Err()
}

type Aviso struct {
	ID          int64  `json:"id"`
	De          string `json:"de"`
	NomeDe      string `json:"nome_de"`
	Para        string `json:"para"`
	NomePara    string `json:"nome_para"`
	Regra       int    `json:"regra"`
	Caminho     string `json:"caminho"`
	Motivo      string `json:"motivo"`
	Em          string `json:"em"`
	FalsoAlarme bool   `json:"falso_alarme"`
}

func (q *Quadro) AvisosDesde(desde time.Time) ([]Aviso, error) {
	rows, err := q.db.Query(`SELECT a.id, a.de, COALESCE(sd.nome, ''), a.para, COALESCE(sp.nome, ''), a.regra, a.caminho, a.motivo, a.em, a.falso_alarme
		FROM avisos a LEFT JOIN sessoes sd ON sd.session_id = a.de LEFT JOIN sessoes sp ON sp.session_id = a.para
		WHERE a.em >= ? ORDER BY a.id DESC LIMIT 100`, agoraTxt(desde))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var r []Aviso
	for rows.Next() {
		var a Aviso
		if err := rows.Scan(&a.ID, &a.De, &a.NomeDe, &a.Para, &a.NomePara, &a.Regra, &a.Caminho, &a.Motivo, &a.Em, &a.FalsoAlarme); err != nil {
			return nil, err
		}
		r = append(r, a)
	}
	return r, rows.Err()
}

func (q *Quadro) MarcarFalsoAlarme(id int64) error {
	res, err := q.db.Exec(`UPDATE avisos SET falso_alarme = 1 WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ContarAvisos devolve, por regra, o total de avisos e quantos foram marcados como falso alarme.
func (q *Quadro) ContarAvisos() (map[int][2]int, error) {
	rows, err := q.db.Query(`SELECT regra, count(*), sum(falso_alarme) FROM avisos GROUP BY regra`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	r := map[int][2]int{}
	for rows.Next() {
		var regra, total, falsos int
		if err := rows.Scan(&regra, &total, &falsos); err != nil {
			return nil, err
		}
		r[regra] = [2]int{total, falsos}
	}
	return r, rows.Err()
}

// Colisao liga uma sessão viva a outra que tocou o mesmo ficheiro nas últimas horas.
type Colisao struct {
	SessionID   string
	Com         string
	NomeCom     string
	Caminho     string
	MesmaBranch bool
}

func (q *Quadro) ColisoesVivas(desde time.Time) ([]Colisao, error) {
	rows, err := q.db.Query(`SELECT DISTINCT a.session_id, b.session_id, COALESCE(sb.nome, ''), a.caminho, a.branch = b.branch
		FROM toques a
		JOIN toques b ON a.repo = b.repo AND a.caminho = b.caminho AND a.session_id != b.session_id
		JOIN sessoes sa ON sa.session_id = a.session_id
		JOIN sessoes sb ON sb.session_id = b.session_id
		WHERE a.tipo != 'git' AND b.tipo != 'git' AND a.repo != '' AND a.em >= ? AND b.em >= ?
		  AND sa.estado != 'terminada' AND sb.estado != 'terminada'
		ORDER BY a.session_id, a.caminho`, agoraTxt(desde), agoraTxt(desde))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var r []Colisao
	for rows.Next() {
		var c Colisao
		if err := rows.Scan(&c.SessionID, &c.Com, &c.NomeCom, &c.Caminho, &c.MesmaBranch); err != nil {
			return nil, err
		}
		r = append(r, c)
	}
	return r, rows.Err()
}

// ReposESessoes devolve os repos já vistos e, por worktree, o nome da sessão viva que lá está.
func (q *Quadro) ReposESessoes() ([]string, map[string]string, error) {
	rows, err := q.db.Query(`SELECT DISTINCT repo FROM sessoes WHERE repo != ''`)
	if err != nil {
		return nil, nil, err
	}
	var repos []string
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			rows.Close()
			return nil, nil, err
		}
		repos = append(repos, r)
	}
	rows.Close()
	vivas, err := q.SessoesNaoTerminadas()
	if err != nil {
		return nil, nil, err
	}
	sessoes := map[string]string{}
	for _, s := range vivas {
		if s.Worktree != "" {
			sessoes[s.Worktree] = nomeSessao(s)
		}
	}
	return repos, sessoes, nil
}

func nomeSessao(s Sessao) string {
	if s.Nome != "" {
		return s.Nome
	}
	if len(s.SessionID) > 8 {
		return s.SessionID[:8]
	}
	return s.SessionID
}
