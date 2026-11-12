#!/bin/bash
# Renova a assinatura do Towerman no iPhone e no relógio (conta Apple gratuita).
#
# Com uma conta Apple gratuita, os perfis de provisionamento valem 7 dias. Este script verifica
# a validade dos perfis do app iOS e, quando faltam menos de LIMIAR_HORAS, compila de novo com
# -allowProvisioningUpdates (o Xcode pede perfis novos à Apple) e reinstala o app no iPhone.
# O app do relógio vai embutido no do iPhone e o iPhone instala-o no relógio.
#
# Uso:
#   renovar-assinatura.sh              verifica e renova se for preciso (é o que o agendamento corre)
#   renovar-assinatura.sh --check      mostra a configuração e a validade dos perfis; não mexe em nada
#   renovar-assinatura.sh --simular    mostra o que faria, sem copiar fontes, compilar nem mover perfis
#   renovar-assinatura.sh --force      renova mesmo que os perfis ainda sejam válidos
#   renovar-assinatura.sh instalar     copia o script para ~/.towerman e agenda-o no launchd (6 em 6 h)
#   renovar-assinatura.sh desinstalar  remove o agendamento (mantém ~/.towerman)
#
# Configuração opcional em ~/.towerman/renovar.conf, uma linha CHAVE=valor por definição.
# O ficheiro é lido como texto (não é executado). Linhas vazias e começadas por # são ignoradas.
#   TOWERMAN_REPO=/caminho/do/checkout     padrão: a pasta acima de tools/, se tiver apple/
#   IPHONE_ID=<UDID>                       padrão: o primeiro iPhone emparelhado e disponível
#   WATCH_ID=<UDID>                        padrão: o primeiro relógio emparelhado
#   DEVELOPER_DIR=/Applications/Xcode.app/Contents/Developer   padrão: xcode-select -p
#   LIMIAR_HORAS=48                        renova quando falta menos do que isto
#   TOWERMAN_IOS_BUNDLE_ID=<bundle ID>     padrão: apple/Config/*.xcconfig ou o projeto Xcode
# Uma variável de ambiente com o mesmo nome sobrepõe-se ao ficheiro.
#
# Porquê uma cópia em ~/.towerman: o launchd não executa ficheiros em volumes externos
# («Operation not permitted»). O script e as fontes de apple/ (incluindo Config/Local.xcconfig)
# são copiados para ~/.towerman a cada execução e a compilação corre a partir dessa cópia.
#
# Ordem segura: só mexe nos perfis depois de confirmar o Xcode, a conta Apple e o iPhone.
# Se algum deles não estiver disponível, regista, não toca em nada e tenta no próximo intervalo.
#
# Códigos de saída:
#   0  renovado e instalado, nada a fazer, ou adiado (Xcode, fontes ou iPhone fora de alcance)
#   1  falhou (conta Apple, compilação, perfis não renovados ou configuração)
#   2  renovado, instalação pendente (o perfil novo existe, mas o app não chegou ao iPhone)
#
# Corre com o /bin/bash do macOS (3.2): sem arrays associativos nem outras extensões recentes.

set -uo pipefail

TOWERMAN_HOME="$HOME/.towerman"
CONF="$TOWERMAN_HOME/renovar.conf"
COPIA_APPLE="$TOWERMAN_HOME/apple"
SCRIPT_INSTALADO="$TOWERMAN_HOME/renovar-assinatura.sh"
BUILD_DIR="$TOWERMAN_HOME/build"
TRINCO="$TOWERMAN_HOME/.renovar.lock"
CACHE_BUNDLE="$TOWERMAN_HOME/.bundle-id"
PENDENTE="$TOWERMAN_HOME/.instalacao-pendente"
ROTULO="io.github.georgemyller.towerman.renew"
ROTULO_ANTIGO="legado.towerman.renew"
AGENTES="$HOME/Library/LaunchAgents"
PLIST="$AGENTES/$ROTULO.plist"
LOG_DIR="$HOME/Library/Logs/Towerman"
LOG_FILE="$LOG_DIR/renew.log"
BUILD_LOG="$LOG_DIR/renew-xcodebuild.log"
PERFIS_DIR="$HOME/Library/Developer/Xcode/UserData/Provisioning Profiles"
LIMIAR_AVISO_HORAS=24
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
export PATH="/usr/bin:/bin:/usr/sbin:/sbin:/usr/local/bin:/opt/homebrew/bin:$PATH"

modo="renovar"; forcar=0
LOG_ATIVO=0
TMPD=""

# ---------- registo e notificação ----------

log() {
    local m
    m="[$(date '+%Y-%m-%d %H:%M:%S')] $*"
    printf '%s\n' "$m"
    if [ "$LOG_ATIVO" = 1 ]; then printf '%s\n' "$m" >> "$LOG_FILE"; fi
}
aviso() { log "AVISO: $*"; }
erro() { log "ERRO: $*"; }

