# Install

Towerman has two parts:

- **The server** (`towerman`, Go). One per computer. It installs hooks into your coding agents and keeps their state.
- **The apps** for iPhone, Apple Watch and Mac. The iPhone app joins all your computers in one radar.

Install the server on every computer you use. Then build the iPhone app and pair it.

## Requirements

| Part | Needs |
|---|---|
| Server (Linux, Windows, Mac CLI) | [Go](https://go.dev/dl/) 1.26 or newer, Git |
| Mac app | macOS 14 or newer. To build it: Xcode 26, `brew install go xcodegen` |
| iPhone and Watch apps | iOS 18 and watchOS 26. A Mac with Xcode 26, an Apple account (free works), Developer Mode on the iPhone and the Watch |
| Agents | Claude Code, Codex or Antigravity (`agy` or the IDE) |

## Mac

**From the release (easier):**

1. Download `Towerman-<version>.dmg` from the [Releases page](https://github.com/GeorgeMyller/towerman/releases).
2. Open it and drag Towerman to Applications.
3. The app is not notarized. On first launch, right-click it and choose **Open**. If macOS still blocks it: **System Settings › Privacy & Security › Open Anyway**.
4. The app installs the hooks, registers the server with launchd and shows up in the menu bar.
5. In the (…) menu, turn on **Abrir ao iniciar sessão** (open at login) if you want it always on.

The apps are in Portuguese. This guide quotes their labels as they appear.

**From source:**

```sh
git clone https://github.com/GeorgeMyller/towerman && cd towerman/apple
cp Config/Local.xcconfig.example Config/Local.xcconfig   # then edit it, see apple-signing.md
xcodegen generate
tools/dmg.sh        # creates apple/dist/Towerman-<version>.dmg
```

## Linux

```sh
go install github.com/GeorgeMyller/towerman/cmd/towerman@latest
towerman install --sem-extensao     # hooks, systemd user service, menu shortcut
```

- `go install` puts the binary in `~/go/bin`. Add it to your PATH: `echo 'export PATH="$HOME/go/bin:$PATH"' >> ~/.bashrc`.
- Or download `towerman-v<version>-linux-<arch>` from the Releases page, check it against `SHA256SUMS`, and put it in your PATH.
- `install` also installs **tmux** if it is missing (it asks for `sudo`). Without a terminal, it prints the command to run.
- `install` turns on systemd "linger", so the service keeps running after logout. If it asks for permission: `sudo loginctl enable-linger $USER`.
- Drop `--sem-extensao` to also install the VS Code / Antigravity extension. It is built locally; it does not come from the Marketplace.

Run your agents inside tmux (`tmux new -s work`). Then the iPhone can type into a session that is waiting.

## Windows

1. Download `towerman-v<version>-windows-amd64.exe` (or `-arm64.exe`) from the [Releases page](https://github.com/GeorgeMyller/towerman/releases).
2. Double-click it. Windows warns that the program is not signed: **More info › Run anyway**.
3. It copies itself to `%LOCALAPPDATA%\Programs\Towerman`, adds that folder to PATH, runs `install`, asks for admin rights only for the firewall rule, and shows the pairing QR code.

From source, in an **administrator** terminal:

```powershell
go install github.com/GeorgeMyller/towerman/cmd/towerman@latest
towerman install --sem-extensao
```

- The scheduled task "Towerman" starts the server at login.
- The firewall rule opens TCP 7352 on private networks only.
- Windows has no tmux. The iPhone can only type into VS Code or Antigravity terminals (through the extension).
- Bonjour discovery needs Apple's Bonjour service. Without it, the app uses the addresses saved during pairing.

## iPhone and Apple Watch

There is no App Store build. Each user builds the app with their own Apple account. Follow [apple-signing.md](apple-signing.md). It takes about 10 minutes the first time.

## Codex

Open `/hooks` in Codex once and trust the `towerman` hooks.

## Pair

1. On the computer: `towerman pair`. Or open the panel at `http://127.0.0.1:7777/parear`, or the Mac app › **Computadores e iPhone…**.
2. Scan the QR code with the iPhone camera and tap **Abrir no Towerman**.
3. Repeat on each computer. The radar joins them.

The iPhone and the computer must be on the same Wi-Fi. To link a Linux computer to the Mac app: run `towerman pair --link` on Linux and paste the link in the Mac app › **Computadores e iPhone…**.

List devices with `towerman aparelhos`. Revoke one with `towerman aparelhos revogar <name>`.

## Away from home

Remote access is off by default. Choose an option in [remote-access.md](remote-access.md).

## Uninstall

```sh
towerman uninstall            # removes the hooks and the service
towerman uninstall --purgar   # also deletes the database, journal and token
```

## Troubleshooting

- **An agent does not show up:** check that `towerman` is in the PATH the agent sees, then restart the agent.
- **The iPhone cannot find the computer:** both must be on the same Wi-Fi. On Windows, check the firewall rule. Try the manual address in the app (`http://<computer-ip>:7352`).
- **The service:** `curl -s -H "Authorization: Bearer $(cat ~/.claude/towerman/token)" http://127.0.0.1:7777/estado`.
