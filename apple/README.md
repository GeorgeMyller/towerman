# apple/

Os três apps do Towerman (iPhone, Apple Watch e Mac) num só projeto, gerado pelo XcodeGen:

```sh
cd apple && xcodegen generate     # cria Towerman.xcodeproj a partir de project.yml
```

Esquemas: **Towerman** (iPhone, com o relógio embutido), **TowermanWatch** e **TowermanMac**. O build do Mac compila o servidor da raiz deste repositório (ou de `TOWERMAN_SRC`) para dentro do app, como binário universal.

- `tools/dmg.sh` gera `dist/Towerman-<versão>.dmg` para instalar noutro Mac.
- `tools/test.sh` corre os testes das políticas puras (`Shared/WatchSyncPolicy.swift`) com o swiftc, sem Xcode; o CI chama-o no macOS.
- `tools/make_icon.swift` desenha o ícone (`swift tools/make_icon.swift saida.png`).
- `PRODUCT.md` e `DESIGN.md` descrevem o produto e o sistema visual.

Não edite o `Towerman.xcodeproj` à mão: mude o `project.yml` e gere de novo. Veja o README da raiz para instalar e parear.

A equipa e os IDs (bundle, App Group, Keychain) vêm de `Config/Towerman.xcconfig`, com padrões neutros (`com.example`). Para assinar com a sua conta:

```sh
cp Config/Local.xcconfig.example Config/Local.xcconfig   # ignorado pelo git
```

Preencha `TOWERMAN_TEAM_ID` e `TOWERMAN_ID_PREFIX` no `Config/Local.xcconfig`. Depois de instalar, não mude os IDs: perde o pareamento e o Keychain dos aparelhos.
