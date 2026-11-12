# Compilar os apps do iPhone e do Apple Watch

A Apple só executa apps assinados por uma conta Apple. O Towerman não está na App Store, então você compila com a sua conta. Uma conta gratuita funciona.

## 1. Preparar

1. Instale o Xcode 26 e abra-o uma vez. Entre em **Xcode › Settings › Accounts**.
2. `brew install xcodegen go`
3. No iPhone: **Ajustes › Privacidade e Segurança › Modo de Desenvolvedor** → ligado. Faça o mesmo no relógio quando a primeira instalação pedir.
4. Na primeira vez, ligue o iPhone por cabo e confie no Mac.

## 2. Definir os seus IDs

Os bundle IDs são únicos entre todas as contas Apple. Escolha um prefixo que ninguém usa, por exemplo `io.github.<seu-usuario-github>`.

```sh
cd towerman/apple
cp Config/Local.xcconfig.example Config/Local.xcconfig
```

Edite `Config/Local.xcconfig`:

```
TOWERMAN_TEAM_ID = ABCDE12345
TOWERMAN_ID_PREFIX = io.github.seunome
```

- `TOWERMAN_TEAM_ID`: o seu Team ID. Veja em Xcode › Settings › Accounts › a sua equipe, ou no portal Apple Developer. A conta gratuita também tem o ID da «Personal Team».
- `TOWERMAN_ID_PREFIX`: todos os outros IDs derivam dele (app, app do relógio, widgets, notificações, Live Activity, App Group e grupo do Keychain). Para mudar um só ID, sobrescreva-o no mesmo arquivo. O `Config/Towerman.xcconfig` lista todos.

O `Config/Local.xcconfig` está no `.gitignore`. Ele nunca sai do seu Mac.

## 3. Compilar e executar

```sh
xcodegen generate
open Towerman.xcodeproj
```

1. Escolha o esquema **Towerman** e o seu iPhone como destino.
2. Clique em Run. O Xcode cria os perfis de provisionamento. O app do relógio vai dentro do app do iPhone.
3. Na primeira vez, no iPhone: **Ajustes › Geral › VPN e Gerenciamento de Dispositivos** → confie no seu certificado.
4. Se o app não aparecer no relógio: app Watch no iPhone › Apps Disponíveis › Towerman › Instalar.

Depois, pareie: veja [instalar.md](instalar.md#parear).

## Limites da conta gratuita

| | Conta gratuita | Conta paga (US$ 99/ano) |
|---|---|---|
| Validade do perfil | 7 dias | 1 ano |
| Push com o iPhone bloqueado (APNs) | Não | Sim, com relay próprio |
| App Groups, compartilhamento do Keychain | Sim | Sim |

Com a conta gratuita, o app para de abrir depois de 7 dias. Compile de novo pelo Xcode, ou use a renovação semanal abaixo. A conta gratuita também cria poucos App IDs novos por semana. Se o Xcode recusar, espere ou reutilize o mesmo prefixo.

Sem push, os alertas chegam ao relógio mesmo assim: o app do iPhone publica uma notificação local enquanto está conectado, e o iOS a espelha no relógio com o iPhone bloqueado.

## Renovação semanal (conta gratuita)

O `tools/renovar-assinatura.sh` compila e reinstala o app antes de o perfil expirar. Ele roda no Mac a cada 6 horas, pelo launchd. Só age quando faltam menos de 48 horas para o perfil expirar.

1. Opcional: crie `~/.towerman/renovar.conf` para definir caminhos e aparelhos:
   ```
   TOWERMAN_REPO=/caminho/para/towerman
   IPHONE_ID=<UDID>          # opcional; detectado quando vazio
   WATCH_ID=<UDID>           # opcional
   DEVELOPER_DIR=/Applications/Xcode.app/Contents/Developer   # opcional
   LIMIAR_HORAS=48
   ```
   Veja os UDIDs com `xcrun devicectl list devices`.
2. Instale o agendamento:
   ```sh
   tools/renovar-assinatura.sh instalar
   ```
3. Confira os perfis quando quiser:
   ```sh
   tools/renovar-assinatura.sh --check
   ```

Regras:

- O iPhone tem de estar ao alcance (cabo ou mesma Wi-Fi, desbloqueado pelo menos uma vez). Quando não está, o script espera a próxima execução e não mexe nos perfis.
- O Xcode tem de continuar com a conta conectada. Quando a conta sai, o script mostra uma notificação.
- Se o Xcode estiver num disco externo, o disco tem de estar montado.
- Log: `~/Library/Logs/Towerman/renew.log`.
- Remova o agendamento com `tools/renovar-assinatura.sh desinstalar`.

## Notificações push (conta paga)

1. No portal Apple Developer, ative **Push Notifications** no App ID `TOWERMAN_IOS_BUNDLE_ID`.
2. Adicione a capacidade no Xcode (ela acrescenta o `aps-environment`).
3. Configure o APNs no seu relay: veja [acesso-remoto.md](acesso-remoto.md#notificações-push-opcional).
