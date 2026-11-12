package bench

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"time"
)

func Executar(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("bench", flag.ExitOnError)
	n := fs.Int("n", 1000, "número de invocações")
	evento := fs.String("evento", "PostToolUse", "hook_event_name do evento sintético")
	fs.Parse(args)
	if *n < 1 {
		return fmt.Errorf("-n tem de ser positivo")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cwd, _ := os.Getwd()
	duracoes := make([]time.Duration, 0, *n)
	for i := 0; i < *n; i++ {
		entrada := fmt.Sprintf(`{"session_id":"bench-%d","hook_event_name":%q,"cwd":%q,"tool_name":"Edit","tool_input":{"file_path":"bench.txt"}}`,
			os.Getpid(), *evento, cwd)
		cmd := exec.Command(exe, "hook")
		cmd.Stdin = bytes.NewReader([]byte(entrada))
		inicio := time.Now()
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("invocação %d: %w", i, err)
		}
		duracoes = append(duracoes, time.Since(inicio))
	}
	sort.Slice(duracoes, func(i, j int) bool { return duracoes[i] < duracoes[j] })
	p := func(q float64) float64 {
		i := int(q*float64(len(duracoes))+0.5) - 1
		if i < 0 {
			i = 0
		}
		return float64(duracoes[i].Microseconds()) / 1000
	}
	fmt.Fprintf(out, "%d invocações de \"towerman hook\" (%s): p50 %.2f ms · p95 %.2f ms · p99 %.2f ms · máx %.2f ms\n",
		*n, *evento, p(0.50), p(0.95), p(0.99), p(1))
	return nil
}
