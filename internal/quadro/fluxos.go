package quadro

import (
	"database/sql"
	"errors"
	"time"
)

const (
	FluxoAtivo  = "ativo"
	FluxoParado = "parado"
	FluxoLimite = "limite"

	GatilhoStop  = "stop"
	GatilhoMarca = "marca"
)

type Fluxo struct {
	ID        int64      `json:"id"`
	Nome      string     `json:"nome"`
	MaxPassos int        `json:"max_passos"`
	Estado    string     `json:"estado"`
	CriadoEm  string     `json:"criado_em"`
	Passos    int        `json:"passos"`
	Arestas   []Aresta   `json:"arestas"`
	Passagens []Passagem `json:"passagens"`
}

type Aresta struct {
	ID       int64  `json:"id"`
	Fluxo    int64  `json:"fluxo"`
	De       string `json:"de"`
	Para     string `json:"para"`
	NomeDe   string `json:"nome_de"`
	NomePara string `json:"nome_para"`
	Gatilho  string `json:"gatilho"`
	Marca    string `json:"marca"`
	Modelo   string `json:"modelo"`
}

type Passagem struct {
	ID       int64  `json:"id"`
	Fluxo    int64  `json:"fluxo"`
	Aresta   int64  `json:"aresta"`
	De       string `json:"de"`
	Para     string `json:"para"`
	NomeDe   string `json:"nome_de"`
	NomePara string `json:"nome_para"`
	Texto    string `json:"texto"`
	Estado   string `json:"estado"`
	Motivo   string `json:"motivo"`
	Em       string `json:"em"`
}

// MarcarHeadless regista (ou atualiza) uma sessão arrancada pelo Towerman antes de os hooks dela chegarem.
func (q *Quadro) MarcarHeadless(id, nome, cwd, papel string, pid int, headless bool, em time.Time) error {
	t := agoraTxt(em)
	_, err := q.db.Exec(`INSERT INTO sessoes(session_id, nome, pid, cwd, papel, headless, estado, iniciada_em, visto_em)
		VALUES(?, ?, ?, ?, ?, ?, 'ativa', ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET papel = excluded.papel, headless = excluded.headless,
			pid = CASE WHEN excluded.pid > 0 THEN excluded.pid ELSE sessoes.pid END,
			nome = CASE WHEN sessoes.nome = '' THEN excluded.nome ELSE sessoes.nome END`,
		id, nome, pid, cwd, papel, headless, t, t)
	return err
}

// MarcarLancamento guarda o agente e o aparelho que pediu uma sessão headless à distância.
func (q *Quadro) MarcarLancamento(id, agente, lancadaPor string) error {
	_, err := q.db.Exec(`UPDATE sessoes SET agente = ?, lancada_por = ? WHERE session_id = ?`, agente, lancadaPor, id)
	return err
}

