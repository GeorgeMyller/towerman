import SwiftUI
import WidgetKit

// Extensão de widgets do relógio: complicações, Smart Stack e o controlo do Centro de Controlo.
// Os dados vêm do resumo que o app grava no App Group; o app recarrega quando muda.

@main
struct TowerWidgets: WidgetBundle {
    var body: some Widget {
        TowerWidget()
        TowerControl()
    }
}

// MARK: - Widget

struct TowerEntry: TimelineEntry {
    let date: Date
    let summary: TowerSummary
    /// A Smart Stack sobe o widget quando há pedido em espera.
    var relevance: TimelineEntryRelevance? {
        TimelineEntryRelevance(score: summary.waiting > 0 ? 100 : summary.running > 0 ? 10 : 0)
    }
}

struct TowerProvider: TimelineProvider {
    func placeholder(in context: Context) -> TowerEntry {
        TowerEntry(date: .now, summary: TowerSummary(waiting: 1, running: 2, topTitle: "towerman",
                                                     topDetail: "Bash? git push"))
    }

    func getSnapshot(in context: Context, completion: @escaping (TowerEntry) -> Void) {
        completion(context.isPreview ? placeholder(in: context) : TowerEntry(date: .now, summary: .load()))
    }

    func getTimeline(in context: Context, completion: @escaping (Timeline<TowerEntry>) -> Void) {
        // O app pede o recarregamento a cada mudança; sem ele, não há nada de novo para mostrar.
        completion(Timeline(entries: [TowerEntry(date: .now, summary: .load())], policy: .never))
    }
}

struct TowerWidget: Widget {
    var body: some WidgetConfiguration {
        StaticConfiguration(kind: "TowerWidget", provider: TowerProvider()) { entry in
            TowerWidgetView(entry: entry)
                .containerBackground(Tower.ground, for: .widget)
                .widgetURL(entry.summary.topId.flatMap(TowerLink.agent))
        }
        .configurationDisplayName("Towerman")
        .description("Pedidos em espera e agentes em curso.")
        .supportedFamilies([.accessoryRectangular, .accessoryCircular, .accessoryCorner, .accessoryInline])
    }
}

struct TowerWidgetView: View {
    let entry: TowerEntry
    @Environment(\.widgetFamily) private var family

    private var s: TowerSummary { entry.summary }
    private var tint: Color { s.waiting > 0 ? Tower.amber : s.running > 0 ? Tower.green : Tower.soft }
    private var count: Int { s.waiting > 0 ? s.waiting : s.running }

    var body: some View {
        switch family {
        case .accessoryCircular:
            ZStack {
                AccessoryWidgetBackground()
                VStack(spacing: 0) {
                    Text("\(count)").font(.title3.weight(.bold).monospacedDigit())
                    Image(systemName: s.waiting > 0 ? "hand.raised.fill" : "dot.radiowaves.left.and.right")
                        .font(.caption2)
                }
                .foregroundStyle(tint)
                .widgetAccentable()
            }
        case .accessoryCorner:
            Image(systemName: s.waiting > 0 ? "hand.raised.fill" : "dot.radiowaves.left.and.right")
                .font(.title3)
                .foregroundStyle(tint)
                .widgetLabel(s.headline)
        case .accessoryInline:
            Text("Towerman · \(s.headline)")
        default:
            VStack(alignment: .leading, spacing: 1) {
                HStack(spacing: 4) {
                    Circle().fill(tint).frame(width: 7, height: 7)
                    Text(s.headline.uppercased())
                        .font(.caption2.weight(.bold))
                        .foregroundStyle(tint)
                        .widgetAccentable()
                    if let since = s.topSince {
                        Spacer(minLength: 2)
                        Text(since, style: .relative)
                            .font(.caption2.monospacedDigit())
                            .foregroundStyle(Tower.soft)
                    }
                }
                if s.waiting > 0, let title = s.topTitle {
                    Text(title).font(.headline).lineLimit(1)
                    if let detail = s.topDetail {
                        Text(detail).font(.caption2).foregroundStyle(Tower.soft).lineLimit(2)
                    }
                } else {
                    Text("Towerman").font(.headline)
                    Text(s.running > 0 ? "Nada à sua espera." : "Nenhum agente ativo.")
                        .font(.caption2).foregroundStyle(Tower.soft)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}

// MARK: - Controlo (Centro de Controlo e botão de ação)

struct TowerControl: ControlWidget {
    var body: some ControlWidgetConfiguration {
        StaticControlConfiguration(kind: "TowerControl") {
            ControlWidgetButton(action: OpenTowerIntent()) {
                Label("Towerman", systemImage: "dot.radiowaves.left.and.right")
            }
        }
        .displayName("Towerman")
        .description("Abre o radar e os pedidos em espera.")
    }
}
