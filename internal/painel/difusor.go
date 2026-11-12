package painel

import (
	"sync"
	"time"
)

// Difusor avisa os clientes do painel de que o quadro mudou, agregando rajadas de eventos.
// Os clientes remotos (iPhone, relay) só recebem logo o que é urgente; o resto espera
// Secundaria, para não acordar o aparelho a cada ferramenta.
type Difusor struct {
	Agregacao  time.Duration
	Secundaria time.Duration

	mu       sync.Mutex
	clientes map[chan struct{}]struct{}
	remotos  map[chan struct{}]struct{}
	pendente bool
	urgencia func() string
	// ultUrgente é a assinatura urgente do último aviso aos remotos; atraso, o aviso adiado.
	ultUrgente string
	atraso     *time.Timer
	// muRemoto põe em fila as avaliações da urgência, que leem o quadro fora de mu.
	muRemoto sync.Mutex
}

func NovoDifusor() *Difusor {
	return &Difusor{Agregacao: 250 * time.Millisecond, Secundaria: 2 * time.Second,
		clientes: map[chan struct{}]struct{}{}, remotos: map[chan struct{}]struct{}{}}
}

func (d *Difusor) Subscrever() (<-chan struct{}, func()) {
	return d.juntar(d.clientes)
}

// SubscreverRemoto avisa logo quando urgencia() muda; senão, no máximo uma vez por Secundaria
// e sempre no fim, para o último estado chegar. urgencia é a mesma para todos os remotos.
func (d *Difusor) SubscreverRemoto(urgencia func() string) (<-chan struct{}, func()) {
	d.mu.Lock()
	d.urgencia = urgencia
	d.mu.Unlock()
	return d.juntar(d.remotos)
}

func (d *Difusor) juntar(grupo map[chan struct{}]struct{}) (<-chan struct{}, func()) {
	c := make(chan struct{}, 1)
	d.mu.Lock()
	grupo[c] = struct{}{}
	d.mu.Unlock()
	return c, func() {
		d.mu.Lock()
		delete(grupo, c)
		d.mu.Unlock()
	}
}

func (d *Difusor) Mudou() {
	d.mu.Lock()
	if d.pendente {
		d.mu.Unlock()
		return
	}
	d.pendente = true
	d.mu.Unlock()
	time.AfterFunc(d.Agregacao, d.emitir)
}

func (d *Difusor) emitir() {
	d.mu.Lock()
	d.pendente = false
	avisar(d.clientes)
	urgencia, haRemotos := d.urgencia, len(d.remotos) > 0
	d.mu.Unlock()
	if !haRemotos {
		return
	}
	d.muRemoto.Lock()
	defer d.muRemoto.Unlock()
	var a string
	if urgencia != nil {
		a = urgencia()
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if urgencia == nil || a != d.ultUrgente {
		// O aviso urgente leva também o acessório pendente.
		d.ultUrgente = a
		if d.atraso != nil {
			d.atraso.Stop()
			d.atraso = nil
		}
		avisar(d.remotos)
		return
	}
	if d.atraso == nil {
		d.atraso = time.AfterFunc(d.Secundaria, d.emitirRemotos)
	}
}

func (d *Difusor) emitirRemotos() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.atraso = nil
	avisar(d.remotos)
}

func avisar(grupo map[chan struct{}]struct{}) {
	for c := range grupo {
		select {
		case c <- struct{}{}:
		default:
		}
	}
}

// Subscritores conta os painéis abertos no browser.
func (d *Difusor) Subscritores() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.clientes)
}
