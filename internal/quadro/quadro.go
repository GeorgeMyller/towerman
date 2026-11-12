package quadro

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const FormatoData = time.RFC3339Nano

var migracoes = []string{
	`CREATE TABLE meta (chave TEXT PRIMARY KEY, valor TEXT NOT NULL);
	CREATE TABLE sessoes (
		session_id      TEXT PRIMARY KEY,
		agente          TEXT NOT NULL DEFAULT 'claude',
		nome            TEXT NOT NULL DEFAULT '',
		pid             INTEGER NOT NULL DEFAULT 0,
		cwd             TEXT NOT NULL DEFAULT '',
		repo            TEXT NOT NULL DEFAULT '',
		worktree        TEXT NOT NULL DEFAULT '',
		branch          TEXT NOT NULL DEFAULT '',
		terminal        TEXT NOT NULL DEFAULT '{}',
		transcript_path TEXT NOT NULL DEFAULT '',
		resumo          TEXT NOT NULL DEFAULT '',
		estado          TEXT NOT NULL DEFAULT 'ativa',
		iniciada_em     TEXT NOT NULL,
		visto_em        TEXT NOT NULL,
		terminada_em    TEXT
	);
	CREATE INDEX sessoes_estado ON sessoes(estado);
	CREATE TABLE toques (
		id         INTEGER PRIMARY KEY,
		session_id TEXT NOT NULL,
		repo       TEXT NOT NULL,
		worktree   TEXT NOT NULL,
		branch     TEXT NOT NULL,
		caminho    TEXT NOT NULL,
		tipo       TEXT NOT NULL,
		ferramenta TEXT NOT NULL,
		em         TEXT NOT NULL
	);
	CREATE INDEX toques_colisao ON toques(repo, caminho, branch, em);
	CREATE INDEX toques_sessao ON toques(session_id);
	CREATE TABLE eventos (
		id          INTEGER PRIMARY KEY,
		session_id  TEXT NOT NULL,
		tipo        TEXT NOT NULL,
		recebido_em TEXT NOT NULL,
		em          TEXT NOT NULL,
		UNIQUE(session_id, tipo, recebido_em)
	);
	CREATE INDEX eventos_em ON eventos(em);`,
	`CREATE TABLE avisos (
		id      INTEGER PRIMARY KEY,
		de      TEXT NOT NULL,
		para    TEXT NOT NULL,
		regra   INTEGER NOT NULL,
		caminho TEXT NOT NULL,
		motivo  TEXT NOT NULL,
		em      TEXT NOT NULL
	);
	CREATE INDEX avisos_chave ON avisos(de, para, regra, caminho, em);
	CREATE TABLE mensagens (
		id          INTEGER PRIMARY KEY,
		para        TEXT NOT NULL,
		texto       TEXT NOT NULL,
		caminho     TEXT NOT NULL,
		estado      TEXT NOT NULL,
		motivo      TEXT NOT NULL DEFAULT '',
		criada_em   TEXT NOT NULL,
		entregue_em TEXT
	);
	CREATE INDEX mensagens_para ON mensagens(para, estado);
	CREATE TABLE permissoes (
		id          INTEGER PRIMARY KEY,
		session_id  TEXT NOT NULL,
		ferramenta  TEXT NOT NULL,
		resumo      TEXT NOT NULL,
		decisao     TEXT NOT NULL DEFAULT '',
		pedida_em   TEXT NOT NULL,
		decidida_em TEXT
	);
	CREATE INDEX permissoes_pendentes ON permissoes(decisao);`,
	`ALTER TABLE avisos ADD COLUMN falso_alarme INTEGER NOT NULL DEFAULT 0;`,
	`ALTER TABLE sessoes ADD COLUMN papel TEXT NOT NULL DEFAULT '';
	ALTER TABLE sessoes ADD COLUMN headless INTEGER NOT NULL DEFAULT 0;
	CREATE TABLE fluxos (
		id         INTEGER PRIMARY KEY,
		nome       TEXT NOT NULL,
		max_passos INTEGER NOT NULL,
		estado     TEXT NOT NULL,
		criado_em  TEXT NOT NULL
	);
	CREATE TABLE arestas (
		id        INTEGER PRIMARY KEY,
		fluxo     INTEGER NOT NULL,
		de        TEXT NOT NULL,
		para      TEXT NOT NULL,
		gatilho   TEXT NOT NULL,
		marca     TEXT NOT NULL DEFAULT '',
		modelo    TEXT NOT NULL,
		criada_em TEXT NOT NULL
	);
	CREATE INDEX arestas_de ON arestas(de);
	CREATE TABLE passagens (
		id     INTEGER PRIMARY KEY,
		fluxo  INTEGER NOT NULL,
		aresta INTEGER NOT NULL,
		de     TEXT NOT NULL,
		para   TEXT NOT NULL,
		texto  TEXT NOT NULL,
		estado TEXT NOT NULL,
		motivo TEXT NOT NULL DEFAULT '',
		em     TEXT NOT NULL
	);
	CREATE INDEX passagens_fluxo ON passagens(fluxo, id);`,
	`CREATE TABLE decisoes (
		id         INTEGER PRIMARY KEY,
		session_id TEXT NOT NULL,
		pergunta   TEXT NOT NULL DEFAULT '',
		opcoes     TEXT NOT NULL DEFAULT '[]',
		padrao     TEXT NOT NULL,
		lingua     TEXT NOT NULL,
		criada_em  TEXT NOT NULL,
		fechada_em TEXT,
		desfecho   TEXT
	);
	CREATE UNIQUE INDEX decisoes_aberta ON decisoes(session_id) WHERE fechada_em IS NULL;`,
	`ALTER TABLE sessoes ADD COLUMN lancada_por TEXT NOT NULL DEFAULT '';`,
	// Quem respondeu e como (_reversa_forward/012-metricas/data-delta.md).
	`ALTER TABLE permissoes ADD COLUMN canal TEXT NOT NULL DEFAULT '';
	ALTER TABLE permissoes ADD COLUMN origem TEXT NOT NULL DEFAULT '';
	ALTER TABLE decisoes ADD COLUMN canal TEXT NOT NULL DEFAULT '';
	ALTER TABLE decisoes ADD COLUMN origem TEXT NOT NULL DEFAULT '';
	ALTER TABLE decisoes ADD COLUMN forma TEXT NOT NULL DEFAULT '';
	CREATE INDEX toques_sessao_tipo ON toques(session_id, tipo, em);`,
}

