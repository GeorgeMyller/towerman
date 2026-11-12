import Foundation

// Testes da política de envio ao relógio e de alerta no pulso (apple/Shared/WatchSyncPolicy.swift).
// Correm sem Xcode: `apple/tools/test.sh` compila este ficheiro com os modelos partilhados.

private var falhas = 0

private func check(_ cond: Bool, _ nome: String) {
    if cond { print("  ok   \(nome)") } else { falhas += 1; print("  FALHA \(nome)") }
}

/// Agente mínimo com o formato JSON do `/v1/state`; o modelo só se constrói pelo decoder.
private func agent(_ id: String, status: String, waiting: String? = nil, tool: String = "",
                   detail: String = "", lastReply: String? = nil, doneAt: String? = nil) -> Agent {
    var json: [String: Any] = ["id": id, "status": status, "activity": ["tool": tool, "detail": detail]]
    if let waiting { json["waiting"] = ["id": waiting, "kind": "decision", "title": "Continuar?"] }
    if let lastReply { json["lastReply"] = lastReply }
    if let doneAt { json["doneAt"] = doneAt }
    let data = try! JSONSerialization.data(withJSONObject: json)
    return try! JSONDecoder().decode(Agent.self, from: data)
}

private func testaUrgenciaDoiPhone() {
    print("iPhone → relógio: o que vai já (urgente) e o que se junta ao intervalo")
    let aTrabalhar = [agent("a", status: "running", tool: "Read")]
    check(WatchSyncPolicy.isUrgent(sent: nil, next: aTrabalhar), "primeiro envio é urgente")
    check(WatchSyncPolicy.isUrgent(sent: aTrabalhar, next: [agent("a", status: "idle")]),
          "running → idle (tarefa concluída) é urgente")
    check(WatchSyncPolicy.isUrgent(sent: aTrabalhar, next: [agent("a", status: "waiting", waiting: "p1")]),
          "running → waiting com pedido é urgente")
    check(WatchSyncPolicy.isUrgent(sent: aTrabalhar, next: [agent("a", status: "error")]),
          "running → error é urgente")
    check(WatchSyncPolicy.isUrgent(sent: [agent("a", status: "waiting", waiting: "p1")],
                                   next: [agent("a", status: "waiting", waiting: "p2")]),
          "pedido trocado é urgente")
    check(WatchSyncPolicy.isUrgent(sent: [agent("a", status: "idle")], next: [agent("a", status: "running")]),
          "idle → running (começou a trabalhar) é urgente")
    check(WatchSyncPolicy.isUrgent(sent: aTrabalhar, next: aTrabalhar + [agent("b", status: "idle")]),
          "agente que entra é urgente")
    check(WatchSyncPolicy.isUrgent(sent: aTrabalhar + [agent("b", status: "idle")], next: aTrabalhar),
          "agente que sai é urgente")
    check(!WatchSyncPolicy.isUrgent(sent: aTrabalhar, next: [agent("a", status: "running", tool: "Edit", detail: "x.go")]),
          "mesmo estado, só a ferramenta mudou: junta-se ao intervalo")
    check(!WatchSyncPolicy.isUrgent(sent: [agent("a", status: "idle")],
                                    next: [agent("a", status: "idle", lastReply: "feito")]),
          "mesmo estado, só a última resposta mudou: junta-se ao intervalo")
    check(!WatchSyncPolicy.isUrgent(sent: aTrabalhar, next: aTrabalhar), "estado igual não é urgente")
    check(WatchSyncPolicy.isUrgent(sent: [agent("a", status: "idle")],
                                   next: [agent("a", status: "idle", doneAt: "2026-11-02T18:00:00.000Z")]),
          "aviso «terminou» que chega depois do idle é urgente")
}

