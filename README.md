# Towerman

**[Download and docs →](https://georgemyller.github.io/towerman/)** · [Português (Brasil)](README.pt-BR.md)

A radar for your coding agents (Claude Code, Codex, Antigravity `agy` and the Antigravity IDE agent) across all your computers, on one screen. Approve or deny requests, pick an answer to a question, send text to an agent and bring its terminal to the front, from your iPhone, your wrist or the Mac menu bar.

> The apps and the CLI speak Portuguese. The docs are in English and Portuguese.

## How it works

```
 Mac                                   Linux / Windows
 ┌───────────────────────────┐         ┌──────────────────────────┐
 │ Towerman.app (menu bar)   │         │ towerman (service)       │
 │  └ towerman serve         │         │  ├ 127.0.0.1:7777 panel  │
 │     ├ 127.0.0.1:7777 panel│         │  └ :7352 devices (/v1)   │
 │     └ :7352 devices (/v1) │         │ hooks: Claude/Codex/agy  │
 │ hooks: Claude/Codex/agy   │         └────────────┬─────────────┘
 └────────────┬──────────────┘                      │
              │  Wi-Fi at home · Tailscale or your own E2EE relay away
              └──────────────────┬──────────────────┘
                         ┌───────▼────────┐  WatchConnectivity  ┌──────────────┐
                         │ iPhone (deck)  │ ──────────────────▶ │ Apple Watch  │
                         └────────────────┘                     └──────────────┘
```

- **The server is `towerman`** (Go), one per computer. Agent hooks report every event to it. It keeps the state, waits for decisions and serves the `/v1` API on port 7352.
- **The iPhone joins all computers** in one radar and forwards the state to the Watch.
- **The Mac app embeds the server.** On first launch it installs the hooks, registers the service and pairs itself.
- **No cloud by default.** Everything stays on your network. Remote access is opt-in: Tailscale, your own relay, or a Cloudflare quick tunnel.

## What works with each agent

| | Claude Code | Codex | Antigravity (`agy` and IDE) |
|---|---|---|---|
| Show up with state and request | ✅ | ✅ | ✅ |
| Approve/deny and answer questions | ✅ | ✅ | ✅ commands, while you are away from the computer |
| Send text to the agent | ✅ | ✅ | ✅ |
| Focus the terminal | ✅ | ✅ | ✅ |
| Launch a new session from iPhone or Watch | ✅ | ✅ no approvals, inside the sandbox | ✅ only `permissions.allow` commands |

**Decisions on the wrist:** many agents end a turn with a free-text question ("Type CONTINUE", "Pick 1, 2 or 3", "Shall I start?"). Towerman reads the end of the answer and shows one button per option on the Watch, the iPhone, the Mac and the web deck. It works with tmux, Terminal.app, iTerm2, and VS Code / Antigravity terminals (through the extension).

## Quick start

1. **Server on each computer:**
   - Mac: download the DMG from [Releases](https://github.com/GeorgeMyller/towerman/releases) and open the app.
   - Linux / Windows:
     ```sh
     go install github.com/GeorgeMyller/towerman/cmd/towerman@latest
     towerman install --sem-extensao
     ```
2. **iPhone and Watch app:** build it with your own Apple account (free works). See [docs/en/apple-signing.md](docs/en/apple-signing.md).
3. **Pair** on the same Wi-Fi: `towerman pair`, then scan the QR code.
4. **Away from home (optional):** see [docs/en/remote-access.md](docs/en/remote-access.md).

Full guide: [docs/en/install.md](docs/en/install.md).

## Commands

| Command | What it does |
|---|---|
| `towerman install` / `uninstall` | Install or remove hooks and the service |
| `towerman pair` | Show the pairing QR code (`--link` for a text link) |
| `towerman aparelhos` | List paired devices (`revogar <name>` to revoke) |
| `towerman deck` | Open the web deck |
| `towerman painel` | Open the panel at `http://127.0.0.1:7777/` |
| `towerman ls` | Live sessions by repo and worktree |
| `towerman wt` / `wt limpar` | Worktree inventory and guided cleanup |
| `towerman version` | Version |

## Configuration

| Variable / `config.json` key | Effect |
|---|---|
| `TOWERMAN=off` | Disable the hook for one session (`TOWERMAN=off claude`) |
| `TOWERMAN_HOME` | Data folder (default `~/.claude/towerman`) |
| `TOWERMAN_RELAY_URL` / `relay_url` | Your relay. Empty = no relay |
| `TOWERMAN_RELAY_TOKEN` / `relay_token` | Token accepted by your relay |
| `TOWERMAN_TUNEL` | `cloudflare` for a quick tunnel, `nao` to turn remote access off |
| `"decisoes": false` | Turn off free-text decision detection |

## Repository layout

| Folder | What |
|---|---|
| `cmd/towerman/` | The `towerman` binary |
| `cmd/towerman-relay/` | The end-to-end encrypted WebSocket relay |
| `internal/` | Service, SQLite board, agent hooks, installer, panel and web deck |
| `apple/` | iPhone, Apple Watch and Mac apps (XcodeGen) |
| `docs/` | Guides in English and Portuguese |

## Contributing and security

See [CONTRIBUTING.md](CONTRIBUTING.md) and [SECURITY.md](SECURITY.md).

## License

[MIT](LICENSE)
