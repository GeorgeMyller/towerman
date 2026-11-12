package servico

import (
	"encoding/json"
	"testing"

	"github.com/GeorgeMyller/towerman/internal/hook"
)

func TestLinhaLinux(t *testing.T) {
	cmd := "claude --resume 'abc-1'"
	// Sem tmux: o comportamento de antes.
	if got, want := linhaLinux("/home/g/proj", cmd, ""),
		`PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"; cd '/home/g/proj' && claude --resume 'abc-1'; exec "${SHELL:-sh}"`; got != want {
		t.Fatalf("sem tmux:\n got %s\nwant %s", got, want)
	}
	// Com tmux: o comando (com o PATH) corre dentro da sessão tmux, na pasta da sessão.
	dentro := `'PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"; claude --resume '\''abc-1'\''; exec "${SHELL:-sh}"'`
	want := `PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"; unset TMUX; tmux new-session -A -s 'towerman-abc-1' -c '/home/g/proj' ` +
		dentro + ` || exec "${SHELL:-sh}"`
	if got := linhaLinux("/home/g/proj", cmd, "towerman-abc-1"); got != want {
		t.Fatalf("com tmux:\n got %s\nwant %s", got, want)
	}
	want = `PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"; unset TMUX; tmux new-session -A -s 'towerman-abc-1' ` +
		dentro + ` || exec "${SHELL:-sh}"`
	if got := linhaLinux("", cmd, "towerman-abc-1"); got != want {
		t.Fatalf("sem pasta:\n got %s\nwant %s", got, want)
	}
}

func TestNomeSessaoTmux(t *testing.T) {
	casos := map[string]string{
		"1f0c2a9e-7b3d-4e1f-9a2b-c3d4e5f60718": "towerman-1f0c2a9e",
		"abc":                                  "towerman-abc",
		"a.b:c":                                "towerman-abc",
		"":                                     "towerman",
	}
	for id, want := range casos {
		if got := nomeSessaoTmux(id); got != want {
			t.Errorf("nomeSessaoTmux(%q) = %q, want %q", id, got, want)
		}
	}
}

// Uma sessão retomada fora do tmux não guarda o TMUX_PANE de antes: esse painel já é de outra.
func TestRetomadaLimpaTerminalAntigo(t *testing.T) {
	a := novoAmbiente(t, nil)
	ev := func(nome string, term hook.Terminal) {
		t.Helper()
		j, _ := json.Marshal(map[string]any{"session_id": "s1", "hook_event_name": nome})
		env := envelopeDe(j)
		env.Terminal = term
		if _, err := a.srv.proc.Processar(env); err != nil {
			t.Fatal(err)
		}
	}
	ev("SessionStart", hook.Terminal{Tmux: "/tmp/tmux-1000/default,1,0", TmuxPane: "%3"})
	if p := terminalDe(a.sessao("s1")).TmuxPane; p != "%3" {
		t.Fatalf("devia guardar o painel do tmux, veio %q", p)
	}
	// Um evento sem variáveis de terminal a meio da sessão não apaga o que se sabe.
	ev("UserPromptSubmit", hook.Terminal{})
	if p := terminalDe(a.sessao("s1")).TmuxPane; p != "%3" {
		t.Fatalf("o painel não devia mudar a meio, veio %q", p)
	}
	ev("SessionStart", hook.Terminal{})
	if p := terminalDe(a.sessao("s1")).TmuxPane; p != "" {
		t.Fatalf("a retomada fora do tmux devia limpar o painel, veio %q", p)
	}
}
