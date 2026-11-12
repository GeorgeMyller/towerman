import ActivityKit
import Foundation

/// Liga, atualiza e desliga a Live Activity da torre conforme o estado dos agentes.
/// Existe enquanto houver agentes em espera ou em curso; o relógio recebe-a na Smart Stack.
@MainActor
final class TowerActivityController {
    static let shared = TowerActivityController()

    private var activity: ActivityKit.Activity<TowerActivityAttributes>?

    func update(with agents: [Agent]) {
        guard ActivityAuthorizationInfo().areActivitiesEnabled else { return }
        // Depois de o app reabrir, retoma a atividade que ficou no ecrã em vez de criar outra.
        if activity == nil { activity = ActivityKit.Activity<TowerActivityAttributes>.activities.first }
        let s = TowerSummary(agents: agents)

        guard s.waiting > 0 || s.running > 0 else {
            if let a = activity {
                activity = nil
                Task { await a.end(ActivityContent(state: s, staleDate: nil), dismissalPolicy: .immediate) }
            }
            return
        }
        let content = ActivityContent(state: s, staleDate: nil, relevanceScore: s.waiting > 0 ? 100 : 10)
        if let a = activity {
            guard a.content.state != s else { return }
            Task { await a.update(content) }
        } else {
            // Só arranca com o app à frente; em segundo plano o iOS recusa e tenta-se na próxima mudança.
            activity = try? ActivityKit.Activity.request(attributes: TowerActivityAttributes(), content: content, pushType: nil)
        }
    }
}
