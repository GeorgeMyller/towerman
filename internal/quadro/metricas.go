package quadro

import (
	"math"
	"sort"
	"time"
)

// Métricas do PRD 2.0 (secção 4) calculadas sobre o quadro deste computador. Só contagens,
// durações e ids: nenhum texto da conversa (_reversa_forward/012-metricas/interfaces/metricas.md).

// Proporcao guarda numerador e denominador para o Mac poder somar vários computadores.
type Proporcao struct {
	Numerador   int `json:"numerador"`
	Denominador int `json:"denominador"`
}

// Tempo resume durações em segundos; com poucos casos o p90 não diz nada e fica nulo.
type Tempo struct {
	Casos    int    `json:"casos"`
	MedianaS *int64 `json:"mediana_s"`
	P90S     *int64 `json:"p90_s"`
}

// LinhaCanal conta as respostas fora do terminal por canal e origem; o nome vem do serviço.
type LinhaCanal struct {
	Canal     string `json:"canal"`
	Nome      string `json:"nome"`
	Origem    string `json:"origem"`
	Respostas int    `json:"respostas"`
}

type ColisoesACommit struct {
	Casos     int     `json:"casos"`
	PorSemana float64 `json:"por_semana"`
}

type Metricas struct {
	Computador string `json:"computador"`
	Desde      string `json:"desde"`
	Ate        string `json:"ate"`
	Dias       int    `json:"dias"`
	Atencao    struct {
		TempoPermissao      Tempo        `json:"tempo_permissao"`
		PermissoesExpiradas Proporcao    `json:"permissoes_expiradas"`
		ForaDoTerminal      Proporcao    `json:"fora_do_terminal"`
		PorCanal            []LinhaCanal `json:"por_canal"`
		TempoDecisao        Tempo        `json:"tempo_decisao"`
	} `json:"atencao"`
	Colisoes struct {
		FalsosAlarmes   Proporcao       `json:"falsos_alarmes"`
		ColisoesACommit ColisoesACommit `json:"colisoes_a_commit"`
	} `json:"colisoes"`
	Decisoes struct {
		Descartadas Proporcao `json:"descartadas"`
		PorBotao    Proporcao `json:"por_botao"`
	} `json:"decisoes"`
	Fiabilidade struct {
		LancadasComSucesso Proporcao `json:"lancadas_com_sucesso"`
		MensagensFalhadas  Proporcao `json:"mensagens_falhadas"`
		FluxosNoLimite     Proporcao `json:"fluxos_no_limite"`
	} `json:"fiabilidade"`
}

// minimoP90 é o número de casos abaixo do qual o p90 fica nulo (RN-03).
const minimoP90 = 5

// janelaCommit é o tempo em que um commit da sessão avisada ainda conta como colisão (RF-11).
const janelaCommit = 24 * time.Hour

// Metricas calcula todas as métricas entre desde e ate.
func (q *Quadro) Metricas(desde, ate time.Time) (Metricas, error) {
	var m Metricas
	m.Desde, m.Ate = agoraTxt(desde), agoraTxt(ate)
	d, a := m.Desde, m.Ate
	var err error
	proporcao := func(alvo *Proporcao, sql string, args ...any) {
		if err == nil {
			err = q.db.QueryRow(sql, args...).Scan(&alvo.Numerador, &alvo.Denominador)
		}
	}

	// Atenção.
	if m.Atencao.TempoPermissao, err = q.tempo(`SELECT pedida_em, decidida_em FROM permissoes
		WHERE pedida_em >= ? AND pedida_em < ? AND decisao IN ('allow', 'deny', 'terminal')`, d, a); err != nil {
		return m, err
	}
	proporcao(&m.Atencao.PermissoesExpiradas, `SELECT count(*) FILTER (WHERE decisao = 'expirada'), count(*)
		FROM permissoes WHERE pedida_em >= ? AND pedida_em < ? AND decisao != ''`, d, a)
	proporcao(&m.Atencao.ForaDoTerminal, `SELECT count(*) FILTER (WHERE decisao IN ('allow', 'deny')), count(*)
		FROM permissoes WHERE pedida_em >= ? AND pedida_em < ? AND decisao IN ('allow', 'deny', 'terminal')`, d, a)
	if err != nil {
		return m, err
	}
	if m.Atencao.PorCanal, err = q.porCanal(d, a); err != nil {
		return m, err
	}
	if m.Atencao.TempoDecisao, err = q.tempo(`SELECT criada_em, fechada_em FROM decisoes
		WHERE criada_em >= ? AND criada_em < ? AND desfecho IN ('respondida', 'terminal')`, d, a); err != nil {
		return m, err
	}

	// Colisões.
	proporcao(&m.Colisoes.FalsosAlarmes, `SELECT count(*) FILTER (WHERE falso_alarme = 1), count(*)
		FROM avisos WHERE em >= ? AND em < ?`, d, a)
	if err != nil {
		return m, err
	}
	if m.Colisoes.ColisoesACommit.Casos, err = q.colisoesACommit(d, a); err != nil {
		return m, err
	}
	if dias := ate.Sub(desde).Hours() / 24; dias > 0 {
		m.Colisoes.ColisoesACommit.PorSemana = math.Round(float64(m.Colisoes.ColisoesACommit.Casos)*7/dias*10) / 10
	}

	// Decisões.
	proporcao(&m.Decisoes.Descartadas, `SELECT count(*) FILTER (WHERE desfecho = 'descartada'), count(*)
		FROM decisoes WHERE criada_em >= ? AND criada_em < ? AND desfecho IN ('respondida', 'terminal', 'descartada')`, d, a)
	proporcao(&m.Decisoes.PorBotao, `SELECT count(*) FILTER (WHERE forma = 'opcao'), count(*)
		FROM decisoes WHERE criada_em >= ? AND criada_em < ? AND desfecho = 'respondida' AND forma != ''`, d, a)

	// Fiabilidade.
	proporcao(&m.Fiabilidade.LancadasComSucesso, `SELECT
		count(*) FILTER (WHERE EXISTS (SELECT 1 FROM eventos e WHERE e.session_id = s.session_id AND e.tipo = 'Stop')),
		count(*) FROM sessoes s
		WHERE lancada_por != '' AND estado = 'terminada' AND terminada_em >= ? AND terminada_em < ?`, d, a)
	proporcao(&m.Fiabilidade.MensagensFalhadas, `SELECT count(*) FILTER (WHERE estado = 'falhada'), count(*)
		FROM mensagens WHERE criada_em >= ? AND criada_em < ? AND estado IN ('entregue', 'falhada')`, d, a)
	proporcao(&m.Fiabilidade.FluxosNoLimite, `SELECT count(*) FILTER (WHERE estado = 'limite'), count(*)
		FROM fluxos WHERE criado_em >= ? AND criado_em < ?`, d, a)
	return m, err
}

