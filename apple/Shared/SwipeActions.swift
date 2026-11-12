import SwiftUI

/// Uma ação de arrastar: aparece por baixo do cartão e corre quando o arrasto passa do limite.
public struct SwipeAction {
    public let label: String
    public let icon: String
    public let tint: Color
    public let ink: Color
    public let run: () -> Void

    public init(_ label: String, icon: String, tint: Color, ink: Color = Tower.ground, run: @escaping () -> Void) {
        self.label = label
        self.icon = icon
        self.tint = tint
        self.ink = ink
        self.run = run
    }
}

/// Arrastar para os lados num cartão que não está numa List (o .swipeActions só existe lá).
/// Direita corre `leading`, esquerda corre `trailing`. Só um arrasto claramente horizontal
/// conta, para não roubar o scroll; a vibração marca o ponto a partir do qual a ação corre.
struct SwipeActionsModifier: ViewModifier {
    let leading: SwipeAction?
    let trailing: SwipeAction?
    let threshold: CGFloat

    @State private var dx: CGFloat = 0
    @State private var armed = false
    /// Durante o arrasto e logo depois, os toques do cartão ficam desligados: sem isto, o
    /// levantar do dedo também abria o detalhe ou carregava num botão por baixo.
    @State private var suppress = false

    private var current: SwipeAction? { dx > 0 ? leading : (dx < 0 ? trailing : nil) }

    /// O cartão segue o arrasto (dedo, rato ou trackpad).
    private func update(_ w: CGFloat) {
        var x = w
        if (x > 0 && leading == nil) || (x < 0 && trailing == nil) { x = 0 }
        // Para lá do limite, o cartão resiste: puxar mais não faz nada de novo.
        if abs(x) > threshold { x = (x > 0 ? 1 : -1) * (threshold + (abs(x) - threshold) * 0.25) }
        if x != 0 { suppress = true }
        dx = x
        armed = abs(x) >= threshold
    }

    /// Fim do arrasto: corre a ação se passou do limite e volta ao sítio.
    private func finish() {
        let action = armed ? current : nil
        withAnimation(.spring(response: 0.3, dampingFraction: 0.8)) {
            dx = 0
            armed = false
        }
        action?.run()
        if suppress {
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.4) { suppress = false }
        }
    }

    func body(content: Content) -> some View {
        content
            .disabled(suppress)
            .offset(x: dx)
            .background(alignment: dx >= 0 ? .leading : .trailing) {
                if let action = current {
                    RoundedRectangle(cornerRadius: 12, style: .continuous)
                        .fill(armed ? action.tint : action.tint.opacity(0.45))
                        .overlay(alignment: dx >= 0 ? .leading : .trailing) {
                            Label(action.label, systemImage: action.icon)
                                .font(.footnote.weight(.bold))
                                .foregroundStyle(action.ink)
                                .labelStyle(.titleAndIcon)
                                .lineLimit(1)
                                .padding(.horizontal, 12)
                        }
                }
            }
            .simultaneousGesture(
                DragGesture(minimumDistance: 18)
                    .onChanged { v in
                        let w = v.translation.width
                        guard abs(w) > abs(v.translation.height) * 1.6 else { return }
                        update(w)
                    }
                    .onEnded { _ in finish() }
            )
            #if os(macOS)
            .background(TrackpadSwipe(enabled: leading != nil || trailing != nil,
                                      onChange: { x in update(x) }, onEnd: { finish() }))
            #endif
            .sensoryFeedback(.impact, trigger: armed) { _, now in now }
            .accessibilityActions {
                if let leading { Button(leading.label, action: leading.run) }
                if let trailing { Button(trailing.label, action: trailing.run) }
            }
    }
}

public extension View {
    /// Ações de arrastar para os lados (direita: `leading`; esquerda: `trailing`).
    func towerSwipe(leading: SwipeAction? = nil, trailing: SwipeAction? = nil, threshold: CGFloat = 90) -> some View {
        modifier(SwipeActionsModifier(leading: leading, trailing: trailing, threshold: threshold))
    }
}

#if os(macOS)
import AppKit

/// Dois dedos para os lados no trackpad: o macOS entrega-os como scroll, não como arrasto.
/// Um monitor de eventos lê o scroll que cai sobre este cartão; o horizontal fica com o
/// cartão, o vertical (e a roda do rato) segue para o ScrollView.
private struct TrackpadSwipe: NSViewRepresentable {
    let enabled: Bool
    let onChange: (CGFloat) -> Void
    let onEnd: () -> Void

    func makeNSView(context: Context) -> NSView {
        let v = ProbeView()
        context.coordinator.view = v
        return v
    }

    func updateNSView(_ nsView: NSView, context: Context) {
        context.coordinator.enabled = enabled
        context.coordinator.onChange = onChange
        context.coordinator.onEnd = onEnd
    }

    func makeCoordinator() -> Coordinator { Coordinator() }

    static func dismantleNSView(_ nsView: NSView, coordinator: Coordinator) { coordinator.stop() }

    /// Uma vista vazia que não apanha cliques: só serve para saber onde está o cartão.
    final class ProbeView: NSView {
        override func hitTest(_ point: NSPoint) -> NSView? { nil }
    }

    final class Coordinator {
        weak var view: NSView?
        var enabled = true
        var onChange: (CGFloat) -> Void = { _ in }
        var onEnd: () -> Void = {}
        private var monitor: Any?
        private var dx: CGFloat = 0
        private var dy: CGFloat = 0
        /// nil: ainda por decidir; true: gesto horizontal deste cartão; false: scroll normal.
        private var horizontal: Bool?

        init() {
            monitor = NSEvent.addLocalMonitorForEvents(matching: .scrollWheel) { [weak self] e in
                self?.handle(e) ?? e
            }
        }

        func stop() {
            if let monitor { NSEvent.removeMonitor(monitor) }
            monitor = nil
        }

        deinit { stop() }

        private func handle(_ e: NSEvent) -> NSEvent? {
            // Inércia depois de soltar: engolida se o gesto era do cartão.
            if e.momentumPhase != [] { return horizontal == true ? nil : e }
            guard e.phase != [] else { return e } // roda do rato: não é gesto
            if e.phase.contains(.began) {
                dx = 0; dy = 0
                horizontal = nil
                guard enabled, let view, let window = view.window, e.window === window else {
                    horizontal = false
                    return e
                }
                let p = view.convert(e.locationInWindow, from: nil)
                if !view.bounds.contains(p) { horizontal = false }
                return e
            }
            guard horizontal != false else {
                if e.phase.contains(.ended) || e.phase.contains(.cancelled) { horizontal = nil }
                return e
            }
            // O sentido dos dedos, com ou sem o scroll natural.
            let sign: CGFloat = e.isDirectionInvertedFromDevice ? 1 : -1
            dx += e.scrollingDeltaX * sign
            dy += e.scrollingDeltaY
            if horizontal == nil, abs(dx) + abs(dy) > 8 {
                horizontal = abs(dx) > abs(dy) * 1.6
                if horizontal == false { return e }
            }
            if e.phase.contains(.ended) || e.phase.contains(.cancelled) {
                if horizontal == true { onEnd() }
                let era = horizontal
                horizontal = era == true ? true : nil // a inércia que vem a seguir ainda é dele
                DispatchQueue.main.asyncAfter(deadline: .now() + 0.6) { [weak self] in
                    if self?.horizontal == true { self?.horizontal = nil }
                }
                return era == true ? nil : e
            }
            if horizontal == true {
                onChange(dx)
                return nil
            }
            return e
        }
    }
}
#endif
