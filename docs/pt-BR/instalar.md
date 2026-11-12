# Instalar

O Towerman tem duas partes:

- **O servidor** (`towerman`, em Go). Um por computador. Ele instala hooks nos seus agentes de código e guarda o estado deles.
- **Os apps** do iPhone, do Apple Watch e do Mac. O app do iPhone junta todos os computadores num só radar.

Instale o servidor em cada computador que você usa. Depois, compile o app do iPhone e pareie.

## Requisitos

| Parte | Precisa de |
|---|---|
| Servidor (Linux, Windows, CLI no Mac) | [Go](https://go.dev/dl/) 1.26 ou mais novo, Git |
| App do Mac | macOS 14 ou mais novo. Para compilar: Xcode 26, `brew install go xcodegen` |
| Apps do iPhone e do relógio | iOS 18 e watchOS 26. Um Mac com Xcode 26, uma conta Apple (a gratuita serve), Modo de Desenvolvedor no iPhone e no relógio |
| Agentes | Claude Code, Codex ou Antigravity (`agy` ou o IDE) |

## Mac

**Pela release (mais fácil):**

1. Baixe o `Towerman-<versão>.dmg` da [página de Releases](https://github.com/GeorgeMyller/towerman/releases).
2. Abra-o e arraste o Towerman para Aplicativos.
3. O app não é notarizado. Na primeira vez, clique com o botão direito e escolha **Abrir**. Se o macOS bloquear: **Ajustes do Sistema › Privacidade e Segurança › Abrir Mesmo Assim**.
4. O app instala os hooks, registra o servidor no launchd e aparece na barra de menus.
5. No menu (…), ligue **Abrir ao iniciar sessão** se quiser o app sempre ativo.

**Pelo código:**

```sh
git clone https://github.com/GeorgeMyller/towerman && cd towerman/apple
cp Config/Local.xcconfig.example Config/Local.xcconfig   # edite-o, veja assinatura-apple.md
xcodegen generate
tools/dmg.sh        # cria apple/dist/Towerman-<versão>.dmg
```

## Linux

```sh
go install github.com/GeorgeMyller/towerman/cmd/towerman@latest
towerman install --sem-extensao     # hooks, serviço systemd do usuário, atalho no menu
```

- O `go install` coloca o binário em `~/go/bin`. Adicione-o ao PATH: `echo 'export PATH="$HOME/go/bin:$PATH"' >> ~/.bashrc`.
- Ou baixe o `towerman-v<versão>-linux-<arch>` da página de Releases, confira com o `SHA256SUMS` e coloque-o no PATH.
- O `install` também instala o **tmux** se faltar (pede `sudo`). Sem terminal, ele mostra o comando a executar.
- O `install` liga o «linger» do systemd, para o serviço continuar depois do logout. Se pedir permissão: `sudo loginctl enable-linger $USER`.
- Tire o `--sem-extensao` para instalar também a extensão do VS Code / Antigravity. Ela é gerada localmente; não vem do Marketplace.

Rode os agentes dentro do tmux (`tmux new -s trabalho`). Assim o iPhone consegue escrever numa sessão parada.

## Windows

1. Baixe o `towerman-v<versão>-windows-amd64.exe` (ou `-arm64.exe`) da [página de Releases](https://github.com/GeorgeMyller/towerman/releases).
2. Dê um duplo clique. O Windows avisa que o programa não é assinado: **Mais informações › Executar assim mesmo**.
3. Ele se copia para `%LOCALAPPDATA%\Programs\Towerman`, junta essa pasta ao PATH, roda o `install`, pede permissão de administrador só para a firewall e mostra o QR de pareamento.

Pelo código, num terminal de **administrador**:

```powershell
go install github.com/GeorgeMyller/towerman/cmd/towerman@latest
towerman install --sem-extensao
```

- A tarefa agendada «Towerman» inicia o servidor no login.
- A regra de firewall abre a porta TCP 7352 só em redes privadas.
- O Windows não tem tmux. O iPhone só escreve em terminais do VS Code ou do Antigravity (pela extensão).
- A descoberta por Bonjour exige o serviço Bonjour da Apple. Sem ele, o app usa os endereços guardados no pareamento.

## iPhone e Apple Watch

Não há versão na App Store. Cada pessoa compila o app com a própria conta Apple. Siga o [assinatura-apple.md](assinatura-apple.md). Leva uns 10 minutos na primeira vez.

## Codex

Abra `/hooks` no Codex uma vez e confie nos hooks do `towerman`.

## Parear

1. No computador: `towerman pair`. Ou abra o painel em `http://127.0.0.1:7777/parear`, ou o app do Mac › **Computadores e iPhone…**.
2. Leia o QR com a câmera do iPhone e toque em **Abrir no Towerman**.
3. Repita em cada computador. O radar junta todos.

O iPhone e o computador têm de estar na mesma Wi-Fi. Para ligar um Linux ao app do Mac: rode `towerman pair --link` no Linux e cole o link no app do Mac › **Computadores e iPhone…**.

Liste os aparelhos com `towerman aparelhos`. Revogue um com `towerman aparelhos revogar <nome>`.

## Fora de casa

O acesso remoto vem desligado. Escolha uma opção em [acesso-remoto.md](acesso-remoto.md).

## Desinstalar

```sh
towerman uninstall            # remove os hooks e o serviço
towerman uninstall --purgar   # também apaga o banco, o diário e o token
```

## Problemas comuns

- **Um agente não aparece:** confira se o `towerman` está no PATH que o agente vê e reinicie o agente.
- **O iPhone não encontra o computador:** os dois têm de estar na mesma Wi-Fi. No Windows, confira a regra da firewall. Tente o endereço manual no app (`http://<ip-do-computador>:7352`).
- **O serviço:** `curl -s -H "Authorization: Bearer $(cat ~/.claude/towerman/token)" http://127.0.0.1:7777/estado`.