const (
	Ativa     = "ativa"
	Parada    = "parada"
	AEspera   = "a_espera"
	Terminada = "terminada"
)

type Sessao struct {
	SessionID      string
	Agente         string
	Nome           string
	Pid            int
	Cwd            string
	Repo           string
	Worktree       string
	Branch         string
	Terminal       string
	TranscriptPath string
	Resumo         string
	Estado         string
	IniciadaEm     string
	VistoEm        string
	TerminadaEm    string
	Papel          string
	Headless       bool
	// LancadaPor é o id do aparelho que pediu a sessão à distância; vazio nas sessões locais.
	LancadaPor string
}

type Toque struct {
	SessionID  string
	Repo       string
	Worktree   string
	Branch     string
	Caminho    string
	Tipo       string
	Ferramenta string
	Em         string
}

type Contagens struct {
	Sessoes map[string]int `json:"sessoes"`
	Toques  int            `json:"toques"`
	Eventos int            `json:"eventos"`
}

type Quadro struct {
	db *sql.DB
}

func Abrir(caminho string) (*Quadro, error) {
	if err := os.MkdirAll(filepath.Dir(caminho), 0o700); err != nil {
		return nil, err
	}
	dsn := "file:" + (&url.URL{Path: filepath.ToSlash(caminho)}).EscapedPath() +
		"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	q := &Quadro{db: db}
	if err := q.migrar(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrar %s: %w", caminho, err)
	}
	return q, nil
}

// AbrirLeitura abre o quadro só para leitura, sem migrar; o serviço continua a ser o único escritor.
func AbrirLeitura(caminho string) (*Quadro, error) {
	if _, err := os.Stat(caminho); err != nil {
		return nil, err
	}
	dsn := "file:" + (&url.URL{Path: filepath.ToSlash(caminho)}).EscapedPath() + "?mode=ro&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	return &Quadro{db: db}, nil
}