private func testaAlertaNoRelogio() {
    print("relógio: quando o pulso vibra")
    let agora = WatchSyncPolicy.parseDate("2026-11-02T18:05:00.000Z")!
    let fim1 = "2026-11-02T18:04:00.123Z", fim2 = "2026-11-02T18:04:30.000Z", velho = "2026-11-02T17:00:00.000Z"
    let nada: WatchAlertMemory? = nil
    var (vibra, mem) = WatchSyncPolicy.alert(after: nada, agents: [agent("a", status: "waiting", waiting: "p1")], now: agora)
    check(vibra, "primeira receção com pedido em espera vibra")
    (vibra, mem) = WatchSyncPolicy.alert(after: mem, agents: [agent("a", status: "waiting", waiting: "p1")], now: agora)
    check(!vibra, "o mesmo pedido outra vez não vibra")
    (vibra, mem) = WatchSyncPolicy.alert(after: mem, agents: [agent("a", status: "running", tool: "Read")], now: agora)
    check(!vibra, "pedido respondido, agente a trabalhar: não vibra")
    (vibra, mem) = WatchSyncPolicy.alert(after: mem, agents: [agent("a", status: "running", tool: "Edit")], now: agora)
    check(!vibra, "só a ferramenta mudou: não vibra")
    (vibra, mem) = WatchSyncPolicy.alert(after: mem, agents: [agent("a", status: "idle")], now: agora)
    check(!vibra, "running → idle sem aviso «terminou» (pessoa ao computador) não vibra")
    (vibra, mem) = WatchSyncPolicy.alert(after: mem, agents: [agent("a", status: "idle", doneAt: fim1)], now: agora)
    check(vibra, "aviso «terminou» novo vibra")
    (vibra, mem) = WatchSyncPolicy.alert(after: mem, agents: [agent("a", status: "idle", lastReply: "feito", doneAt: fim1)], now: agora)
    check(!vibra, "o mesmo aviso «terminou» outra vez não vibra")
    (vibra, mem) = WatchSyncPolicy.alert(after: mem, agents: [agent("a", status: "idle", doneAt: fim2)], now: agora)
    check(vibra, "aviso «terminou» seguinte vibra")
    (vibra, mem) = WatchSyncPolicy.alert(after: mem, agents: [agent("a", status: "idle", doneAt: fim2), agent("b", status: "starting")], now: agora)
    check(!vibra, "agente novo a arrancar não vibra")
    (vibra, mem) = WatchSyncPolicy.alert(after: mem, agents: [agent("a", status: "exited", doneAt: fim2)], now: agora)
    check(!vibra, "sessão fechada não vibra")
    check(mem.done == ["a": fim2], "a memória esquece os agentes que saíram")
    (vibra, _) = WatchSyncPolicy.alert(after: nada, agents: [agent("a", status: "idle", doneAt: fim1)], now: agora)
    check(vibra, "primeira receção com aviso «terminou» recente vibra (app acordado pela transferência)")
    (vibra, _) = WatchSyncPolicy.alert(after: nada, agents: [agent("a", status: "idle", doneAt: velho)], now: agora)
    check(!vibra, "aviso «terminou» com mais de 10 min não vibra")
    (vibra, mem) = WatchSyncPolicy.alert(after: nada, agents: [agent("a", status: "idle")], now: agora)
    check(!vibra, "primeira receção sem pedido nem aviso não vibra")
    (vibra, mem) = WatchSyncPolicy.alert(after: mem, agents: [agent("a", status: "waiting", waiting: "p9")], now: agora)
    check(vibra, "pedido novo vibra")
    check(mem.waiting == ["a"] && mem.status == ["a": "waiting"], "a memória guarda pedidos e estados atuais")
    check(WatchSyncPolicy.parseDate("2026-11-02T18:00:00Z") != nil, "lê a hora sem fração de segundo")
}

@main
struct WatchSyncPolicyTests {
    static func main() {
        testaUrgenciaDoiPhone()
        testaAlertaNoRelogio()
        if falhas > 0 { print("\(falhas) falha(s)"); exit(1) }
        print("todos os testes passaram")
    }
}
