---
name: Towerman
description: Deck de agentes de código no iPhone e no Apple Watch, no mundo da torre de controle do Towerman.
colors:
  ground: "#0B1626"
  panel: "#12223A"
  panel-raised: "#1A2E4A"
  line: "#26405F"
  text: "#E3EBF5"
  soft: "#8EA3C0"
  amber: "#FFB547"
  on-amber: "#1A1205"
  green: "#5BBF8A"
  red: "#F0645A"
  blue: "#5AA9FF"
  parked: "#6F7F99"
typography:
  title:
    fontFamily: "SF Pro (system), .largeTitle"
    fontWeight: 700
  label:
    fontFamily: "SF Pro (system), .caption2 semibold, uppercase"
    fontWeight: 600
    letterSpacing: "1.6pt"
  led-number:
    fontFamily: "SF Pro (system), .title3 bold, monospacedDigit"
    fontWeight: 700
  row-name:
    fontFamily: "SF Pro (system), .subheadline semibold / .headline"
    fontWeight: 600
  body:
    fontFamily: "SF Pro (system), .body / .caption"
    fontWeight: 400
  command:
    fontFamily: "SF Mono (system), .callout monospaced"
    fontWeight: 400
rounded:
  panel: "14px"
  button: "12px"
  led: "8px"
  command-well: "8px"
  tag: "4px"
spacing:
  gutter: "16px"
  strip-padding: "14px"
  section-gap: "22px"
  destructive-isolation: "28px"
  row-height: "60px"
components:
  request-strip:
    backgroundColor: "{colors.amber}"
    textColor: "{colors.on-amber}"
    rounded: "{rounded.panel}"
    padding: "{spacing.strip-padding}"
  panel:
    backgroundColor: "{colors.panel}"
    textColor: "{colors.text}"
    rounded: "{rounded.panel}"
  led-counter:
    backgroundColor: "{colors.panel}"
    textColor: "{colors.soft}"
    rounded: "{rounded.led}"
    padding: "6px 10px"
  button-allow:
    backgroundColor: "{colors.on-amber}"
    textColor: "{colors.amber}"
    rounded: "{rounded.button}"
    height: "48px"
  button-deny:
    backgroundColor: "transparent"
    textColor: "{colors.on-amber}"
    rounded: "{rounded.button}"
    height: "48px"
    width: "104px"
  state-tag:
    backgroundColor: "{colors.ground}"
    rounded: "{rounded.tag}"
    padding: "3px 6px"
---

# Design System: Towerman

## Overview

**Creative North Star: "A torre de controle no bolso"**

O app é o radar da torre de controle do Towerman, levado para o telemóvel e para o pulso. Cada agente é um ponto num radar cujos sectores são os seus computadores. O ponto que precisa de você pulsa em âmbar e desce para uma faixa de pedido, onde responde com um toque. Tudo o resto fica no quadro de partidas.

O mundo vem da vista "Torre" do painel web do Towerman, adaptado para não parecer um terminal: azul-marinho sem brilho, SF Pro na interface, mono só no comando, verde sóbrio em vez de néon. A navegação, as listas, as folhas e os controles são os nativos do iOS e do watchOS. O gesto próprio é o radar.

**Key Characteristics:**
- Sempre escuro: a torre à noite.
- Âmbar só para "aguardando você"; o azul é a cor de interação.
- Contadores LED, radar com sectores por computador e quadro de partidas.
- SF Pro em tudo; SF Mono apenas no comando que vai ser aprovado.

## Colors

