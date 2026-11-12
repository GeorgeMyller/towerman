import ActivityKit

/// Live Activity da torre: uma só, com o resumo de todos os computadores.
/// No iPhone vive no ecrã bloqueado e na Dynamic Island; o watchOS mostra-a na Smart Stack.
public struct TowerActivityAttributes: ActivityAttributes {
    public typealias ContentState = TowerSummary

    public init() {}
}