notificar() {
    if [ "$modo" = simular ]; then
        log "[simular] notificaria: $1 — $2"
        return 0
    fi
    osascript -e 'on run argv' \
        -e 'display notification (item 2 of argv) with title (item 1 of argv)' \
        -e 'end run' "$1" "$2" >/dev/null 2>&1 || true
}

rodar_log() {
    local tam
    tam=$(stat -f %z "$LOG_FILE" 2>/dev/null || echo 0)
    if [ "$tam" -gt 1048576 ]; then mv -f "$LOG_FILE" "$LOG_FILE.1"; fi
}

limpar() {
    [ -z "$TMPD" ] || rm -rf "$TMPD"
    if [ -f "$TRINCO/pid" ] && [ "$(cat "$TRINCO/pid" 2>/dev/null)" = "$$" ]; then rm -rf "$TRINCO"; fi
}
trap limpar EXIT

TMPD=$(mktemp -d "${TMPDIR:-/tmp}/towerman-renovar.XXXXXX") || exit 1

# ---------- configuração ----------

expandir_til() {
    case "$1" in
        "~") printf '%s' "$HOME" ;;
        "~/"*) printf '%s/%s' "$HOME" "${1#\~/}" ;;
        *) printf '%s' "$1" ;;
    esac
}

ler_conf() {
    local linha chave valor n=0
    [ -f "$CONF" ] || return 0
    while IFS= read -r linha || [ -n "$linha" ]; do
        n=$((n + 1))
        linha="${linha%$'\r'}"
        case "$(printf '%s' "$linha" | tr -d '[:space:]')" in ''|'#'*) continue ;; esac
        if [ "${linha#*=}" = "$linha" ]; then
            aviso "renovar.conf:$n sem «=», ignorada"
            continue
        fi
        chave=$(printf '%s' "${linha%%=*}" | tr -d '[:space:]')
        valor=$(printf '%s' "${linha#*=}" | sed -E 's/^[[:space:]]+//; s/[[:space:]]+$//; s/^"(.*)"$/\1/; s/^'"'"'(.*)'"'"'$/\1/')
        case "$chave" in
            TOWERMAN_REPO|IPHONE_ID|WATCH_ID|DEVELOPER_DIR|LIMIAR_HORAS|TOWERMAN_IOS_BUNDLE_ID)
                printf -v "CONF_$chave" '%s' "$(expandir_til "$valor")" ;;
            *) aviso "renovar.conf:$n chave desconhecida «${chave}», ignorada" ;;
        esac
    done < "$CONF"
}

# Valor final: variável de ambiente > renovar.conf > padrão.
definir() { # $1 nome, $2 padrão
    local amb conf
    eval "amb=\${$1:-}"
    eval "conf=\${CONF_$1:-}"
    if [ -n "$amb" ]; then printf -v "$1" '%s' "$amb"
    elif [ -n "$conf" ]; then printf -v "$1" '%s' "$conf"
    else printf -v "$1" '%s' "$2"; fi
}

carregar_config() {
    ler_conf
    local repo_padrao=""
    if [ -d "$SCRIPT_DIR/../apple" ] && [ -f "$SCRIPT_DIR/renovar-assinatura.sh" ]; then
        repo_padrao="$(cd "$SCRIPT_DIR/.." && pwd)"
    fi
    definir TOWERMAN_REPO "$repo_padrao"
    definir IPHONE_ID ""
    definir WATCH_ID ""
    definir DEVELOPER_DIR ""
    definir LIMIAR_HORAS 48
    definir TOWERMAN_IOS_BUNDLE_ID ""
    TOWERMAN_REPO="${TOWERMAN_REPO%/}"
    case "$LIMIAR_HORAS" in ''|*[!0-9]*)
        aviso "LIMIAR_HORAS=«${LIMIAR_HORAS}» não é um número inteiro; uso 48"
        LIMIAR_HORAS=48 ;;
    esac
}

