# Acesso fora de casa

Em casa, o iPhone fala com cada computador pela Wi-Fi. Fora de casa, ele precisa de um caminho até o computador. **O Towerman não traz nenhum servidor público.** O acesso remoto fica desligado até você escolher uma das opções abaixo.

| Opção | Você precisa de | Custo | Push com o iPhone bloqueado |
|---|---|---|---|
| [Tailscale](#opção-1-tailscale-recomendada) (recomendada) | Uma conta gratuita do Tailscale | Grátis | Não |
| [Relay próprio](#opção-2-relay-próprio) | Um servidor com HTTPS (Render, VPS, Docker) | Grátis a poucos dólares por mês | Só com conta Apple Developer paga |
| [Túnel rápido da Cloudflare](#opção-3-túnel-rápido-da-cloudflare) | Nada | Grátis | Não |

Em todas as opções, pareie o iPhone em casa primeiro, na mesma Wi-Fi. O pareamento nunca passa pela internet.

## Opção 1: Tailscale (recomendada)

O Tailscale cria uma rede privada entre os seus aparelhos. Não precisa de servidor nem de portas abertas.

1. Instale o Tailscale em cada computador e no iPhone. Entre com a mesma conta.
2. Rode `towerman install` de novo em cada computador (ou reabra o app do Mac).
3. Pareie o iPhone em casa (`towerman pair`).

No pareamento, o computador publica o IP do Tailscale e o nome MagicDNS (`*.ts.net`). Fora de casa, o app tenta esses endereços. Mantenha o Tailscale ligado no iPhone.

## Opção 2: Relay próprio

O relay (`cmd/towerman-relay`) repassa envelopes cifrados de ponta a ponta entre o computador e o iPhone. Ele não consegue lê-los. Os computadores só fazem conexões de saída, então você não abre portas em casa.

**Defina sempre `TOWERMAN_RELAY_TOKENS`.** Sem essa variável, qualquer pessoa que descobrir a URL usa o seu relay.

1. Gere um token:
   ```sh
   openssl rand -hex 32
   ```
2. Publique o relay com HTTPS. O iOS recusa HTTP simples para um endereço público.
   - **Render:** faça um fork do repositório e crie um Blueprint a partir do `render.yaml`. Defina `TOWERMAN_RELAY_TOKENS` no painel. O plano gratuito funciona. Ele dorme sem uso e acorda quando um computador se conecta.
   - **Docker numa VPS:**
     ```sh
     docker build -f Dockerfile.relay -t towerman-relay .
     docker run -d --restart unless-stopped -p 8080:8080 \
       -e TOWERMAN_RELAY_TOKENS=<token> towerman-relay
     ```
     Coloque um proxy reverso com TLS na frente (Caddy, nginx, Cloudflare). Ou passe `-tls-cert` e `-tls-key` ao relay.
3. Confira: `curl https://<seu-relay>/health` deve responder `{"ok":true,...}`.
4. Aponte cada computador para o relay. Crie ou edite o `config.json` da pasta do Towerman (`~/.claude/towerman/config.json` por padrão):
   ```json
   {
     "relay_url": "https://<seu-relay>",
     "relay_token": "<token>"
   }
   ```
   O serviço relê o token a cada tentativa de conexão. `TOWERMAN_RELAY_URL` e `TOWERMAN_RELAY_TOKEN` sobrepõem essas chaves, mas só num `towerman serve` iniciado à mão: o `install` não as copia para o serviço.
5. Reinicie o serviço: `towerman install` (Linux, Windows) ou feche e reabra o app do Mac.
6. Pareie o iPhone de novo, em casa. As chaves do relay nascem no pareamento.

O relay aceita vários tokens separados por vírgula. Dê um token a cada computador se quiser revogar um depois.

### Notificações push (opcional)

Sem push, o app do iPhone mostra os alertas enquanto está conectado (em primeiro plano, ou em segundo plano com um agente ativo). Para acordar um iPhone bloqueado com um pedido novo, o relay manda um push APNs. Isso exige uma conta Apple Developer **paga**:

1. Ative a capacidade Push Notifications no App ID do iOS.
2. Crie uma chave APNs (`.p8`) no portal Apple Developer.
3. Defina no relay: `APNS_KEY_ID`, `APNS_TEAM_ID`, `APNS_KEY_P8` (o texto PEM ou o caminho do arquivo) e `APNS_TOPIC` (o bundle ID do iOS, `TOWERMAN_IOS_BUNDLE_ID`). Use `APNS_SANDBOX=1` em builds Debug.

O push só diz «Uma sessão espera uma decisão». O texto da pergunta viaja cifrado.

## Opção 3: Túnel rápido da Cloudflare

Rode `TOWERMAN_TUNEL=cloudflare towerman install` (o `install` copia essa variável para o serviço). O Towerman baixa o `cloudflared` e abre um túnel `trycloudflare.com`. O endereço muda quando o túnel reinicia, e o app só aprende o novo depois de uma conexão em casa. Use para testes, não como caminho diário.

## Desligar o acesso remoto

Remova `relay_url` do `config.json`. Para bloquear qualquer caminho remoto, rode `TOWERMAN_TUNEL=nao towerman install`.

## Segurança

- O pareamento cria um par de chaves X25519 por aparelho. Cada mensagem do relay é selada com AES-256-GCM.
- O relay só vê IDs de sala, tamanhos e horários.
- Revogue um aparelho com `towerman aparelhos revogar <nome>`.