func (q *Quadro) Fechar() error { return q.db.Close() }

func (q *Quadro) migrar() error {
	var versao int
	err := q.db.QueryRow(`SELECT CAST(valor AS INTEGER) FROM meta WHERE chave = 'versao_esquema'`).Scan(&versao)
	if err != nil {
		var existe int
		q.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name = 'meta'`).Scan(&existe)
		if existe != 0 {
			return err
		}
	}
	for i := versao; i < len(migracoes); i++ {
		tx, err := q.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migracoes[i]); err != nil {
			tx.Rollback()
			return err
		}
		if _, err := tx.Exec(`INSERT INTO meta(chave, valor) VALUES('versao_esquema', ?)
			ON CONFLICT(chave) DO UPDATE SET valor = excluded.valor`, fmt.Sprint(i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// RegistarEvento devolve false quando o evento já tinha sido registado.
func (q *Quadro) RegistarEvento(sessionID, tipo, recebidoEm string, em time.Time) (bool, error) {
	res, err := q.db.Exec(`INSERT OR IGNORE INTO eventos(session_id, tipo, recebido_em, em) VALUES(?, ?, ?, ?)`,
		sessionID, tipo, recebidoEm, em.UTC().Format(FormatoData))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

const colunasSessao = `session_id, agente, nome, pid, cwd, repo, worktree, branch, terminal,
	transcript_path, resumo, estado, iniciada_em, visto_em, COALESCE(terminada_em, ''), papel, headless, lancada_por`

func lerSessao(s interface{ Scan(...any) error }) (Sessao, error) {
	var x Sessao
	err := s.Scan(&x.SessionID, &x.Agente, &x.Nome, &x.Pid, &x.Cwd, &x.Repo, &x.Worktree, &x.Branch,
		&x.Terminal, &x.TranscriptPath, &x.Resumo, &x.Estado, &x.IniciadaEm, &x.VistoEm, &x.TerminadaEm, &x.Papel, &x.Headless, &x.LancadaPor)
	return x, err
}

func (q *Quadro) ObterSessao(id string) (Sessao, bool, error) {
	s, err := lerSessao(q.db.QueryRow(`SELECT `+colunasSessao+` FROM sessoes WHERE session_id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Sessao{}, false, nil
	}
	return s, err == nil, err
}

func (q *Quadro) GuardarSessao(s Sessao) error {
	var terminada any
	if s.TerminadaEm != "" {
		terminada = s.TerminadaEm
	}
	_, err := q.db.Exec(`INSERT INTO sessoes(session_id, agente, nome, pid, cwd, repo, worktree, branch, terminal,
		transcript_path, resumo, estado, iniciada_em, visto_em, terminada_em, papel, headless, lancada_por)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET
			agente = excluded.agente, nome = excluded.nome, pid = excluded.pid, cwd = excluded.cwd,
			repo = excluded.repo, worktree = excluded.worktree, branch = excluded.branch,
			terminal = excluded.terminal, transcript_path = excluded.transcript_path,
			resumo = excluded.resumo, estado = excluded.estado, visto_em = excluded.visto_em,
			terminada_em = excluded.terminada_em, papel = excluded.papel, headless = excluded.headless,
			lancada_por = CASE WHEN excluded.lancada_por <> '' THEN excluded.lancada_por ELSE sessoes.lancada_por END`,
		s.SessionID, s.Agente, s.Nome, s.Pid, s.Cwd, s.Repo, s.Worktree, s.Branch, s.Terminal,
		s.TranscriptPath, s.Resumo, s.Estado, s.IniciadaEm, s.VistoEm, terminada, s.Papel, s.Headless, s.LancadaPor)
	return err
}

