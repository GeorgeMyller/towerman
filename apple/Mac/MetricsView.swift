import SwiftUI

// MARK: - Métricas (Mac)

/// Uma coluna por computador pareado e uma coluna «Total». Cada computador calcula as suas
/// métricas; o Mac só as junta na vista.
struct MetricsView: View {
    @ObservedObject var store: DeckStore
    @State private var days = 7
    @State private var columns: [String: MetricsColumn] = [:]

    private var answered: [ServerMetrics] { store.servers.compactMap { columns[$0.id]?.metrics } }

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack {
                Picker("Período", selection: $days) {
                    Text("1 dia").tag(1)
                    Text("7 dias").tag(7)
                    Text("30 dias").tag(30)
                }
                .pickerStyle(.segmented)
                .fixedSize()
                Spacer()
                Button("Atualizar") { Task { await load() } }
            }
            ScrollView([.vertical, .horizontal]) {
                Grid(alignment: .leading, horizontalSpacing: 18, verticalSpacing: 8) {
                    GridRow {
                        Text("Métrica").foregroundStyle(Tower.soft)
                        Text("Alvo").foregroundStyle(Tower.soft)
                        ForEach(store.servers) { Text($0.name).font(.headline) }
                        Text("Total").font(.headline)
                    }
                    ForEach(MetricRow.sections, id: \.0) { section in
                        GridRow {
                            TowerLabel(section.0).gridCellColumns(store.servers.count + 3)
                        }
                        ForEach(section.1) { row in
                            GridRow {
                                Text(row.label)
                                Text(row.target).font(.caption).foregroundStyle(Tower.soft)
                                ForEach(store.servers) { server in cell(row, columns[server.id] ?? .loading) }
                                Text(row.total(answered)).monospacedDigit()
                            }
                        }
                    }
                    GridRow {
                        TowerLabel("Respostas por canal").gridCellColumns(store.servers.count + 3)
                    }
                    ForEach(store.servers) { server in
                        if let m = columns[server.id]?.metrics {
                            ForEach(m.atencao.por_canal, id: \.self) { linha in
                                GridRow {
                                    Text("\(server.name) · \(Self.channelName(linha))")
                                    Text("")
                                    Text("\(linha.respostas)").monospacedDigit().gridCellColumns(store.servers.count + 1)
                                }
                            }
                        }
                    }
                }
                .padding(.bottom, 8)
            }
            Text("Respostas tocadas numa notificação espelhada no relógio contam como iPhone. As colisões que chegam a commit são uma aproximação.")
                .font(.caption)
                .foregroundStyle(Tower.soft)
        }
        .padding(18)
        .task(id: days) { await load() }
    }

    @ViewBuilder
    private func cell(_ row: MetricRow, _ column: MetricsColumn) -> some View {
        switch column {
        case .loading: ProgressView().controlSize(.small)
        case .unsupported: Text("versão sem métricas").font(.caption).foregroundStyle(Tower.soft)
        case .offline(let why): Text("sem ligação").font(.caption).foregroundStyle(Tower.amber).help(why)
        case .ok(let m):
            VStack(alignment: .leading, spacing: 1) {
                Text(row.value(m)).monospacedDigit()
                if (1..<fewCases).contains(row.cases(m)) {
                    Text("poucos dados").font(.caption2).foregroundStyle(Tower.amber)
                }
            }
        }
    }

    private static func channelName(_ l: ServerMetrics.ChannelRow) -> String {
        switch l.origem {
        case "relogio": return "\(l.nome) (relógio)"
        case "iphone": return "\(l.nome) (iPhone)"
        case "mac": return "\(l.nome) (Mac)"
        default: return l.nome
        }
    }

    private func load() async {
        for server in store.servers { columns[server.id] = .loading }
        await withTaskGroup(of: (String, MetricsColumn).self) { group in
            for server in store.servers {
                group.addTask { await (server.id, store.metrics(serverId: server.id, days: days)) }
            }
            for await (id, column) in group { columns[id] = column }
        }
    }
}
