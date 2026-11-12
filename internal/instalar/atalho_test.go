package instalar

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInstalarAtalhoLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("o atalho só existe no Linux")
	}
	d := t.TempDir()
	t.Setenv("XDG_DATA_HOME", d)
	if _, err := InstalarAtalho("/opt/towerman/towerman"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(d, "applications", "towerman.desktop"))
	if err != nil || !strings.Contains(string(b), `Exec="/opt/towerman/towerman" deck`) {
		t.Fatalf("desktop errado: %s %v", b, err)
	}
	RemoverAtalho()
	if _, err := os.Stat(filepath.Join(d, "applications", "towerman.desktop")); err == nil {
		t.Fatal("devia remover")
	}
}

func TestIcoDePNG(t *testing.T) {
	ico := icoDePNG(iconeTowerman)
	if len(ico) != 22+len(iconeTowerman) {
		t.Fatalf("tamanho %d, esperava %d", len(ico), 22+len(iconeTowerman))
	}
	if string(ico[:6]) != "\x00\x00\x01\x00\x01\x00" {
		t.Fatalf("ICONDIR inválido: % x", ico[:6])
	}
	if n := binary.LittleEndian.Uint32(ico[14:18]); int(n) != len(iconeTowerman) {
		t.Fatalf("tamanho dos dados %d", n)
	}
	if off := binary.LittleEndian.Uint32(ico[18:22]); off != 22 {
		t.Fatalf("deslocamento %d", off)
	}
	if string(ico[22:26]) != "\x89PNG" {
		t.Fatal("o PNG não começa no deslocamento 22")
	}
}
