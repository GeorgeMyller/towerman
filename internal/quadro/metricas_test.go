package quadro

import (
	"path/filepath"
	"testing"
	"time"
)

func abrirMetricas(t *testing.T) *Quadro {
	t.Helper()
	q, err := Abrir(filepath.Join(t.TempDir(), "quadro.db"))
	if err != nil {
		t.Fatal(err)
	}
	// No Windows, a pasta temporária só se apaga com a base fechada.
	t.Cleanup(func() { q.Fechar() })
	return q
}

func (q *Quadro) executar(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := q.db.Exec(sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

func TestMetricasVazias(t *testing.T) {
	q := abrirMetricas(t)
	agora := time.Now()
	m, err := q.Metricas(agora.Add(-7*24*time.Hour), agora)
	if err != nil {
		t.Fatal(err)
	}
	if m.Atencao.TempoPermissao.Casos != 0 || m.Atencao.TempoPermissao.MedianaS != nil || m.Atencao.TempoPermissao.P90S != nil {
		t.Fatalf("tempo sem dados: %+v", m.Atencao.TempoPermissao)
	}
	if m.Atencao.PorCanal == nil || len(m.Atencao.PorCanal) != 0 {
		t.Fatalf("por canal vazio deve ser lista vazia: %#v", m.Atencao.PorCanal)
	}
	if m.Atencao.PermissoesExpiradas != (Proporcao{}) || m.Colisoes.ColisoesACommit.Casos != 0 {
		t.Fatalf("sem dados: %+v", m)
	}
}

func TestMetricasPermissoes(t *testing.T) {
	q := abrirMetricas(t)
	agora := time.Now().UTC()
	base := agora.Add(-time.Hour)
	permissao := func(minutos int, decisao, canal, origem string) {
		q.executar(t, `INSERT INTO permissoes(session_id, ferramenta, resumo, decisao, pedida_em, decidida_em, canal, origem)
			VALUES('s1', 'Bash', '', ?, ?, ?, ?, ?)`, decisao, agoraTxt(base), agoraTxt(base.Add(time.Duration(minutos)*time.Minute)), canal, origem)
	}
	// Exemplo do RF-03: respostas em 1, 2 e 10 min → mediana 2 min.
	permissao(1, "allow", "ap1", OrigemRelogio)
	permissao(2, "deny", CanalPainel, "")
	permissao(10, "terminal", CanalTerminal, "")
	permissao(10, "expirada", "", "")
	// Fora do período: não conta.
	q.executar(t, `INSERT INTO permissoes(session_id, ferramenta, resumo, decisao, pedida_em, decidida_em)
		VALUES('s1', 'Bash', '', 'allow', ?, ?)`, agoraTxt(agora.Add(-10*24*time.Hour)), agoraTxt(agora.Add(-10*24*time.Hour)))

	m, err := q.Metricas(agora.Add(-7*24*time.Hour), agora)
	if err != nil {
		t.Fatal(err)
	}
	tp := m.Atencao.TempoPermissao
	if tp.Casos != 3 || tp.MedianaS == nil || *tp.MedianaS != 120 || tp.P90S != nil {
		t.Fatalf("tempo de permissão: %+v", tp)
	}
	if m.Atencao.PermissoesExpiradas != (Proporcao{1, 4}) {
		t.Fatalf("expiradas: %+v", m.Atencao.PermissoesExpiradas)
	}
	if m.Atencao.ForaDoTerminal != (Proporcao{2, 3}) {
		t.Fatalf("fora do terminal: %+v", m.Atencao.ForaDoTerminal)
	}
	if len(m.Atencao.PorCanal) != 2 {
		t.Fatalf("por canal: %+v", m.Atencao.PorCanal)
	}
	for _, l := range m.Atencao.PorCanal {
		if l.Canal == CanalTerminal || l.Respostas != 1 {
			t.Fatalf("linha de canal: %+v", l)
		}
	}
}

func TestResumirP90(t *testing.T) {
	r := resumir([]int64{10, 1, 9, 2, 8, 3, 7, 4, 6, 5})
	if r.Casos != 10 || *r.MedianaS != 5 || *r.P90S != 9 {
		t.Fatalf("resumo: casos=%d mediana=%d p90=%d", r.Casos, *r.MedianaS, *r.P90S)
	}
	if r := resumir([]int64{1, 2, 3, 4}); r.P90S != nil {
		t.Fatalf("p90 com 4 casos deve ser nulo")
	}
}

func TestMetricasDecisoes(t *testing.T) {
	q := abrirMetricas(t)
	agora := time.Now().UTC()
	base := agora.Add(-time.Hour)
	decisao := func(desfecho, forma string) {
		q.executar(t, `INSERT INTO decisoes(session_id, padrao, lingua, criada_em, fechada_em, desfecho, forma)
			VALUES('s1', 'menu', 'pt', ?, ?, ?, ?)`, agoraTxt(base), agoraTxt(base.Add(2*time.Minute)), desfecho, forma)
	}
	// Exemplo do RF-08: 1 descartada, 3 respondidas, 1 terminal → 20%.
	decisao(DecisaoDescartada, "")
	decisao(DecisaoRespondida, FormaOpcao)
	decisao(DecisaoRespondida, FormaOpcao)
	decisao(DecisaoRespondida, FormaTexto)
	decisao(DecisaoNoTerminal, "")
	decisao(DecisaoSubstituida, "")
	decisao(DecisaoTerminada, "")

	m, err := q.Metricas(agora.Add(-7*24*time.Hour), agora)
	if err != nil {
		t.Fatal(err)
	}
	if m.Decisoes.Descartadas != (Proporcao{1, 5}) {
		t.Fatalf("descartadas: %+v", m.Decisoes.Descartadas)
	}
	if m.Decisoes.PorBotao != (Proporcao{2, 3}) {
		t.Fatalf("por botão: %+v", m.Decisoes.PorBotao)
	}
	if m.Atencao.TempoDecisao.Casos != 4 || *m.Atencao.TempoDecisao.MedianaS != 120 {
		t.Fatalf("tempo de decisão: %+v", m.Atencao.TempoDecisao)
	}
	// Decisões antigas, sem canal, contam como «desconhecido» (canal vazio).
	if len(m.Atencao.PorCanal) != 1 || m.Atencao.PorCanal[0].Canal != "" || m.Atencao.PorCanal[0].Respostas != 4 {
		t.Fatalf("por canal: %+v", m.Atencao.PorCanal)
	}
}

func TestMetricasColisoesACommit(t *testing.T) {
	q := abrirMetricas(t)
	agora := time.Now().UTC()
	aviso := agora.Add(-48 * time.Hour)
	toque := func(sessao, caminho, tipo, branch string, em time.Time) {
		q.executar(t, `INSERT INTO toques(session_id, repo, worktree, branch, caminho, tipo, ferramenta, em)
			VALUES(?, 'r', 'w', ?, ?, ?, 'x', ?)`, sessao, branch, caminho, tipo, agoraTxt(em))
	}
	novoAviso := func(de, para, caminho string, em time.Time) {
		q.executar(t, `INSERT INTO avisos(de, para, regra, caminho, motivo, em) VALUES(?, ?, 2, ?, '', ?)`, de, para, caminho, agoraTxt(em))
	}
	// Exemplo do RF-11: aviso às 10h e commit da sessão avisada às 15h, mesma branch → 1 colisão.
	toque("b", "a.go", "edit", "main", aviso.Add(-time.Hour))
	novoAviso("a", "b", "a.go", aviso)
	toque("a", "commit", "git", "main", aviso.Add(5*time.Hour))
	// Commit 25 h depois → não conta.
	toque("d", "c.go", "edit", "main", aviso.Add(-time.Hour))
	novoAviso("c", "d", "c.go", aviso)
	toque("c", "commit", "git", "main", aviso.Add(25*time.Hour))
	// Commit noutra branch → não conta.
	toque("f", "e.go", "edit", "dev", aviso.Add(-time.Hour))
	novoAviso("e", "f", "e.go", aviso)
	toque("e", "commit", "git", "main", aviso.Add(time.Hour))
	q.executar(t, `UPDATE avisos SET falso_alarme = 1 WHERE de = 'e'`)

	m, err := q.Metricas(agora.Add(-7*24*time.Hour), agora)
	if err != nil {
		t.Fatal(err)
	}
	if m.Colisoes.ColisoesACommit.Casos != 1 || m.Colisoes.ColisoesACommit.PorSemana != 1 {
		t.Fatalf("colisões a commit: %+v", m.Colisoes.ColisoesACommit)
	}
	if m.Colisoes.FalsosAlarmes != (Proporcao{1, 3}) {
		t.Fatalf("falsos alarmes: %+v", m.Colisoes.FalsosAlarmes)
	}
}

func TestMetricasFiabilidade(t *testing.T) {
	q := abrirMetricas(t)
	agora := time.Now().UTC()
	fim := agoraTxt(agora.Add(-time.Hour))
	// Exemplo do RF-12: 4 lançadas, 3 com Stop → 75%.
	for i, id := range []string{"l1", "l2", "l3", "l4"} {
		q.executar(t, `INSERT INTO sessoes(session_id, agente, nome, pid, cwd, repo, worktree, branch, terminal, transcript_path,
			resumo, estado, iniciada_em, visto_em, terminada_em, lancada_por)
			VALUES(?, 'claude', '', 0, '', '', '', '', '', '', '', 'terminada', ?, ?, ?, 'ap1')`, id, fim, fim, fim)
		if i < 3 {
			q.executar(t, `INSERT INTO eventos(session_id, tipo, recebido_em, em) VALUES(?, 'Stop', ?, ?)`, id, fim, fim)
		}
	}
	// Sessão local: não conta.
	q.executar(t, `INSERT INTO sessoes(session_id, agente, nome, pid, cwd, repo, worktree, branch, terminal, transcript_path,
		resumo, estado, iniciada_em, visto_em, terminada_em) VALUES('loc', 'claude', '', 0, '', '', '', '', '', '', '', 'terminada', ?, ?, ?)`, fim, fim, fim)
	for _, estado := range []string{MensagemEntregue, MensagemEntregue, MensagemEntregue, MensagemFalhada, MensagemPendente} {
		q.executar(t, `INSERT INTO mensagens(para, texto, caminho, estado, criada_em) VALUES('s', 'x', 'stop', ?, ?)`, estado, fim)
	}
	q.executar(t, `INSERT INTO fluxos(nome, max_passos, estado, criado_em) VALUES('f1', 3, 'limite', ?), ('f2', 3, 'ativo', ?)`, fim, fim)

	m, err := q.Metricas(agora.Add(-7*24*time.Hour), agora)
	if err != nil {
		t.Fatal(err)
	}
	if m.Fiabilidade.LancadasComSucesso != (Proporcao{3, 4}) {
		t.Fatalf("lançadas: %+v", m.Fiabilidade.LancadasComSucesso)
	}
	if m.Fiabilidade.MensagensFalhadas != (Proporcao{1, 4}) {
		t.Fatalf("mensagens: %+v", m.Fiabilidade.MensagensFalhadas)
	}
	if m.Fiabilidade.FluxosNoLimite != (Proporcao{1, 2}) {
		t.Fatalf("fluxos: %+v", m.Fiabilidade.FluxosNoLimite)
	}
}

func TestReabrirDecisao(t *testing.T) {
	q := abrirMetricas(t)
	agora := time.Now()
	d := Decisao{SessionID: "s1", Pergunta: "Qual?", Padrao: "menu", Lingua: "pt", Opcoes: []OpcaoDecisao{{"1", "1"}}}
	id, err := q.AbrirDecisao(d, agora)
	if err != nil {
		t.Fatal(err)
	}
	d.ID = id
	if _, err := q.FecharDecisao(id, DecisaoRespondida, Resposta{Canal: "ap1", Forma: FormaTexto}, agora); err != nil {
		t.Fatal(err)
	}
	if err := q.ReabrirDecisao(d); err != nil {
		t.Fatal(err)
	}
	aberta, ok, err := q.DecisaoAberta("s1")
	if err != nil || !ok || aberta.ID != id || aberta.Pergunta != "Qual?" || len(aberta.Opcoes) != 1 {
		t.Fatalf("decisão reaberta: %+v %v %v", aberta, ok, err)
	}
	var canal, forma string
	q.db.QueryRow(`SELECT canal, forma FROM decisoes WHERE id = ?`, id).Scan(&canal, &forma)
	if canal != "" || forma != "" {
		t.Fatalf("a reabertura deve limpar canal e forma: %q %q", canal, forma)
	}
}
