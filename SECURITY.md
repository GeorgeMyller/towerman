# Security

## Report a vulnerability

Do not open a public issue. Use GitHub's private reporting: **Security › Report a vulnerability** on this repository.

Include the version (`towerman version`), the platform and the steps to reproduce.

## Design summary

- The server listens on `127.0.0.1:7777` (panel, local token) and on `:7352` (devices, one token per paired device).
- Pairing only works on the local network. It creates an X25519 key pair per device.
- Relay traffic is sealed with AES-256-GCM. The relay cannot read it.
- A relay without `TOWERMAN_RELAY_TOKENS` accepts any computer. Always set it on a relay reachable from the internet.
- The project ships no public relay and no default relay URL.
