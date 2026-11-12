// Desenha o ícone do Towerman (1024 × 1024) no mundo da Torre: o radar da torre de controle,
// com um sector por computador, o varrimento azul e o ponto âmbar de quem aguarda você.
// Uso: swift tools/make_icon.swift <saída.png>
import AppKit

let side: CGFloat = 1024
let out = CommandLine.arguments.count > 1 ? CommandLine.arguments[1] : "AppIcon.png"

func rgb(_ hex: UInt32, _ alpha: CGFloat = 1) -> CGColor {
    CGColor(red: CGFloat((hex >> 16) & 0xFF) / 255, green: CGFloat((hex >> 8) & 0xFF) / 255,
            blue: CGFloat(hex & 0xFF) / 255, alpha: alpha)
}

let ground: UInt32 = 0x0B1626, line: UInt32 = 0x3D5878, amber: UInt32 = 0xFFB547
let blue: UInt32 = 0x5AA9FF, green: UInt32 = 0x5BBF8A, soft: UInt32 = 0x8EA3C0

let space = CGColorSpace(name: CGColorSpace.sRGB)!
let ctx = CGContext(data: nil, width: Int(side), height: Int(side), bitsPerComponent: 8, bytesPerRow: 0,
                    space: space, bitmapInfo: CGImageAlphaInfo.noneSkipLast.rawValue)!
// Coordenadas com y para baixo, como num ecrã.
ctx.translateBy(x: 0, y: side)
ctx.scaleBy(x: 1, y: -1)

let c = CGPoint(x: side / 2, y: side / 2)
func point(_ r: CGFloat, _ degrees: CGFloat) -> CGPoint {
    let a = degrees * .pi / 180
    return CGPoint(x: c.x + cos(a) * r, y: c.y + sin(a) * r)
}

// Fundo marinho, plano.
ctx.setFillColor(rgb(ground))
ctx.fill(CGRect(x: 0, y: 0, width: side, height: side))

// Varrimento: cunha azul que se apaga para trás, a terminar a -40°.
let sweepEnd: CGFloat = -40, sweepSpan: CGFloat = 50, outer: CGFloat = 400
for i in 0..<60 {
    let t = CGFloat(i) / 60
    let a0 = sweepEnd - sweepSpan * (1 - t), a1 = sweepEnd - sweepSpan * (1 - t - 1.0 / 60)
    // Polígono explícito: com o eixo y invertido, um arco poderia ir pelo lado longo.
    ctx.move(to: c)
    for k in 0...4 { ctx.addLine(to: point(outer - 5, a0 + (a1 - a0) * CGFloat(k) / 4)) }
    ctx.closePath()
    ctx.setFillColor(rgb(blue, 0.30 * t * t))
    ctx.fillPath()
}

// Anéis: parados (exterior) e em curso (meio) a aço; quem aguarda você (interior) tracejado a âmbar.
ctx.setLineWidth(14)
ctx.setStrokeColor(rgb(line))
ctx.strokeEllipse(in: CGRect(x: c.x - outer, y: c.y - outer, width: outer * 2, height: outer * 2))
ctx.strokeEllipse(in: CGRect(x: c.x - 285, y: c.y - 285, width: 570, height: 570))
ctx.saveGState()
ctx.setStrokeColor(rgb(amber, 0.7))
ctx.setLineWidth(12)
ctx.setLineDash(phase: 0, lengths: [26, 22])
ctx.strokeEllipse(in: CGRect(x: c.x - 170, y: c.y - 170, width: 340, height: 340))
ctx.restoreGState()

// Dois sectores: Mac e Linux, separados por uma linha vertical.
ctx.setStrokeColor(rgb(line))
ctx.setLineWidth(11)
ctx.move(to: CGPoint(x: c.x, y: c.y - outer))
ctx.addLine(to: CGPoint(x: c.x, y: c.y + outer))
ctx.strokePath()

// Aresta do varrimento.
ctx.setStrokeColor(rgb(blue, 0.85))
ctx.setLineWidth(12)
ctx.setLineCap(.round)
ctx.move(to: c)
ctx.addLine(to: point(outer, sweepEnd))
ctx.strokePath()

// Ponto em curso (verde, anel do meio) e ponto parado (oco, anel exterior).
let g = point(285, 25)
ctx.setFillColor(rgb(green))
ctx.fillEllipse(in: CGRect(x: g.x - 24, y: g.y - 24, width: 48, height: 48))
let p = point(outer, 130)
ctx.setStrokeColor(rgb(soft))
ctx.setLineWidth(14)
ctx.strokeEllipse(in: CGRect(x: p.x - 22, y: p.y - 22, width: 44, height: 44))

// Quem aguarda você: ponto âmbar com o anel do pulso, no anel interior.
let a = point(170, -140)
ctx.setStrokeColor(rgb(amber, 0.5))
ctx.setLineWidth(12)
ctx.strokeEllipse(in: CGRect(x: a.x - 72, y: a.y - 72, width: 144, height: 144))
ctx.setFillColor(rgb(amber))
ctx.fillEllipse(in: CGRect(x: a.x - 40, y: a.y - 40, width: 80, height: 80))

// Centro do radar.
ctx.setFillColor(rgb(soft))
ctx.fillEllipse(in: CGRect(x: c.x - 14, y: c.y - 14, width: 28, height: 28))

let image = ctx.makeImage()!
let rep = NSBitmapImageRep(cgImage: image)
try! rep.representation(using: .png, properties: [:])!.write(to: URL(fileURLWithPath: out))
print("ícone escrito em \(out)")
