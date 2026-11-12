package servico

import (
	"context"
	"os"
	"path/filepath"
	"time"
)

// ritmo é um intervalo que duplica enquanto não há trabalho, até max, e volta à base
// quando há. Poupa a bateria com o servidor parado sem atrasar quem está a trabalhar.
type ritmo struct {
	base, max, atual time.Duration
}

func novoRitmo(base, max time.Duration) *ritmo {
	if max < base {
		max = base
	}
	return &ritmo{base: base, max: max, atual: base}
}

// seguinte devolve o próximo intervalo depois de uma volta com ou sem trabalho.
func (r *ritmo) seguinte(ocupado bool) time.Duration {
	if ocupado {
		r.atual = r.base
	} else {
		r.atual = min(r.atual*2, r.max)
	}
	return r.atual
}

// acordar volta à base; diz se o ritmo estava abrandado, para quem tem de rearmar o relógio.
func (r *ritmo) acordar() bool {
	if r.atual <= r.base {
		return false
	}
	r.atual = r.base
	return true
}

// acordarFundo avisa o ciclo de fundo de que chegou um evento; nunca bloqueia o pedido.
func (s *Servico) acordarFundo() {
	select {
	case s.despertar <- struct{}{}:
	default:
	}
}

// diarioPendente diz se há linhas por ingerir: o diário atual com conteúdo ou restos renomeados.
func diarioPendente(home string) bool {
	if st, err := os.Stat(filepath.Join(home, "diario.jsonl")); err == nil && st.Size() > 0 {
		return true
	}
	restos, _ := filepath.Glob(filepath.Join(home, "diario.[0-9]*.jsonl"))
	return len(restos) > 0
}

// correrFundo ingere o diário, vigia os pids e aplica a retenção. O diário e a vigia abrandam
// quando não há nada a fazer; um evento de hook devolve-os ao ritmo base.
func (s *Servico) correrFundo(ctx context.Context) {
	rd := novoRitmo(s.op.IntervaloDiario, s.op.IntervaloDiarioMax)
	rv := novoRitmo(s.op.IntervaloVigia, s.op.IntervaloVigiaMax)

	if diarioPendente(s.op.Home) {
		s.ingerirDiario(ctx)
	}
	s.enfileirar(ctx, s.reter)
	// A vigia corre no escritor; o resultado volta por vigiado e só então se rearma o relógio,
	// para nunca haver duas na fila.
	vigiado := make(chan bool, 1)
	vigiaEmCurso, acordouEmCurso := true, false
	s.enfileirar(ctx, func() { vigiado <- s.vigiar() })

	diario := time.NewTimer(rd.atual)
	vigia := time.NewTimer(rv.atual)
	vigia.Stop()
	retencao := time.NewTicker(s.op.IntervaloRetencao)
	defer diario.Stop()
	defer vigia.Stop()
	defer retencao.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-diario.C:
			pendente := diarioPendente(s.op.Home)
			if pendente {
				s.ingerirDiario(ctx)
			}
			diario.Reset(rd.seguinte(pendente))
		case <-vigia.C:
			vigiaEmCurso, acordouEmCurso = true, false
			s.enfileirar(ctx, func() { vigiado <- s.vigiar() })
		case vivas := <-vigiado:
			vigiaEmCurso = false
			vigia.Reset(rv.seguinte(vivas || acordouEmCurso))
		case <-s.despertar:
			// Só se rearma um relógio abrandado; rearmar sempre adiava a volta a cada evento.
			if rd.acordar() {
				diario.Reset(rd.atual)
			}
			if vigiaEmCurso {
				acordouEmCurso = true
			} else if rv.acordar() {
				vigia.Reset(rv.atual)
			}
		case <-retencao.C:
			s.enfileirar(ctx, s.reter)
		}
	}
}
