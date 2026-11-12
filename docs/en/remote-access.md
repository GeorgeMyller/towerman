# Remote access (away from home)

At home, the iPhone talks to each computer over Wi-Fi. Away from home, it needs a path to reach the computer. **Towerman ships with no public server.** Remote access is off until you choose one of the options below.

| Option | You need | Cost | Push to a locked iPhone |
|---|---|---|---|
| [Tailscale](#option-1-tailscale-recommended) (recommended) | A free Tailscale account | Free | No |
| [Your own relay](#option-2-your-own-relay) | A server with HTTPS (Render, a VPS, Docker) | Free to a few $/month | Only with a paid Apple Developer account |
| [Cloudflare quick tunnel](#option-3-cloudflare-quick-tunnel) | Nothing | Free | No |

Pair the iPhone at home first, on the same Wi-Fi, for every option. Pairing never happens over the internet.

## Option 1: Tailscale (recommended)

Tailscale builds a private network between your devices. No server and no open ports.

1. Install Tailscale on each computer and on the iPhone. Sign in with the same account.
2. Run `towerman install` again on each computer (or restart the Mac app).
3. Pair the iPhone at home (`towerman pair`).

The computer publishes its Tailscale IP and its MagicDNS name (`*.ts.net`) during pairing. Away from home, the app tries these addresses. Keep Tailscale connected on the iPhone.

## Option 2: Your own relay

The relay (`cmd/towerman-relay`) forwards end-to-end encrypted envelopes between the computer and the iPhone. It cannot read them. It only makes outbound connections from your computers, so you do not open ports at home.

**Always set `TOWERMAN_RELAY_TOKENS`.** Without it, anyone who finds the URL can use your relay.

1. Create a token:
   ```sh
   openssl rand -hex 32
   ```
2. Deploy the relay with HTTPS. iOS refuses plain HTTP to a public host.
   - **Render:** fork the repository and create a Blueprint from `render.yaml`. Set `TOWERMAN_RELAY_TOKENS` in the dashboard. The free plan works. It sleeps when idle and wakes when a computer connects.
   - **Docker on a VPS:**
     ```sh
     docker build -f Dockerfile.relay -t towerman-relay .
     docker run -d --restart unless-stopped -p 8080:8080 \
       -e TOWERMAN_RELAY_TOKENS=<token> towerman-relay
     ```
     Put a reverse proxy with TLS in front of it (Caddy, nginx, Cloudflare). Or pass `-tls-cert` and `-tls-key` to the relay.
3. Check it: `curl https://<your-relay>/health` must answer `{"ok":true,...}`.
4. Point each computer to the relay. Create or edit `config.json` in the Towerman folder (`~/.claude/towerman/config.json` by default):
   ```json
   {
     "relay_url": "https://<your-relay>",
     "relay_token": "<token>"
   }
   ```
   The service rereads the token on each connection attempt. `TOWERMAN_RELAY_URL` and `TOWERMAN_RELAY_TOKEN` override these keys, but only for a `towerman serve` you start by hand: `install` does not copy them into the service.
5. Restart the service: `towerman install` (Linux, Windows) or quit and reopen the Mac app.
6. Pair the iPhone again at home. The relay keys are created during pairing.

The relay accepts several tokens, separated by commas. Give each computer its own token if you want to revoke one later.

### Push notifications (optional)

Without push, the iPhone app shows alerts while it is connected (in the foreground, or in the background while an agent runs). To wake a locked iPhone with a new request, the relay sends an APNs push. This needs a **paid** Apple Developer account:

1. Add the Push Notifications capability to your iOS App ID.
2. Create an APNs key (`.p8`) in the Apple Developer portal.
3. Set on the relay: `APNS_KEY_ID`, `APNS_TEAM_ID`, `APNS_KEY_P8` (the PEM text or a file path) and `APNS_TOPIC` (your iOS bundle ID, `TOWERMAN_IOS_BUNDLE_ID`). Set `APNS_SANDBOX=1` for Debug builds.

The push only says "A session is waiting for a decision". The question text travels encrypted.

## Option 3: Cloudflare quick tunnel

Run `TOWERMAN_TUNEL=cloudflare towerman install` (`install` copies this variable into the service). Towerman downloads `cloudflared` and opens a `trycloudflare.com` tunnel. The address changes when the tunnel restarts, so the app only learns the new one after a connection at home. Use it for tests, not as your daily path.

## Turn remote access off

Remove `relay_url` from `config.json`. To block every remote path, run `TOWERMAN_TUNEL=nao towerman install`.

## Security notes

- Pairing creates an X25519 key pair per device. Every relay message is sealed with AES-256-GCM.
- The relay only sees room IDs, sizes and timing.
- Revoke a device with `towerman aparelhos revogar <name>`.