func (q *Quadro) InserirToque(t Toque) error {
	_, err := q.db.Exec(`INSERT INTO toques(session_id, repo, worktree, branch, caminho, tipo, ferramenta, em)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		t.SessionID, t.Repo, t.Worktree, t.Branch, t.Caminho, t.Tipo, t.Ferramenta, t.Em)
	return err
}

func (q *Quadro) Toques(sessionID string) ([]Toque, error) {
	return q.listarToques(`SELECT session_id, repo, worktree, branch, caminho, tipo, ferramenta, em
		FROM toques WHERE session_id = ? ORDER BY id`, sessionID)
}

// ToquesRecentes devolve o último toque de cada caminho da sessão, do mais recente para o mais antigo.
func (q *Quadro) ToquesRecentes(sessionID string, limite int) ([]Toque, error) {
	return q.listarToques(`SELECT session_id, repo, worktree, branch, caminho, tipo, ferramenta, max(em) AS em
		FROM toques WHERE session_id = ? GROUP BY caminho ORDER BY em DESC LIMIT ?`, sessionID, limite)
}

func (q *Quadro) listarToques(consulta string, args ...any) ([]Toque, error) {
	rows, err := q.db.Query(consulta, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var r []Toque
	for rows.Next() {
		var t Toque
		if err := rows.Scan(&t.SessionID, &t.Repo, &t.Worktree, &t.Branch, &t.Caminho, &t.Tipo, &t.Ferramenta, &t.Em); err != nil {
			return nil, err
		}
		r = append(r, t)
	}
	return r, rows.Err()
}

func (q *Quadro) SessoesNaoTerminadas() ([]Sessao, error) {
	return q.listarSessoes(`SELECT `+colunasSessao+` FROM sessoes WHERE estado != ?`, Terminada)
}

// Sessoes devolve as sessões da mais recente para a mais antiga; as terminadas só com todas.
func (q *Quadro) Sessoes(todas bool) ([]Sessao, error) {
	if todas {
		return q.listarSessoes(`SELECT ` + colunasSessao + ` FROM sessoes ORDER BY visto_em DESC`)
	}
	return q.listarSessoes(`SELECT `+colunasSessao+` FROM sessoes WHERE estado != ? ORDER BY visto_em DESC`, Terminada)
}

func (q *Quadro) listarSessoes(consulta string, args ...any) ([]Sessao, error) {
	rows, err := q.db.Query(consulta, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var r []Sessao
	for rows.Next() {
		s, err := lerSessao(rows)
		if err != nil {
			return nil, err
		}
		r = append(r, s)
	}
	return r, rows.Err()
}

func (q *Quadro) TerminarSessao(id string, em time.Time) error {
	t := em.UTC().Format(FormatoData)
	_, err := q.db.Exec(`UPDATE sessoes SET estado = ?, terminada_em = ?, visto_em = ? WHERE session_id = ?`,
		Terminada, t, t, id)
	return err
}

func (q *Quadro) Contar() (Contagens, error) {
	c := Contagens{Sessoes: map[string]int{Ativa: 0, Parada: 0, AEspera: 0, Terminada: 0}}
	rows, err := q.db.Query(`SELECT estado, count(*) FROM sessoes GROUP BY estado`)
	if err != nil {
		return c, err
	}
	for rows.Next() {
		var e string
		var n int
		if err := rows.Scan(&e, &n); err != nil {
			rows.Close()
			return c, err
		}
		c.Sessoes[e] = n
	}
	rows.Close()
	if err := q.db.QueryRow(`SELECT count(*) FROM toques`).Scan(&c.Toques); err != nil {
		return c, err
	}
	err = q.db.QueryRow(`SELECT count(*) FROM eventos`).Scan(&c.Eventos)
	return c, err
}

// Reter apaga eventos e toques anteriores a limite e as sessões terminadas antes dele.
func (q *Quadro) Reter(limite time.Time) error {
	l := limite.UTC().Format(FormatoData)
	for _, s := range []string{
		`DELETE FROM eventos WHERE em < ?`,
		`DELETE FROM toques WHERE em < ?`,
		`DELETE FROM sessoes WHERE estado = 'terminada' AND terminada_em < ?`,
		`DELETE FROM avisos WHERE em < ?`,
		`DELETE FROM mensagens WHERE criada_em < ?`,
		`DELETE FROM permissoes WHERE pedida_em < ?`,
		`DELETE FROM decisoes WHERE fechada_em IS NOT NULL AND criada_em < ?`,
	} {
		if _, err := q.db.Exec(s, l); err != nil {
			return err
		}
	}
	return nil
}
