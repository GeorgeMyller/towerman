# Towerman

[English](README.md)

O radar dos seus agentes de código (Claude Code, Codex, Antigravity `agy` e o agente do Antigravity IDE) em todos os seus computadores, numa só tela. Aprove ou negue pedidos, escolha a resposta de uma pergunta, mande texto ao agente e traga o terminal para a frente, pelo iPhone, pelo pulso ou pela barra de menus do Mac.

## Como funciona

```
 Mac                                   Linux / Windows
 ┌───────────────────────────┐         ┌──────────────────────────┐
 │ Towerman.app (barra menus)│         │ towerman (serviço)       │
 │  └ towerman serve         │         │  ├ 127.0.0.1:7777 painel │
 │     ├ 127.0.0.1:7777 painel│        │  └ :7352 aparelhos (/v1) │
 │     └ :7352 aparelhos (/v1)│        │ hooks: Claude/Codex/agy  │
 │ hooks: Claude/Codex/agy   │         └────────────┬─────────────┘
 └────────────┬──────────────┘                      │
              │  Wi-Fi em casa · Tailscale ou relay E2EE próprio fora
              └──────────────────┬──────────────────┘
                         ┌───────▼────────┐  WatchConnectivity  ┌──────────────┐
                         │ iPhone (deck)  │ ──────────────────▶ │ Apple Watch  │
                         └────────────────┘                     └──────────────┘
```

- **O servidor é o `towerman`** (Go), um por computador. Os hooks dos agentes avisam-no de cada evento. Ele guarda o estado, espera as decisões e serve a API `/v1` na porta 7352.
- **O iPhone junta todos os computadores** num só radar e passa o estado ao relógio.
- **O app do Mac traz o servidor dentro.** Na primeira abertura, instala os hooks, registra o serviço e se pareia sozinho.
- **Sem nuvem por padrão.** Tudo fica na sua rede. O acesso remoto é opcional: Tailscale, relay próprio ou túnel rápido da Cloudflare.

## O que funciona com cada agente

| | Claude Code | Codex | Antigravity (`agy` e IDE) |
|---|---|---|---|
| Aparecer com estado e pedido | ✅ | ✅ | ✅ |
| Aprovar/negar e responder perguntas | ✅ | ✅ | ✅ comandos, quando você está longe do computador |
| Mandar texto ao agente | ✅ | ✅ | ✅ |
| Focar o terminal | ✅ | ✅ | ✅ |
| Lançar sessão nova pelo iPhone ou relógio | ✅ | ✅ sem aprovações, dentro do sandbox | ✅ só comandos de `permissions.allow` |

**Decisão no pulso:** muitos agentes terminam o turno com uma pergunta em texto livre («Digite CONTINUAR», «Escolha 1, 2 ou 3», «Posso começar?»). O Towerman lê o fim da resposta e mostra um botão por opção no relógio, no iPhone, no Mac e no deck web. Funciona com tmux, Terminal.app, iTerm2 e terminais do VS Code / Antigravity (pela extensão).

## Início rápido

1. **Servidor em cada computador:**
   - Mac: baixe o DMG em [Releases](https://github.com/GeorgeMyller/towerman/releases) e abra o app.
   - Linux / Windows:
     ```sh
     go install github.com/GeorgeMyller/towerman/cmd/towerman@latest
     towerman install --sem-extensao
     ```
2. **App do iPhone e do relógio:** compile com a sua conta Apple (a gratuita serve). Veja [docs/pt-BR/assinatura-apple.md](docs/pt-BR/assinatura-apple.md).
3. **Pareie** na mesma Wi-Fi: `towerman pair` e leia o QR.
4. **Fora de casa (opcional):** veja [docs/pt-BR/acesso-remoto.md](docs/pt-BR/acesso-remoto.md).

Guia completo: [docs/pt-BR/instalar.md](docs/pt-BR/instalar.md).

## Comandos

| Comando | O que faz |
|---|---|
| `towerman install` / `uninstall` | Instala ou remove os hooks e o serviço |
| `towerman pair` | Mostra o QR de pareamento (`--link` para um link em texto) |
| `towerman aparelhos` | Lista os aparelhos pareados (`revogar <nome>` para revogar) |
| `towerman deck` | Abre o deck web |
| `towerman painel` | Abre o painel em `http://127.0.0.1:7777/` |
| `towerman ls` | Sessões vivas por repo e worktree |
| `towerman wt` / `wt limpar` | Inventário de worktrees e limpeza guiada |
| `towerman version` | Versão |

## Configuração

| Variável / chave do `config.json` | Efeito |
|---|---|
| `TOWERMAN=off` | Desliga o hook numa sessão (`TOWERMAN=off claude`) |
| `TOWERMAN_HOME` | Pasta de dados (padrão `~/.claude/towerman`) |
| `TOWERMAN_RELAY_URL` / `relay_url` | O seu relay. Vazio = sem relay |
| `TOWERMAN_RELAY_TOKEN` / `relay_token` | Token aceito pelo seu relay |
| `TOWERMAN_TUNEL` | `cloudflare` para túnel rápido, `nao` para desligar o acesso remoto |
| `"decisoes": false` | Desliga a detecção de decisões em texto livre |

## Estrutura

| Pasta | O quê |
|---|---|
| `cmd/towerman/` | O binário `towerman` |
| `cmd/towerman-relay/` | O relay WebSocket com cifra de ponta a ponta |
| `internal/` | Serviço, quadro SQLite, hooks dos agentes, instalador, painel e deck web |
| `apple/` | Apps do iPhone, do Apple Watch e do Mac (XcodeGen) |
| `docs/` | Guias em inglês e português |

## Contribuir e segurança

Veja [CONTRIBUTING.md](CONTRIBUTING.md) e [SECURITY.md](SECURITY.md).

## Licença

[MIT](LICENSE)
