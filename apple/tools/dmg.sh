#!/bin/sh
# Compila o Towerman.app (com o servidor towerman lá dentro) e gera dist/Towerman-<versão>.dmg,
# com a ligação para /Applications. Uso: tools/dmg.sh (a partir de apple/).
set -e
cd "$(dirname "$0")/.."

SRC="${TOWERMAN_SRC:-$PWD/..}"
VERSAO=$(sed -n 's/.*Versao *= *"\(.*\)".*/\1/p' "$SRC/internal/config/config.go")
BUILD="${TMPDIR:-/tmp}/towerman-mac"
PALCO="$BUILD/dmg"
SAIDA="$PWD/dist/Towerman-${VERSAO:-dev}.dmg"

xcodegen generate
# TOWERMAN_XCODEBUILD_ARGS: definições extra (o workflow de release usa-as para assinar ad hoc).
# shellcheck disable=SC2086
xcodebuild -project Towerman.xcodeproj -scheme TowermanMac -configuration Release \
  -derivedDataPath "$BUILD" $TOWERMAN_XCODEBUILD_ARGS build > "$BUILD.log" 2>&1 || {
  # Falhou: mostra os erros (o log completo fica ao lado da pasta de build).
  grep -E 'error:|\*\* BUILD' "$BUILD.log" | head -40 >&2
  echo "xcodebuild falhou; log completo em $BUILD.log" >&2
  exit 1
}
grep -E '^\*\*|warning: towerman' "$BUILD.log" || true
APP="$BUILD/Build/Products/Release/Towerman.app"
[ -x "$APP/Contents/MacOS/towerman-cli" ] || { echo "falta o towerman-cli em $APP" >&2; exit 1; }

rm -rf "$PALCO" && mkdir -p "$PALCO" "$(dirname "$SAIDA")"
cp -R "$APP" "$PALCO/"
ln -s /Applications "$PALCO/Applications"
rm -f "$SAIDA"
hdiutil create -volname "Towerman" -srcfolder "$PALCO" -fs HFS+ -format UDZO -ov "$SAIDA" >/dev/null
echo "$SAIDA"
