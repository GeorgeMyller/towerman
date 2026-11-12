package quadro

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// Desfechos de uma decisão pendente (ver _reversa_forward/011-decisao-no-pulso/data-delta.md).
const (
	DecisaoRespondida  = "respondida"
	DecisaoDescartada  = "descartada"
	DecisaoNoTerminal  = "terminal"
	DecisaoSubstituida = "substituida"
	DecisaoTerminada   = "terminada"
)

// OpcaoDecisao é um botão: o rótulo vai para o ecrã, o envio é o texto entregue à sessão.
type OpcaoDecisao struct {
	Rotulo string `json:"rotulo"`
	Envio  string `json:"envio"`
}

// Decisao é a pergunta em texto livre com que um agente terminou o turno.
type Decisao struct {
	ID        int64          `json:"id"`
	SessionID string         `json:"session_id"`
	Pergunta  string         `json:"pergunta"`
	Opcoes    []OpcaoDecisao `json:"opcoes"`
	Padrao    string         `json:"padrao"`
	Lingua    string         `json:"lingua"`
	CriadaEm  string         `json:"criada_em"`
}

// AbrirDecisao fecha a decisão aberta da sessão (substituída) e abre a nova, numa transação:
// cada sessão tem no máximo uma decisão aberta.
func (q *Quadro) AbrirDecisao(d Decisao, em time.Time) (int64, error) {
	opcoes, _ := json.Marshal(d.Opcoes)
	if d.Opcoes == nil {
		opcoes = []byte("[]")
	}
	tx, err := q.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(fecharSQL+` WHERE session_id = ? AND fechada_em IS NULL`,
		agoraTxt(em), DecisaoSubstituida, d.SessionID); err != nil {
		return 0, err
	}
	res, err := tx.Exec(`INSERT INTO decisoes(session_id, pergunta, opcoes, padrao, lingua, criada_em) VALUES(?, ?, ?, ?, ?, ?)`,
		d.SessionID, d.Pergunta, string(opcoes), d.Padrao, d.Lingua, agoraTxt(em))
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// DecisaoAberta devolve a decisão aberta da sessão, se houver.
func (q *Quadro) DecisaoAberta(sessionID string) (Decisao, bool, error) {
	var d Decisao
	var opcoes string
	err := q.db.QueryRow(`SELECT id, session_id, pergunta, opcoes, padrao, lingua, criada_em FROM decisoes
		WHERE session_id = ? AND fechada_em IS NULL`, sessionID).
		Scan(&d.ID, &d.SessionID, &d.Pergunta, &opcoes, &d.Padrao, &d.Lingua, &d.CriadaEm)
	if errors.Is(err, sql.ErrNoRows) {
		return Decisao{}, false, nil
	}
	if err != nil {
		return Decisao{}, false, err
	}
	json.Unmarshal([]byte(opcoes), &d.Opcoes)
	return d, true, nil
}

// fecharSQL apaga o texto ao fechar: depois de respondida, a decisão só guarda padrão e desfecho.
const fecharSQL = `UPDATE decisoes SET fechada_em = ?, desfecho = ?, pergunta = '', opcoes = '[]'`

// FecharDecisao fecha a decisão se ainda estiver aberta; devolve falso se já estava fechada.
func (q *Quadro) FecharDecisao(id int64, desfecho string, resp Resposta, em time.Time) (bool, error) {
	res, err := q.db.Exec(fecharSQL+`, canal = ?, origem = ?, forma = ? WHERE id = ? AND fechada_em IS NULL`,
		agoraTxt(em), desfecho, resp.Canal, resp.Origem, resp.Forma, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// DecisoesAbertas devolve as decisões abertas de todas as sessões.
func (q *Quadro) DecisoesAbertas() ([]Decisao, error) {
	rows, err := q.db.Query(`SELECT id, session_id, pergunta, opcoes, padrao, lingua, criada_em FROM decisoes
		WHERE fechada_em IS NULL ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var r []Decisao
	for rows.Next() {
		var d Decisao
		var opcoes string
		if err := rows.Scan(&d.ID, &d.SessionID, &d.Pergunta, &opcoes, &d.Padrao, &d.Lingua, &d.CriadaEm); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(opcoes), &d.Opcoes)
		r = append(r, d)
	}
	return r, rows.Err()
}

// FecharDecisaoDaSessao fecha a decisão aberta da sessão, se houver, e devolve-a já fechada.
func (q *Quadro) FecharDecisaoDaSessao(sessionID, desfecho string, resp Resposta, em time.Time) (Decisao, bool, error) {
	d, ok, err := q.DecisaoAberta(sessionID)
	if err != nil || !ok {
		return Decisao{}, false, err
	}
	fechou, err := q.FecharDecisao(d.ID, desfecho, resp, em)
	return d, fechou, err
}

// ReabrirDecisao devolve a decisão ao estado aberto, com o mesmo id, depois de uma resposta que não
// chegou à sessão; assim a métrica não conta uma resposta que não houve. Se entretanto a sessão abriu
// outra decisão, a antiga fica fechada.
func (q *Quadro) ReabrirDecisao(d Decisao) error {
	opcoes, _ := json.Marshal(d.Opcoes)
	if d.Opcoes == nil {
		opcoes = []byte("[]")
	}
	_, err := q.db.Exec(`UPDATE decisoes SET fechada_em = NULL, desfecho = NULL, canal = '', origem = '', forma = '',
		pergunta = ?, opcoes = ? WHERE id = ? AND NOT EXISTS
		(SELECT 1 FROM decisoes WHERE session_id = ? AND fechada_em IS NULL)`,
		d.Pergunta, string(opcoes), d.ID, d.SessionID)
	return err
}