// tempo lê pares (início, fim) e resume as durações; datas inválidas ficam de fora.
func (q *Quadro) tempo(sql string, args ...any) (Tempo, error) {
	rows, err := q.db.Query(sql, args...)
	if err != nil {
		return Tempo{}, err
	}
	defer rows.Close()
	var duracoes []int64
	for rows.Next() {
		var ini, fim string
		if err := rows.Scan(&ini, &fim); err != nil {
			return Tempo{}, err
		}
		i, err1 := time.Parse(FormatoData, ini)
		f, err2 := time.Parse(FormatoData, fim)
		if err1 != nil || err2 != nil || f.Before(i) {
			continue
		}
		duracoes = append(duracoes, int64(f.Sub(i).Round(time.Second)/time.Second))
	}
	if err := rows.Err(); err != nil {
		return Tempo{}, err
	}
	return resumir(duracoes), nil
}

// resumir calcula a mediana e o p90 (vizinho mais próximo) das durações.
func resumir(duracoes []int64) Tempo {
	t := Tempo{Casos: len(duracoes)}
	if len(duracoes) == 0 {
		return t
	}
	sort.Slice(duracoes, func(i, j int) bool { return duracoes[i] < duracoes[j] })
	n := len(duracoes)
	mediana := duracoes[n/2]
	if n%2 == 0 {
		mediana = (duracoes[n/2-1] + duracoes[n/2]) / 2
	}
	t.MedianaS = &mediana
	if n >= minimoP90 {
		p90 := duracoes[int(math.Ceil(0.9*float64(n)))-1]
		t.P90S = &p90
	}
	return t
}

// porCanal junta permissões e decisões respondidas fora do terminal, por canal e origem.
func (q *Quadro) porCanal(desde, ate string) ([]LinhaCanal, error) {
	rows, err := q.db.Query(`SELECT canal, origem, count(*) FROM (
		SELECT canal, origem FROM permissoes
			WHERE pedida_em >= ? AND pedida_em < ? AND decisao IN ('allow', 'deny')
		UNION ALL
		SELECT canal, origem FROM decisoes
			WHERE criada_em >= ? AND criada_em < ? AND desfecho IN ('respondida', 'descartada'))
		GROUP BY canal, origem ORDER BY count(*) DESC, canal, origem`, desde, ate, desde, ate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	r := []LinhaCanal{}
	for rows.Next() {
		var l LinhaCanal
		if err := rows.Scan(&l.Canal, &l.Origem, &l.Respostas); err != nil {
			return nil, err
		}
		r = append(r, l)
	}
	return r, rows.Err()
}

// colisoesACommit conta os avisos da regra 2 em que a sessão avisada fez commit na branch da
// colisão nas 24 h seguintes. É uma aproximação: os toques não dizem que ficheiros entram no commit.
func (q *Quadro) colisoesACommit(desde, ate string) (int, error) {
	rows, err := q.db.Query(`SELECT a.de, a.em, COALESCE((SELECT t.branch FROM toques t
			WHERE t.session_id = a.para AND t.caminho = a.caminho ORDER BY t.em DESC LIMIT 1), '')
		FROM avisos a WHERE a.regra = 2 AND a.em >= ? AND a.em < ?`, desde, ate)
	if err != nil {
		return 0, err
	}
	type aviso struct{ de, em, branch string }
	var avisos []aviso
	for rows.Next() {
		var v aviso
		if err := rows.Scan(&v.de, &v.em, &v.branch); err != nil {
			rows.Close()
			return 0, err
		}
		avisos = append(avisos, v)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	n := 0
	for _, v := range avisos {
		em, err := time.Parse(FormatoData, v.em)
		if err != nil || v.branch == "" {
			continue
		}
		var c int
		if err := q.db.QueryRow(`SELECT count(*) FROM toques WHERE session_id = ? AND tipo = 'git' AND caminho = 'commit'
			AND branch = ? AND em > ? AND em <= ?`, v.de, v.branch, v.em, agoraTxt(em.Add(janelaCommit))).Scan(&c); err != nil {
			return 0, err
		}
		if c > 0 {
			n++
		}
	}
	return n, nil
}