# Diz se um caminho fica num volume externo que não está montado (e qual).
volume_ausente() {
    case "$1" in
        /Volumes/*)
            local vol
            vol="/Volumes/$(printf '%s' "${1#/Volumes/}" | cut -d/ -f1)"
            if [ ! -d "$vol" ]; then printf '%s' "$vol"; return 0; fi ;;
    esac
    return 1
}

# ---------- Xcode e conta Apple ----------

XCODE_MOTIVO=""
# 0 pronto; 1 erro de configuração (permanente); 2 indisponível agora (volume desmontado).
verificar_xcode() {
    local dd vol
    dd="$DEVELOPER_DIR"
    [ -n "$dd" ] || dd=$(xcode-select -p 2>/dev/null || true)
    if [ -z "$dd" ]; then
        XCODE_MOTIVO="nenhum Xcode selecionado: instale o Xcode e corra «sudo xcode-select -s /Applications/Xcode.app» ou defina DEVELOPER_DIR em $CONF"
        return 1
    fi
    if [ ! -d "$dd" ]; then
        if vol=$(volume_ausente "$dd"); then
            XCODE_MOTIVO="o Xcode está no volume $vol, que não está montado ($dd); ligue o disco"
            return 2
        fi
        XCODE_MOTIVO="o DEVELOPER_DIR não existe: $dd"
        return 1
    fi
    case "$dd" in *CommandLineTools*)
        XCODE_MOTIVO="$dd são só as Command Line Tools; a renovação precisa do Xcode completo"
        return 1 ;;
    esac
    if [ ! -x "$dd/usr/bin/xcodebuild" ]; then
        XCODE_MOTIVO="não encontrei xcodebuild em $dd/usr/bin"
        return 1
    fi
    export DEVELOPER_DIR="$dd"
    export PATH="$dd/usr/bin:$PATH"
    return 0
}

# Sinal prévio de «No Accounts»: o Xcode não tem nenhuma conta Apple registada.
# A compilação confirma depois (procura «No Accounts» no registo do xcodebuild).
conta_presente() {
    defaults read com.apple.dt.Xcode DVTDeveloperAccountManagerAppleIDLists 2>/dev/null | grep -q identifier
}

MSG_SEM_CONTA="Nenhuma conta Apple no Xcode (No Accounts). Abra Xcode > Settings > Accounts e inicie sessão."

# ---------- fontes ----------

FONTE_APPLE=""
# Copia o script e apple/ do checkout para ~/.towerman. Define FONTE_APPLE (de onde compilar).
# 0 pronto; 1 sem fontes; 2 fontes num volume desmontado e sem cópia local.
sincronizar() {
    local repo_apple="$TOWERMAN_REPO/apple" vol tmp
    if [ -z "$TOWERMAN_REPO" ]; then
        aviso "TOWERMAN_REPO não definido em $CONF; uso a cópia em $COPIA_APPLE sem a atualizar"
    elif [ ! -d "$repo_apple" ]; then
        if vol=$(volume_ausente "$repo_apple"); then
            aviso "o checkout está no volume $vol, que não está montado; uso a cópia em $COPIA_APPLE"
        else
            aviso "não consigo ler $repo_apple; uso a cópia em $COPIA_APPLE"
        fi
    elif [ "$modo" = simular ] || [ "$modo" = check ]; then
        [ "$modo" = check ] || log "[simular] copiaria $repo_apple/ para $COPIA_APPLE/ (com Config/Local.xcconfig)"
        FONTE_APPLE="$repo_apple"
        return 0
    else
        mkdir -p "$COPIA_APPLE"
        if rsync -a --delete --exclude build --exclude .build --exclude DerivedData --exclude xcuserdata \
            "$repo_apple/" "$COPIA_APPLE/" 2>>"$LOG_FILE"; then
            [ -f "$repo_apple/Config/Local.xcconfig" ] || \
                log "Sem apple/Config/Local.xcconfig: compila com os IDs de apple/Config/Towerman.xcconfig."
        else
            aviso "a cópia de $repo_apple falhou (permissões do launchd?); uso a cópia anterior"
        fi
        # Atualiza o script instalado (escrita atómica: o bash lê o script enquanto corre).
        if [ -f "$SCRIPT_INSTALADO" ] && [ -r "$TOWERMAN_REPO/tools/renovar-assinatura.sh" ] \
            && ! cmp -s "$TOWERMAN_REPO/tools/renovar-assinatura.sh" "$SCRIPT_INSTALADO"; then
            tmp="$SCRIPT_INSTALADO.novo.$$"
            if cp "$TOWERMAN_REPO/tools/renovar-assinatura.sh" "$tmp" && chmod 755 "$tmp" && mv -f "$tmp" "$SCRIPT_INSTALADO"; then
                log "Script instalado atualizado a partir de $TOWERMAN_REPO/tools/."
            else
                rm -f "$tmp"
            fi
        fi
    fi
    if [ -d "$COPIA_APPLE/Towerman.xcodeproj" ]; then
        FONTE_APPLE="$COPIA_APPLE"
        return 0
    fi
    if [ -n "$TOWERMAN_REPO" ] && volume_ausente "$repo_apple" >/dev/null; then return 2; fi
    return 1
}

# ---------- bundle ID ----------

valor_xcconfig() { # $1 chave, $2... ficheiros (o último sobrepõe-se)
    local chave="$1" f v r=""
    shift
    for f in "$@"; do
        [ -r "$f" ] || continue
        v=$(sed -n -E "s#^[[:space:]]*$chave[[:space:]]*=[[:space:]]*([^/[:space:]]+)[[:space:]]*(//.*)?\$#\\1#p" "$f" | tail -1)
        [ -n "$v" ] && r="$v"
    done
    case "$r" in *'$('*|*'${'*) r="" ;; esac
    printf '%s' "$r"
}

bundle_do_projeto() { # $1 pasta apple, $2 esquema, $3 alvo
    xcodebuild -project "$1/Towerman.xcodeproj" -scheme "$2" -configuration Debug -showBuildSettings 2>/dev/null \
        | awk -v alvo="$3" '/^Build settings for action .* and target /{t=$NF; sub(/:$/, "", t)}
            t == alvo && $1 == "PRODUCT_BUNDLE_IDENTIFIER" {print $3; exit}'
}

BUNDLE=""
# Ordem: TOWERMAN_IOS_BUNDLE_ID (ambiente/conf) > apple/Config/*.xcconfig > projeto Xcode > cache.
resolver_bundle() {
    local apple="$FONTE_APPLE" origem
    if [ -n "$TOWERMAN_IOS_BUNDLE_ID" ]; then
        BUNDLE="$TOWERMAN_IOS_BUNDLE_ID"; origem="configuração"
    elif [ -n "$apple" ] && BUNDLE=$(valor_xcconfig TOWERMAN_IOS_BUNDLE_ID \
            "$apple/Config/Towerman.xcconfig" "$apple/Config/Local.xcconfig") && [ -n "$BUNDLE" ]; then
        origem="apple/Config"
    elif [ -n "$apple" ] && [ "$XCODE_OK" = 0 ] && BUNDLE=$(bundle_do_projeto "$apple" Towerman Towerman) && [ -n "$BUNDLE" ]; then
        origem="projeto Xcode"
    elif [ -s "$CACHE_BUNDLE" ]; then
        BUNDLE=$(cat "$CACHE_BUNDLE"); origem="cache de $CACHE_BUNDLE"
    else
        BUNDLE=""
        return 1
    fi
    if [ "$modo" != simular ] && [ "$modo" != check ] && [ "$origem" != "cache de $CACHE_BUNDLE" ]; then
        printf '%s\n' "$BUNDLE" > "$CACHE_BUNDLE" 2>/dev/null || true
    fi
    log "Bundle ID do app iOS: $BUNDLE ($origem)"
}

# ---------- perfis ----------

# Lista os perfis iOS do app e das extensões: caminho|app-id|epoch de expiração|horas|UDIDs
listar_perfis() {
    local p dec appid plat exp epoch agora devs
    agora=$(date +%s)
    dec="$TMPD/perfil.plist"
    for p in "$PERFIS_DIR"/*.mobileprovision "$HOME/Library/MobileDevice/Provisioning Profiles"/*.mobileprovision; do
        [ -f "$p" ] || continue
        security cms -D -i "$p" > "$dec" 2>/dev/null || continue
        appid=$(plutil -extract Entitlements.application-identifier raw -o - "$dec" 2>/dev/null) || continue
        case "${appid#*.}" in "$BUNDLE"|"$BUNDLE".*) ;; *) continue ;; esac
        plat=$(plutil -extract Platform json -o - "$dec" 2>/dev/null || true)
        case "$plat" in *iOS*|*watchOS*) ;; *) continue ;; esac
        exp=$(plutil -extract ExpirationDate raw -o - "$dec" 2>/dev/null) || continue
        epoch=$(date -j -u -f '%Y-%m-%dT%H:%M:%SZ' "$exp" +%s 2>/dev/null) || continue
        devs=$(plutil -extract ProvisionedDevices json -o - "$dec" 2>/dev/null | tr -d '[]" \n' || true)
        printf '%s|%s|%s|%s|%s\n' "$p" "$appid" "$epoch" "$(( (epoch - agora) / 3600 ))" "$devs"
    done
}

MIN_HORAS=""   # vazio = nenhum perfil
avaliar_perfis() {
    local p appid epoch horas devs
    PERFIS=$(listar_perfis)
    MIN_HORAS=""
    while IFS='|' read -r p appid epoch horas devs; do
        [ -n "$p" ] || continue
        log "Perfil $appid: $horas h restantes (expira em $(date -r "$epoch" '+%Y-%m-%d %H:%M'))"
        if [ -z "$MIN_HORAS" ] || [ "$horas" -lt "$MIN_HORAS" ]; then MIN_HORAS="$horas"; fi
    done <<< "$PERFIS"
}

perfil_tem_aparelho() { # $1 sufixo do app-id (ex.: .watchkitapp), $2 UDID
    local p appid epoch horas devs
    while IFS='|' read -r p appid epoch horas devs; do
        [ "${appid#*.}" = "$BUNDLE$1" ] || continue
        case ",$devs," in *",$2,"*) return 0 ;; esac
    done <<< "$PERFIS"
    return 1
}

# ---------- aparelhos ----------

APARELHOS=""      # tipo|UDID|emparelhamento (só físicos)
APARELHOS_TXT=""
detetar_aparelhos() {
    local json
    json="$TMPD/aparelhos.json"
    xcrun devicectl list devices --json-output "$json" >/dev/null 2>&1 || return 1
    APARELHOS_TXT=$(xcrun devicectl list devices 2>/dev/null || true)
    APARELHOS=$(osascript -l JavaScript - "$json" 2>/dev/null <<'JXA'
function run(argv) {
  ObjC.import('Foundation');
  var s = $.NSString.stringWithContentsOfFileEncodingError(argv[0], $.NSUTF8StringEncoding, null);
  if (s.isNil()) return '';
  var d = JSON.parse(s.js), out = [];
  ((d.result || {}).devices || []).forEach(function (x) {
    var h = x.hardwareProperties || {}, c = x.connectionProperties || {};
    if (h.reality !== 'physical' || !h.udid) return;
    out.push([h.deviceType || '', h.udid, c.pairingState || ''].join('|'));
  });
  return out.join('\n');
}
JXA
)
    return 0
}

disponivel() { # o devicectl mostra o aparelho como «connected» ou «available (paired)»
    printf '%s\n' "$APARELHOS_TXT" | grep -F "$1" | grep -Eq '(^|[[:space:]])(connected|available)'
}

escolher() { # $1 tipo (iPhone|appleWatch): imprime o UDID do primeiro emparelhado, disponível primeiro
    local tipo udid par primeiro=""
    while IFS='|' read -r tipo udid par; do
        [ "$tipo" = "$1" ] && [ "$par" = paired ] || continue
        if disponivel "$udid"; then printf '%s' "$udid"; return 0; fi
        [ -n "$primeiro" ] || primeiro="$udid"
    done <<< "$APARELHOS"
    printf '%s' "$primeiro"
}

IPHONE=""; RELOGIO=""; IPHONE_OK=1; RELOGIO_OK=1
resolver_aparelhos() {
    if ! detetar_aparelhos; then
        IPHONE="$IPHONE_ID"; RELOGIO="$WATCH_ID"
        aviso "o devicectl não listou os aparelhos"
        return 0
    fi
    IPHONE="$IPHONE_ID"; [ -n "$IPHONE" ] || IPHONE=$(escolher iPhone)
    RELOGIO="$WATCH_ID"; [ -n "$RELOGIO" ] || RELOGIO=$(escolher appleWatch)
    if [ -n "$IPHONE" ] && disponivel "$IPHONE"; then IPHONE_OK=0; fi
    if [ -n "$RELOGIO" ] && disponivel "$RELOGIO"; then RELOGIO_OK=0; fi
    log "iPhone: ${IPHONE:-nenhum emparelhado} ($([ "$IPHONE_OK" = 0 ] && echo disponível || echo indisponível))"
    log "Relógio: ${RELOGIO:-nenhum emparelhado} ($([ "$RELOGIO_OK" = 0 ] && echo disponível || echo indisponível))"
}

# ---------- desfechos ----------

horas_txt() { if [ -z "$MIN_HORAS" ]; then echo "sem perfil válido"; else echo "faltam $MIN_HORAS h"; fi; }

# Não toca nos perfis: regista e tenta no próximo intervalo. Notifica só perto do fim.
adiar() {
    log "Adiado: $1. Os perfis não foram tocados; nova tentativa no próximo intervalo ($(horas_txt))."
    if [ -z "$MIN_HORAS" ] || [ "$MIN_HORAS" -lt "$LIMIAR_AVISO_HORAS" ]; then
        notificar "Towerman: assinatura a expirar" "$(horas_txt). Não renovei: $1."
    fi
    exit 0
}

falhar() { # $1 mensagem para o registo, $2 texto da notificação (opcional)
    erro "$1"
    log "Estado final: falhou."
    notificar "Towerman: renovação falhou" "${2:-$1}"
    exit 1
}

# ---------- instalar / desinstalar ----------

agente_carregado() { launchctl print "gui/$(id -u)/$1" >/dev/null 2>&1; }

desligar_antigo() {
    local antigo="$AGENTES/$ROTULO_ANTIGO.plist"
    if agente_carregado "$ROTULO_ANTIGO"; then
        launchctl bootout "gui/$(id -u)/$ROTULO_ANTIGO" 2>/dev/null || launchctl bootout "gui/$(id -u)" "$antigo" 2>/dev/null || true
        echo "Agendamento antigo $ROTULO_ANTIGO descarregado."
    fi
    if [ -f "$antigo" ]; then
        rm -f "$antigo"
        echo "Removido $antigo."
    fi
}

instalar() {
    local repo="$TOWERMAN_REPO" modelo tmp
    [ -n "$repo" ] && [ -d "$repo/apple" ] || { echo "Defina TOWERMAN_REPO (o checkout do towerman) em $CONF ou no ambiente." >&2; exit 1; }
    modelo="$repo/tools/renovar-assinatura.plist.modelo"
    [ -r "$modelo" ] || modelo="$SCRIPT_DIR/renovar-assinatura.plist.modelo"
    [ -r "$modelo" ] || { echo "Falta o modelo renovar-assinatura.plist.modelo em $repo/tools." >&2; exit 1; }
    mkdir -p "$TOWERMAN_HOME" "$LOG_DIR" "$AGENTES"

    tmp="$SCRIPT_INSTALADO.novo.$$"
    cp "$repo/tools/renovar-assinatura.sh" "$tmp" && chmod 755 "$tmp" && mv -f "$tmp" "$SCRIPT_INSTALADO" \
        || { rm -f "$tmp"; echo "Não consegui copiar o script para $SCRIPT_INSTALADO." >&2; exit 1; }
    echo "Script copiado para $SCRIPT_INSTALADO."

    if [ ! -f "$CONF" ]; then
        cat > "$CONF" <<EOF
# Configuração da renovação da assinatura do Towerman (formato CHAVE=valor; não é executado).
# Veja o cabeçalho de tools/renovar-assinatura.sh para a lista de chaves.
TOWERMAN_REPO=$repo
# IPHONE_ID=
# WATCH_ID=
# DEVELOPER_DIR=
# LIMIAR_HORAS=48
EOF
        echo "Criado $CONF."
    fi

    LOG_ATIVO=1
    if ! sincronizar; then echo "Aviso: não consegui preparar a cópia das fontes em $COPIA_APPLE." >&2; fi
    LOG_ATIVO=0

    desligar_antigo

    esc() { printf '%s' "$1" | sed -e 's/[&|\\]/\\&/g'; }
    sed -e "s|@ROTULO@|$(esc "$ROTULO")|g" -e "s|@SCRIPT@|$(esc "$SCRIPT_INSTALADO")|g" \
        -e "s|@LOG_DIR@|$(esc "$LOG_DIR")|g" "$modelo" > "$PLIST.novo" \
        && plutil -lint "$PLIST.novo" >/dev/null && mv -f "$PLIST.novo" "$PLIST" \
        || { rm -f "$PLIST.novo"; echo "Não consegui gerar $PLIST." >&2; exit 1; }
    if agente_carregado "$ROTULO"; then launchctl bootout "gui/$(id -u)/$ROTULO" 2>/dev/null || true; fi
    launchctl bootstrap "gui/$(id -u)" "$PLIST" || { echo "launchctl bootstrap falhou para $PLIST." >&2; exit 1; }
    echo "Agendamento $ROTULO carregado: corre agora e de 6 em 6 horas. Registo: $LOG_FILE"
}

desinstalar() {
    if agente_carregado "$ROTULO"; then launchctl bootout "gui/$(id -u)/$ROTULO" 2>/dev/null || true; fi
    rm -f "$PLIST"
    desligar_antigo
    echo "Agendamento removido. $TOWERMAN_HOME fica (configuração e cópia das fontes); apague-o à mão se quiser."
}

# ---------- argumentos ----------

for a in "$@"; do
    case "$a" in
        --check) modo=check ;;
        --simular) modo=simular ;;
        --force) forcar=1 ;;
        instalar) modo=instalar ;;
        desinstalar) modo=desinstalar ;;
        -h|--help) sed -n '2,40p' "$0"; exit 0 ;;
        *) echo "Argumento desconhecido: $a (veja --help)" >&2; exit 1 ;;
    esac
done

mkdir -p "$TOWERMAN_HOME" "$LOG_DIR"
carregar_config

case "$modo" in
    instalar) instalar; exit 0 ;;
    desinstalar) desinstalar; exit 0 ;;
esac

if [ "$modo" = renovar ]; then
    LOG_ATIVO=1
    rodar_log
    if ! mkdir "$TRINCO" 2>/dev/null; then
        if kill -0 "$(cat "$TRINCO/pid" 2>/dev/null || echo 0)" 2>/dev/null; then
            log "Outra renovação já está a correr; saio."
            exit 0
        fi
        rm -rf "$TRINCO"; mkdir "$TRINCO"
    fi
    echo "$$" > "$TRINCO/pid"
fi

log "--- Renovação da assinatura do Towerman ($modo$([ "$forcar" = 1 ] && echo ', --force')) ---"

# Fontes e Xcode antes do bundle ID: o bundle ID pode vir do projeto.
sincronizar; FONTES_OK=$?
verificar_xcode; XCODE_OK=$?

if [ "$modo" = check ]; then
    log "Configuração: ${CONF} $([ -f "$CONF" ] && echo '(existe)' || echo '(não existe; uso os padrões)')"
    log "TOWERMAN_REPO=${TOWERMAN_REPO:-<não definido>}"
    log "Fontes para compilar: ${FONTE_APPLE:-<nenhumas>}"
    log "Xcode: $([ "$XCODE_OK" = 0 ] && echo "$DEVELOPER_DIR" || echo "indisponível: $XCODE_MOTIVO")"
    log "Conta Apple no Xcode: $(conta_presente && echo presente || echo 'nenhuma (No Accounts)')"
    log "Limiar: $LIMIAR_HORAS h"
fi

resolver_bundle || {
    if [ "$XCODE_OK" != 0 ]; then
        msg="não sei o bundle ID do app iOS: $XCODE_MOTIVO (ou defina TOWERMAN_IOS_BUNDLE_ID)"
        [ "$modo" = check ] && { aviso "$msg"; exit 0; }
        [ "$XCODE_OK" = 2 ] && adiar "$msg"
        falhar "$msg"
    fi
    falhar "não sei o bundle ID do app iOS: defina TOWERMAN_IOS_BUNDLE_ID em $CONF ou TOWERMAN_REPO"
}

avaliar_perfis
[ -n "$MIN_HORAS" ] || log "Nenhum perfil de $BUNDLE encontrado."

if [ "$modo" = check ]; then
    if [ "$XCODE_OK" = 0 ]; then resolver_aparelhos; fi
    if [ -z "$MIN_HORAS" ] || [ "$MIN_HORAS" -le "$LIMIAR_HORAS" ]; then
        log "Resultado: precisa de renovação ($(horas_txt); limiar $LIMIAR_HORAS h)."
    else
        log "Resultado: perfis válidos ($(horas_txt); limiar $LIMIAR_HORAS h)."
    fi
    exit 0
fi

if [ "$forcar" = 1 ]; then
    log "Renovação forçada (--force)."
elif [ -z "$MIN_HORAS" ]; then
    log "Sem perfil válido: renovação necessária."
elif [ "$MIN_HORAS" -le "$LIMIAR_HORAS" ]; then
    log "O perfil mais próximo expira em $MIN_HORAS h (limiar $LIMIAR_HORAS h): renovação necessária."
elif [ -f "$PENDENTE" ]; then
    log "Perfis válidos, mas a instalação da renovação de $(cat "$PENDENTE") ficou pendente: compila e instala sem mexer nos perfis."
else
    log "Perfis válidos ($(horas_txt); limiar $LIMIAR_HORAS h). Nada a fazer."
    exit 0
fi

# ---------- ordem segura: Xcode → conta → fontes → iPhone ----------
case "$XCODE_OK" in
    1) falhar "$XCODE_MOTIVO" ;;
    2) adiar "$XCODE_MOTIVO" ;;
esac
conta_presente || falhar "$MSG_SEM_CONTA" "$MSG_SEM_CONTA"
case "$FONTES_OK" in
    1) falhar "sem fontes para compilar: defina TOWERMAN_REPO em $CONF" ;;
    2) adiar "o checkout em $TOWERMAN_REPO está num volume desmontado e não há cópia em $COPIA_APPLE" ;;
esac
PROJ="$FONTE_APPLE/Towerman.xcodeproj"

resolver_aparelhos
[ -n "$IPHONE" ] || falhar "nenhum iPhone emparelhado com este Mac: ligue-o por cabo uma vez e confie no Mac"
[ "$IPHONE_OK" = 0 ] || adiar "o iPhone $IPHONE não está ao alcance (ligado à mesma rede ou por cabo, desbloqueado)"

# O UDID do relógio só entra no perfil do app do relógio depois de o Xcode o registar, o que
# acontece ao compilar o esquema TowermanWatch para ele. Só compila se o perfil ainda não o tiver.
compilar_relogio=0
if [ -n "$RELOGIO" ] && ! perfil_tem_aparelho .watchkitapp "$RELOGIO"; then
    if [ "$RELOGIO_OK" = 0 ]; then
        compilar_relogio=1
    else
        aviso "o perfil do relógio não inclui $RELOGIO e o relógio não está ao alcance; o app do relógio pode não instalar"
    fi
fi

# ---------- perfis: backup e compilação ----------
BACKUP="$TOWERMAN_HOME/perfis-backup"
mover=""
while IFS='|' read -r p appid epoch horas devs; do
    [ -n "$p" ] || continue
    if [ "$forcar" = 1 ] || [ "$horas" -le "$LIMIAR_HORAS" ]; then mover="$mover$p|$appid"$'\n'; fi
done <<< "$PERFIS"

if [ "$modo" = simular ]; then
    while IFS='|' read -r p appid; do
        [ -n "$p" ] && log "[simular] moveria para $BACKUP: $appid ($(basename "$p"))"
    done <<< "$mover"
    [ "$compilar_relogio" = 1 ] && log "[simular] compilaria o esquema TowermanWatch para o relógio $RELOGIO (regista o UDID no perfil)"
    log "[simular] compilaria o esquema Towerman para o iPhone $IPHONE com -allowProvisioningUpdates, a partir de $COPIA_APPLE"
    log "[simular] instalaria o app no iPhone $IPHONE; o app do relógio vai embutido e o iPhone instala-o"
    log "[simular] fim: nada foi copiado, compilado nem movido."
    exit 0
fi

rm -rf "$BACKUP" "$BUILD_DIR"
mkdir -p "$BACKUP" "$PERFIS_DIR"
while IFS='|' read -r p appid; do
    [ -n "$p" ] || continue
    mv -f "$p" "$BACKUP/" && printf '%s\n' "$p" >> "$BACKUP/origens" \
        && log "Perfil movido para backup (força um perfil novo): $appid"
done <<< "$mover"

restaurar_perfis() { # devolve cada perfil à pasta de onde saiu, sem pisar um perfil novo
    local p
    [ -f "$BACKUP/origens" ] || return 0
    while IFS= read -r p; do
        [ -f "$BACKUP/$(basename "$p")" ] && [ ! -e "$p" ] && mv -f "$BACKUP/$(basename "$p")" "$p"
    done < "$BACKUP/origens"
    rm -rf "$BACKUP"
    log "Perfis anteriores restaurados."
}

: > "$BUILD_LOG"
compilar() { # $1 esquema, $2 UDID, $3 pasta de derivados
    log "A compilar $1 para $2 (-allowProvisioningUpdates)..."
    xcodebuild -project "$PROJ" -scheme "$1" -configuration Debug -destination "id=$2" \
        -derivedDataPath "$3" -allowProvisioningUpdates build >> "$BUILD_LOG" 2>&1
}

if [ "$compilar_relogio" = 1 ]; then
    compilar TowermanWatch "$RELOGIO" "$BUILD_DIR/relogio" \
        || aviso "a compilação do TowermanWatch para o relógio falhou; continuo com o iPhone (veja $BUILD_LOG)"
fi

if ! compilar Towerman "$IPHONE" "$BUILD_DIR/iphone"; then
    grep -E 'error:|Unable to find|No Accounts' "$BUILD_LOG" | sort -u | tail -15 >> "$LOG_FILE"
    restaurar_perfis
    rm -rf "$BUILD_DIR"
    if grep -q 'No Accounts' "$BUILD_LOG"; then falhar "$MSG_SEM_CONTA" "$MSG_SEM_CONTA"; fi
    if grep -q 'Unable to find a destination' "$BUILD_LOG"; then
        adiar "o Xcode não encontrou o iPhone $IPHONE como destino"
    fi
    falhar "a compilação para o iPhone falhou; veja $BUILD_LOG" "A compilação falhou. Veja $BUILD_LOG."
fi
log "Compilação concluída."

# Confirma que há perfis novos, em vez de confiar no código de saída do xcodebuild.
avaliar_perfis
if [ -z "$MIN_HORAS" ] || [ "$MIN_HORAS" -le "$LIMIAR_HORAS" ]; then
    restaurar_perfis
    rm -rf "$BUILD_DIR"
    falhar "a compilação passou, mas os perfis não foram renovados ($(horas_txt))"
fi
rm -rf "$BACKUP"
validade="válida até $(date -v+"${MIN_HORAS}"H '+%d/%m %H:%M')"
log "Perfis renovados ($validade)."

# ---------- instalação ----------
app="$BUILD_DIR/iphone/Build/Products/Debug-iphoneos/Towerman.app"
estado="renovado e instalado"; saida=0
if [ ! -d "$app" ]; then
    aviso "não encontrei $app"
    estado="renovado, instalação pendente"; saida=2
elif xcrun devicectl device install app --device "$IPHONE" "$app" >> "$BUILD_LOG" 2>&1; then
    log "App instalado no iPhone $IPHONE."
else
    aviso "a instalação no iPhone falhou (bloqueado ou fora de alcance?); veja $BUILD_LOG"
    estado="renovado, instalação pendente"; saida=2
fi

# Relógio: o app vai embutido no do iPhone e o iPhone instala-o (app Watch > Towerman).
# A instalação direta é só uma tentativa extra; falhar não muda o estado final.
aviso_relogio=""
relogio_app="$app/Watch/TowermanWatch.app"
if [ "$saida" = 0 ] && [ -n "$RELOGIO" ] && [ -d "$relogio_app" ]; then
    emb="$TMPD/relogio.plist"
    if security cms -D -i "$relogio_app/embedded.mobileprovision" > "$emb" 2>/dev/null \
        && plutil -extract ProvisionedDevices json -o - "$emb" 2>/dev/null | grep -q "$RELOGIO"; then
        if [ "$RELOGIO_OK" = 0 ] && xcrun devicectl device install app --device "$RELOGIO" "$relogio_app" >> "$BUILD_LOG" 2>&1; then
            log "App instalado também diretamente no relógio $RELOGIO."
        else
            aviso_relogio="instalação direta no relógio não feita; o iPhone instala o app do relógio"
        fi
    else
        aviso_relogio="o perfil do app do relógio não inclui $RELOGIO; instale pelo Xcode (esquema TowermanWatch)"
    fi
    [ -z "$aviso_relogio" ] || aviso "$aviso_relogio"
fi

rm -rf "$BUILD_DIR"
if [ "$saida" = 0 ]; then rm -f "$PENDENTE"; else date '+%Y-%m-%d %H:%M' > "$PENDENTE"; fi
log "Estado final: $estado ($validade)."
if [ "$saida" = 0 ]; then
    notificar "Towerman" "Assinatura renovada e app instalado no iPhone ($validade).${aviso_relogio:+ Relógio: $aviso_relogio.}"
else
    notificar "Towerman: instalação pendente" "Assinatura renovada ($validade), mas o app não chegou ao iPhone. Desbloqueie-o perto do Mac: a próxima execução instala."
fi
exit "$saida"
