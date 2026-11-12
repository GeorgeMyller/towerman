// Package jsonord descodifica e codifica JSON mantendo a ordem das chaves dos objetos.
package jsonord

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

type Par struct {
	Chave string
	Valor any
}

// Objeto é um objeto JSON com as chaves pela ordem original.
// Os outros valores são string, json.Number, bool, nil ou []any.
type Objeto struct {
	Pares []Par
}

func (o *Objeto) Obter(chave string) (any, bool) {
	for _, p := range o.Pares {
		if p.Chave == chave {
			return p.Valor, true
		}
	}
	return nil, false
}

func (o *Objeto) Definir(chave string, valor any) {
	for i, p := range o.Pares {
		if p.Chave == chave {
			o.Pares[i].Valor = valor
			return
		}
	}
	o.Pares = append(o.Pares, Par{chave, valor})
}

func (o *Objeto) Remover(chave string) {
	for i, p := range o.Pares {
		if p.Chave == chave {
			o.Pares = append(o.Pares[:i], o.Pares[i+1:]...)
			return
		}
	}
}

func Descodificar(dados []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(dados))
	dec.UseNumber()
	v, err := valor(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("conteúdo extra depois do valor JSON")
	}
	return v, nil
}

func valor(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, posicao(dec, err)
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			o := &Objeto{}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, posicao(dec, err)
				}
				k, ok := kt.(string)
				if !ok {
					return nil, posicao(dec, errors.New("chave não é string"))
				}
				v, err := valor(dec)
				if err != nil {
					return nil, err
				}
				o.Pares = append(o.Pares, Par{k, v})
			}
			if _, err := dec.Token(); err != nil {
				return nil, posicao(dec, err)
			}
			return o, nil
		case '[':
			a := []any{}
			for dec.More() {
				v, err := valor(dec)
				if err != nil {
					return nil, err
				}
				a = append(a, v)
			}
			if _, err := dec.Token(); err != nil {
				return nil, posicao(dec, err)
			}
			return a, nil
		}
		return nil, posicao(dec, fmt.Errorf("delimitador inesperado %q", t))
	default:
		return t, nil
	}
}

func posicao(dec *json.Decoder, err error) error {
	return fmt.Errorf("JSON inválido perto do byte %d: %w", dec.InputOffset(), err)
}

// Codificar devolve o valor indentado com dois espaços, como o Claude Code escreve o settings.json.
func Codificar(v any) ([]byte, error) {
	var b strings.Builder
	if err := escrever(&b, v, 0); err != nil {
		return nil, err
	}
	b.WriteByte('\n')
	return []byte(b.String()), nil
}

func escrever(b *strings.Builder, v any, nivel int) error {
	recuo := func(n int) { b.WriteString(strings.Repeat("  ", n)) }
	switch t := v.(type) {
	case *Objeto:
		if len(t.Pares) == 0 {
			b.WriteString("{}")
			return nil
		}
		b.WriteString("{\n")
		for i, p := range t.Pares {
			recuo(nivel + 1)
			k, _ := marshal(p.Chave)
			b.Write(k)
			b.WriteString(": ")
			if err := escrever(b, p.Valor, nivel+1); err != nil {
				return err
			}
			if i < len(t.Pares)-1 {
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}
		recuo(nivel)
		b.WriteByte('}')
	case []any:
		if len(t) == 0 {
			b.WriteString("[]")
			return nil
		}
		b.WriteString("[\n")
		for i, e := range t {
			recuo(nivel + 1)
			if err := escrever(b, e, nivel+1); err != nil {
				return err
			}
			if i < len(t)-1 {
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}
		recuo(nivel)
		b.WriteByte(']')
	default:
		j, err := marshal(t)
		if err != nil {
			return err
		}
		b.Write(j)
	}
	return nil
}

func marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