### Primary
- **Âmbar** (`amber` #FFB547): a faixa inteira de um agente que aguarda você, o ponto que pulsa no radar, o anel interior tracejado e o contorno do contador "À espera". A tinta sobre âmbar é `on-amber` #1A1205 (≈ 11:1).

### Secondary
- **Azul da torre** (`blue` #5AA9FF): a cor de interação (tint) no iPhone e no relógio, e o varrimento do radar.

### Tertiary
- **Verde sóbrio** (`green` #5BBF8A): em curso. **Vermelho** (`red` #F0645A): erro e desconectado. **Aço** (`parked` #6F7F99): parado, concluído e encerrado.

### Neutral
- **Marinho** (`ground` #0B1626): fundo. **Painel** (`panel` #12223A) com linha de aço de 1 pt (`line` #26405F): radar, quadro, contadores e listas.
- **Texto** (`text` #E3EBF5) e **texto suave** (`soft` #8EA3C0).

### Named Rules
**The Amber Means You Rule.** O âmbar só aparece onde um agente aguarda você. Os controles usam o azul.

**The Hollow Blip Rule.** Ponto cheio quer dizer ocupado; ponto oco quer dizer livre. O estado lê-se também sem cor.

**The No Glow Rule.** Nada brilha: nem halos, nem gradientes de fundo, nem néon. A profundidade vem dos painéis e das linhas de 1 pt.

## Typography

Só fontes de sistema, com Dynamic Type.

### Hierarchy
- **Título** (`.largeTitle` bold): "Towerman".
- **Rótulo** (`.caption2` semibold, maiúsculas, espaçamento 1,6 pt): "RADAR", "AGUARDANDO VOCÊ", "PARTIDAS".
- **Número LED** (`.title3` bold, algarismos de largura fixa).
- **Nome** (`.headline` na faixa, `.subheadline` semibold no quadro).
- **Corpo** (`.body`, `.caption`): atividade e último pedido.
- **Comando** (`.callout` monoespaçado): só o comando a aprovar.

### Named Rules
**The Mono For Commands Rule.** A letra mono aparece apenas no comando ou ficheiro que vai ser aprovado, nunca como figurino "técnico".

## Layout

- Margem lateral de 16 pt; painéis de largura total com cantos de 14 pt.
- Ordem: linha de estado ("1 computador em linha"), contadores LED, filtro segmentado por computador (só com 2 ou mais), radar (250 pt), "AGUARDANDO VOCÊ" com as faixas âmbar, "PARTIDAS" com uma linha por agente (60 pt).
- Com letra de acessibilidade, as faixas sobem acima do radar (220 pt), os contadores passam a uma grade de duas colunas, o cabeçalho da faixa empilha e os botões ficam um por linha.
- A configuração do servidor fica numa folha nativa, aberta pela antena na barra.

## Elevation & Depth

Plano: painéis marinho sobre fundo marinho, separados por linhas de aço de 1 pt. Não há sombras nem brilhos.

## Shapes

Retângulos contínuos: 14 pt nos painéis e nas faixas, 12 pt nos botões, 8 pt nos contadores e no poço do comando, 4 pt nas etiquetas de estado. O radar é feito de círculos e linhas retas.

## Components

### Buttons
- **Permitir** (primário): cheio a `on-amber`, texto âmbar, 48 pt, ocupa a largura livre.
- **Negar**: contorno de 2 pt, 104 pt de largura, a 28 pt do primário (na mesma linha ou empilhado). No relógio, a 14 pt.

### Chips
- **Etiqueta de estado** (quadro de partidas): maiúsculas sobre marinho com linha de aço; "ESPERA" fica cheia a âmbar.
- **Contador LED**: número e rótulo; "À espera" ganha contorno âmbar quando é maior que zero.

### Cards / Containers
- **Faixa de pedido**: âmbar inteiro; ponto, nome, origem em maiúsculas com os minutos, título, comando e botões. Sem botões, mostra "Responda no computador." e "Focar".
- **Quadro de partidas**: um painel com linhas separadas por 1 pt; ponto, nome, atividade, etiqueta e computador.

### Navigation
NavigationStack com título grande, antena na barra (estado da ligação e configuração), detalhe por push, toque num ponto do radar para abrir o agente, puxar para atualizar.

### Radar (signature)
Um sector por computador, separados por linhas de aço. O anel interior (tracejado âmbar) mostra quem aguarda você, o do meio quem está em curso e o exterior quem está parado. Um varrimento azul dá uma volta a cada 7 s; o ponto âmbar pulsa a cada 1,6 s. Os nomes ficam do lado de fora do ponto, sobre o fundo do painel. Com "Reduzir movimento", nada se mexe; com letra grande, só os pontos âmbar mantêm o nome. No relógio, o radar é o primeiro ecrã.

### Ícone do app
O mesmo radar, sobre marinho plano: dois sectores (Mac e Linux), varrimento azul, ponto âmbar com o anel do pulso no anel interior, um ponto verde em curso e um oco parado. É desenhado em código por `apple/tools/make_icon.swift` (1024 pt, sem transparência) e partilhado pelo iPhone e pelo relógio, onde a máscara circular enquadra o radar.

## Do's and Don'ts

### Do:
- **Do** levar todo o agente que aguarda você para as faixas âmbar, com ou sem botões.
- **Do** mostrar o comando exato antes de pedir para o permitir.
- **Do** manter "Negar" afastado de "Permitir" por espaço deliberado.
- **Do** usar controles nativos e SF Symbols.

### Don't:
- **Don't** usar fundo preto com letra mono e verde néon: o app não é um terminal.
- **Don't** pôr brilhos, halos ou gradientes de fundo.
- **Don't** usar o âmbar como cor de interação.
- **Don't** indicar estado apenas com cor.
