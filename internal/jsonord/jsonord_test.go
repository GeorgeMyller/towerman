package jsonord

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestIdaEVoltaPreservaOrdem(t *testing.T) {
	entrada := `{
  "z": 1,
  "a": {
    "y": [true, null, "x", 1.50],
    "b": {}
  },
  "m": []
}
`
	v, err := Descodificar([]byte(entrada))
	if err != nil {
		t.Fatal(err)
	}
	saida, err := Codificar(v)
	if err != nil {
		t.Fatal(err)
	}
	esperado := `{
  "z": 1,
  "a": {
    "y": [
      true,
      null,
      "x",
      1.50
    ],
    "b": {}
  },
  "m": []
}
`
	if string(saida) != esperado {
		t.Fatalf("saída inesperada:\n%s", saida)
	}
}

func TestValorEquivalenteAoEncodingJSON(t *testing.T) {
	entrada := `{"hooks":{"Stop":[{"matcher":"","hooks":[{"type":"command","command":"node \"x.js\" <a&b>","timeout":5}]}]},"env":{"A":"1"}}`
	v, err := Descodificar([]byte(entrada))
	if err != nil {
		t.Fatal(err)
	}
	saida, err := Codificar(v)
	if err != nil {
		t.Fatal(err)
	}
	var a, b any
	json.Unmarshal([]byte(entrada), &a)
	json.Unmarshal(saida, &b)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("valores diferentes:\n%v\n%v", a, b)
	}
}

func TestErros(t *testing.T) {
	for _, s := range []string{`{"a":}`, `{"a":1} x`, ``, `[1,`} {
		if _, err := Descodificar([]byte(s)); err == nil {
			t.Errorf("esperava erro para %q", s)
		}
	}
}

func TestObjetoDefinirRemover(t *testing.T) {
	o := &Objeto{}
	o.Definir("a", "1")
	o.Definir("b", "2")
	o.Definir("a", "3")
	o.Remover("b")
	if len(o.Pares) != 1 || o.Pares[0].Valor != "3" {
		t.Fatalf("estado inesperado: %+v", o.Pares)
	}
	if _, ok := o.Obter("b"); ok {
		t.Fatal("b devia ter sido removido")
	}
}
