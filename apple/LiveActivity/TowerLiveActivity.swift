import ActivityKit
import SwiftUI
import WidgetKit

@main
struct TowerLiveBundle: WidgetBundle {
    var body: some Widget {
        TowerLiveActivity()
    }
}

struct TowerLiveActivity: Widget {
    var body: some WidgetConfiguration {
        ActivityConfiguration(for: TowerActivityAttributes.self) { context in
            TowerActivityView(s: context.state)
                .activityBackgroundTint(Tower.ground)
                .activitySystemActionForegroundColor(Tower.text)
                .widgetURL(context.state.topId.flatMap(TowerLink.agent))
        } dynamicIsland: { context in
            let s = context.state
            return DynamicIsland {
                DynamicIslandExpandedRegion(.leading) {
                    Label("\(s.waiting)", systemImage: "hand.raised.fill")
                        .foregroundStyle(s.waiting > 0 ? Tower.amber : Tower.soft)
                        .font(.headline.monospacedDigit())
                }
                DynamicIslandExpandedRegion(.trailing) {
                    Label("\(s.running)", systemImage: "play.fill")
                        .foregroundStyle(s.running > 0 ? Tower.green : Tower.soft)
                        .font(.headline.monospacedDigit())
                }
                DynamicIslandExpandedRegion(.bottom) {
                    TowerTopRequest(s: s)
                }
            } compactLeading: {
                Image(systemName: s.waiting > 0 ? "hand.raised.fill" : "dot.radiowaves.left.and.right")
                    .foregroundStyle(s.waiting > 0 ? Tower.amber : Tower.green)
            } compactTrailing: {
                Text("\(s.waiting > 0 ? s.waiting : s.running)")
                    .font(.headline.monospacedDigit())
                    .foregroundStyle(s.waiting > 0 ? Tower.amber : Tower.green)
            } minimal: {
                Text("\(s.waiting > 0 ? s.waiting : s.running)")
                    .font(.headline.monospacedDigit())
                    .foregroundStyle(s.waiting > 0 ? Tower.amber : Tower.green)
            }
            .widgetURL(s.topId.flatMap(TowerLink.agent))
        }
        // Sem .small: a Live Activity não vai para a Smart Stack do relógio. Lá, tocar nela pedia
        // para abrir o iPhone; o widget do próprio relógio abre o pedido no pulso.
    }
}

struct TowerActivityView: View {
    let s: TowerSummary

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                Text("TOWERMAN")
                    .font(.caption.weight(.heavy))
                    .foregroundStyle(Tower.soft)
                Spacer()
                Label("\(s.waiting) espera", systemImage: "hand.raised.fill")
                    .foregroundStyle(s.waiting > 0 ? Tower.amber : Tower.soft)
                Label("\(s.running) em curso", systemImage: "play.fill")
                    .foregroundStyle(s.running > 0 ? Tower.green : Tower.soft)
            }
            .font(.caption.weight(.semibold).monospacedDigit())
            TowerTopRequest(s: s)
        }
        .padding(14)
    }
}

struct TowerTopRequest: View {
    let s: TowerSummary

    var body: some View {
        if s.waiting > 0, let title = s.topTitle {
            VStack(alignment: .leading, spacing: 1) {
                HStack {
                    Text(title).font(.headline).foregroundStyle(Tower.text).lineLimit(1)
                    if let since = s.topSince {
                        Spacer(minLength: 4)
                        Text(since, style: .timer)
                            .font(.caption.monospacedDigit())
                            .foregroundStyle(Tower.amber)
                            .multilineTextAlignment(.trailing)
                            .frame(maxWidth: 60, alignment: .trailing)
                    }
                }
                if let detail = s.topDetail {
                    Text(detail).font(.caption).foregroundStyle(Tower.soft).lineLimit(2)
                }
            }
        } else {
            Text(s.running > 0 ? "Agentes a trabalhar. Nada à sua espera." : "Céu limpo.")
                .font(.subheadline)
                .foregroundStyle(Tower.text)
        }
    }
}
