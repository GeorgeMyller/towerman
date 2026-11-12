# Build the iPhone and Apple Watch apps

Apple only runs apps signed by an Apple account. Towerman is not on the App Store, so you build it with your own account. A free account works.

## 1. Prepare

1. Install Xcode 26 and run it once. Sign in at **Xcode › Settings › Accounts**.
2. `brew install xcodegen go`
3. On the iPhone: **Settings › Privacy & Security › Developer Mode** → on. Do the same on the Watch after the first install asks for it.
4. Connect the iPhone by cable the first time, and trust the Mac.

## 2. Set your IDs

Bundle IDs are unique across all Apple accounts. Pick a prefix nobody else uses, for example `io.github.<your-github-user>`.

```sh
cd towerman/apple
cp Config/Local.xcconfig.example Config/Local.xcconfig
```

Edit `Config/Local.xcconfig`:

```
TOWERMAN_TEAM_ID = ABCDE12345
TOWERMAN_ID_PREFIX = io.github.yourname
```

- `TOWERMAN_TEAM_ID`: your Team ID. Find it in Xcode › Settings › Accounts › your team, or in the Apple Developer portal. A free account has a "Personal Team" ID too.
- `TOWERMAN_ID_PREFIX`: every other ID is derived from it (app, watch app, widgets, notifications, Live Activity, App Group, Keychain group). Override a single ID in the same file if you need to. `Config/Towerman.xcconfig` lists them all.

`Config/Local.xcconfig` is in `.gitignore`. It never leaves your Mac.

## 3. Build and run

```sh
xcodegen generate
open Towerman.xcodeproj
```

1. Choose the **Towerman** scheme and your iPhone as the destination.
2. Press Run. Xcode creates the provisioning profiles. The Watch app goes inside the iPhone app.
3. On the iPhone, the first time: **Settings › General › VPN & Device Management** → trust your developer certificate.
4. If the Watch app does not appear, open the Watch app on the iPhone › Available Apps › Towerman › Install.

Then pair: see [install.md](install.md#pair).

## Free account limits

| | Free account | Paid account ($99/year) |
|---|---|---|
| Profile lifetime | 7 days | 1 year |
| Push to a locked iPhone (APNs) | No | Yes, with your own relay |
| App Groups, Keychain sharing | Yes | Yes |

With a free account, the app stops opening after 7 days. Rebuild it from Xcode, or use the weekly renewal below. Free accounts can also create only a few new App IDs per week. If Xcode refuses, wait or reuse the same prefix.

Without push, alerts still reach the Watch: the iPhone app posts a local notification while it is connected, and iOS mirrors it to the Watch when the iPhone is locked.

## Weekly renewal (free account)

`tools/renovar-assinatura.sh` rebuilds and reinstalls the app before the profile expires. It runs on the Mac, every 6 hours, through launchd. It only acts when the profile has less than 48 hours left.

1. Optional: create `~/.towerman/renovar.conf` to set paths and devices:
   ```
   TOWERMAN_REPO=/path/to/towerman
   IPHONE_ID=<UDID>          # optional; detected when empty
   WATCH_ID=<UDID>           # optional
   DEVELOPER_DIR=/Applications/Xcode.app/Contents/Developer   # optional
   LIMIAR_HORAS=48
   ```
   Find UDIDs with `xcrun devicectl list devices`.
2. Install the schedule:
   ```sh
   tools/renovar-assinatura.sh instalar
   ```
3. Check the profiles at any time:
   ```sh
   tools/renovar-assinatura.sh --check
   ```

Rules:

- The iPhone must be reachable (cable or the same Wi-Fi, unlocked at least once). When it is not, the script waits for the next run and does not touch the profiles.
- Xcode must stay signed in. When the account signs out, the script shows a notification.
- If Xcode is on an external disk, the disk must be mounted.
- Logs: `~/Library/Logs/Towerman/renew.log`.
- Remove the schedule with `tools/renovar-assinatura.sh desinstalar`.

## Push notifications (paid account)

1. In the Apple Developer portal, enable **Push Notifications** on the App ID `TOWERMAN_IOS_BUNDLE_ID`.
2. Add the capability in Xcode (it adds `aps-environment`).
3. Configure APNs on your relay: see [remote-access.md](remote-access.md#push-notifications-optional).