func (q *Quadro) CriarFluxo(nome string, maxPassos int, em time.Time) (int64, error) {
	res, err := q.db.Exec(`INSERT INTO fluxos(nome, max_passos, estado, criado_em) VALUES(?, ?, ?, ?)`, nome, maxPassos, FluxoAtivo, agoraTxt(em))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// FluxoDaSessao devolve o fluxo ativo onde a sessão já participa, para novas ligações se juntarem a ele.
func (q *Quadro) FluxoDaSessao(sessionID string) (int64, error) {
	var id int64
	err := q.db.QueryRow(`SELECT a.fluxo FROM arestas a JOIN fluxos f ON f.id = a.fluxo
		WHERE (a.de = ? OR a.para = ?) AND f.estado != ? ORDER BY a.id DESC LIMIT 1`, sessionID, sessionID, FluxoParado).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

func (q *Quadro) CriarAresta(a Aresta, em time.Time) (int64, error) {
	res, err := q.db.Exec(`INSERT INTO arestas(fluxo, de, para, gatilho, marca, modelo, criada_em) VALUES(?, ?, ?, ?, ?, ?, ?)`,
		a.Fluxo, a.De, a.Para, a.Gatilho, a.Marca, a.Modelo, agoraTxt(em))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (q *Quadro) ApagarAresta(id int64) error {
	res, err := q.db.Exec(`DELETE FROM arestas WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

const colunasAresta = `a.id, a.fluxo, a.de, a.para, COALESCE(sd.nome, ''), COALESCE(sp.nome, ''), a.gatilho, a.marca, a.modelo
	FROM arestas a LEFT JOIN sessoes sd ON sd.session_id = a.de LEFT JOIN sessoes sp ON sp.session_id = a.para`

func (q *Quadro) listarArestas(onde string, args ...any) ([]Aresta, error) {
	rows, err := q.db.Query(`SELECT `+colunasAresta+` `+onde, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var r []Aresta
	for rows.Next() {
		var a Aresta
		if err := rows.Scan(&a.ID, &a.Fluxo, &a.De, &a.Para, &a.NomeDe, &a.NomePara, &a.Gatilho, &a.Marca, &a.Modelo); err != nil {
			return nil, err
		}
		r = append(r, a)
	}
	return r, rows.Err()
}

// ArestasAtivasDe devolve as ligações que saem da sessão em fluxos ainda ativos.
func (q *Quadro) ArestasAtivasDe(sessionID string) ([]Aresta, error) {
	return q.listarArestas(`JOIN fluxos f ON f.id = a.fluxo WHERE a.de = ? AND f.estado = ? ORDER BY a.id`, sessionID, FluxoAtivo)
}

func (q *Quadro) ObterFluxo(id int64) (Fluxo, error) {
	var f Fluxo
	err := q.db.QueryRow(`SELECT id, nome, max_passos, estado, criado_em, (SELECT count(*) FROM passagens WHERE fluxo = fluxos.id)
		FROM fluxos WHERE id = ?`, id).Scan(&f.ID, &f.Nome, &f.MaxPassos, &f.Estado, &f.CriadoEm, &f.Passos)
	return f, err
}

func (q *Quadro) DefinirEstadoFluxo(id int64, estado string) error {
	res, err := q.db.Exec(`UPDATE fluxos SET estado = ? WHERE id = ?`, estado, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (q *Quadro) RegistarPassagem(p Passagem, em time.Time) (int64, error) {
	res, err := q.db.Exec(`INSERT INTO passagens(fluxo, aresta, de, para, texto, estado, motivo, em) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Fluxo, p.Aresta, p.De, p.Para, p.Texto, p.Estado, p.Motivo, agoraTxt(em))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (q *Quadro) MarcarPassagem(id int64, estado, motivo string) error {
	_, err := q.db.Exec(`UPDATE passagens SET estado = ?, motivo = ? WHERE id = ?`, estado, motivo, id)
	return err
}

// Fluxos devolve todos os fluxos com as suas ligações e as últimas passagens (mais recentes primeiro).
func (q *Quadro) Fluxos(passagensPorFluxo int) ([]Fluxo, error) {
	rows, err := q.db.Query(`SELECT id, nome, max_passos, estado, criado_em, (SELECT count(*) FROM passagens WHERE fluxo = fluxos.id)
		FROM fluxos ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	var r []Fluxo
	for rows.Next() {
		var f Fluxo
		if err := rows.Scan(&f.ID, &f.Nome, &f.MaxPassos, &f.Estado, &f.CriadoEm, &f.Passos); err != nil {
			rows.Close()
			return nil, err
		}
		r = append(r, f)
	}
	rows.Close()
	for i := range r {
		if r[i].Arestas, err = q.listarArestas(`WHERE a.fluxo = ? ORDER BY a.id`, r[i].ID); err != nil {
			return nil, err
		}
		if r[i].Arestas == nil {
			r[i].Arestas = []Aresta{}
		}
		ps, err := q.db.Query(`SELECT p.id, p.fluxo, p.aresta, p.de, p.para, COALESCE(sd.nome, ''), COALESCE(sp.nome, ''), p.texto, p.estado, p.motivo, p.em
			FROM passagens p LEFT JOIN sessoes sd ON sd.session_id = p.de LEFT JOIN sessoes sp ON sp.session_id = p.para
			WHERE p.fluxo = ? ORDER BY p.id DESC LIMIT ?`, r[i].ID, passagensPorFluxo)
		if err != nil {
			return nil, err
		}
		r[i].Passagens = []Passagem{}
		for ps.Next() {
			var p Passagem
			if err := ps.Scan(&p.ID, &p.Fluxo, &p.Aresta, &p.De, &p.Para, &p.NomeDe, &p.NomePara, &p.Texto, &p.Estado, &p.Motivo, &p.Em); err != nil {
				ps.Close()
				return nil, err
			}
			r[i].Passagens = append(r[i].Passagens, p)
		}
		ps.Close()
	}
	return r, nil
}

// ArestasVivas devolve as ligações de fluxos não parados, para o mapa as desenhar.
func (q *Quadro) ArestasVivas() ([]Aresta, error) {
	return q.listarArestas(`JOIN fluxos f ON f.id = a.fluxo WHERE f.estado != ? ORDER BY a.id`, FluxoParado)
}

func (q *Quadro) DefinirMaxPassos(id int64, max int) error {
	_, err := q.db.Exec(`UPDATE fluxos SET max_passos = ? WHERE id = ?`, max, id)
	return err
}
