"use strict";
// Cada janela do IDE liga-se ao towerman serve e só responde aos pedidos cujo terminal é seu.
const vscode = require("vscode");
const http = require("http");
const fs = require("fs");
const os = require("os");
const path = require("path");
const crypto = require("crypto");

const home = process.env.TOWERMAN_HOME || path.join(os.homedir(), ".claude", "towerman");
const porta = Number(process.env.TOWERMAN_PORTA) || 7777;
let ativo = true;
let pedidoAtual = null;
// janela identifica esta janela nos pedidos em duas fases; ultimoExecutado evita agir duas vezes.
let janela = "";
let ultimoExecutado = "";

function token() {
  try { return fs.readFileSync(path.join(home, "token"), "utf8").trim(); } catch (e) { return ""; }
}

function responder(corpo) {
  const dados = Buffer.from(JSON.stringify(corpo));
  const req = http.request({ host: "127.0.0.1", port: porta, method: "POST", path: "/v1/extensao/resposta",
    headers: { "Authorization": "Bearer " + token(), "Content-Type": "application/json", "Content-Length": dados.length } });
  req.on("error", () => {});
  req.end(dados);
}

async function terminalDe(pids) {
  for (const t of vscode.window.terminals) {
    const pid = await t.processId;
    if (pid && pids.includes(pid)) return t;
  }
  return null;
}

// A janela «dona» de uma pasta é a que tem aberta a pasta mais próxima dela.
function donaDe(cwd) {
  let melhor = -1;
  for (const f of vscode.workspace.workspaceFolders || []) {
    const raiz = f.uri.fsPath.replace(/[\\/]+$/, "");
    if (cwd === raiz || cwd.startsWith(raiz + path.sep)) melhor = Math.max(melhor, raiz.length);
  }
  return melhor;
}

// pasta é o que, aberto no IDE, traz esta janela para a frente: o ficheiro do workspace ou a
// única pasta. No Mac, o serviço abre-a com «open -a», que funciona noutro Space. Um workspace
// sem título com várias pastas não tem caminho que reabra esta janela: abriria uma nova.
function pasta() {
  const f = vscode.workspace.workspaceFile;
  if (f) return f.scheme === "file" ? f.fsPath : "";
  const pastas = vscode.workspace.workspaceFolders || [];
  return pastas.length === 1 ? pastas[0].uri.fsPath : "";
}

async function tratar(p) {
  if (p.acao === "abrir") {
    if (donaDe(p.cwd || "") < 0) return;
    const t = vscode.window.createTerminal({ name: "Towerman", cwd: p.cwd });
    t.show(false);
    t.sendText(p.texto, true);
    responder({ pedido: p.id, ok: true, workspace: vscode.workspace.name || "" });
    return;
  }
  if (p.acao === "foco") {
    // Responde a janela com o terminal da sessão; sem ele (agente do IDE), a dona da pasta.
    // Uma dona sem foco espera um pouco, para uma dona focada responder primeiro.
    const focada = vscode.window.state.focused;
    if (!(await terminalDe(p.pids || []))) {
      if (donaDe(p.cwd || "") < 0) return;
      if (!focada) await new Promise((r) => setTimeout(r, 200));
    }
    responder({ pedido: p.id, ok: true, workspace: vscode.workspace.name || "", focada });
    return;
  }
  if (p.acao === "painel") {
    await painel(p);
    return;
  }
  if (p.acao !== "enviar" && p.acao !== "focar") return;
  if (p.fase === "candidatar") {
    const c = await candidatura(p);
    if (c) responder({ pedido: p.id, ok: true, workspace: vscode.workspace.name || "", janela, alcance: c.alcance });
    return;
  }
  if (p.fase === "executar") {
    if (p.janela !== janela || p.id === ultimoExecutado) return;
    ultimoExecutado = p.id;
    const c = await candidatura(p);
    if (!c) return;
    agir(p, c.terminal);
    responder({ pedido: p.id, ok: true, workspace: vscode.workspace.name || "", pasta: pasta(), janela });
    return;
  }
  // Serviço antigo (sem fase): só a janela com o terminal da sessão age.
  const t = await terminalDe(p.pids || []);
  if (!t) return;
  agir(p, t);
  responder({ pedido: p.id, ok: true, workspace: vscode.workspace.name || "", pasta: pasta() });
}

// candidatura diz quanto esta janela serve para o pedido: o terminal da sessão ganha sempre.
// «enviar» só escreve nesse terminal: o terminal ativo pode ser uma shell, e o texto correria
// como comando (R-01). Sem ele, só «focar» usa a janela dona da pasta e o terminal ativo.
async function candidatura(p) {
  const t = await terminalDe(p.pids || []);
  if (t) return { alcance: Number.MAX_VALUE, terminal: t };
  if (p.acao === "enviar") return null;
  const alcance = donaDe(p.cwd || "");
  if (alcance < 0) return null;
  return { alcance, terminal: vscode.window.activeTerminal };
}

// O agente do painel do Antigravity IDE não tem terminal: a mensagem entra pelo comando do IDE
// que a envia à conversa aberta no painel. Responde a janela dona da pasta da sessão.
const comandoPainel = "antigravity.sendPromptToAgentPanel";
let temPainel = null;

async function painel(p) {
  if (temPainel === null) temPainel = (await vscode.commands.getCommands(true)).includes(comandoPainel);
  if (!temPainel) return;
  const alcance = donaDe(p.cwd || "");
  if (alcance < 0) return;
  if (p.fase === "candidatar") {
    responder({ pedido: p.id, ok: true, workspace: vscode.workspace.name || "", janela, alcance });
    return;
  }
  if (p.fase !== "executar" || p.janela !== janela || p.id === ultimoExecutado) return;
  ultimoExecutado = p.id;
  await vscode.commands.executeCommand(comandoPainel, p.texto);
  responder({ pedido: p.id, ok: true, workspace: vscode.workspace.name || "", janela });
}

function agir(p, t) {
  if (p.acao === "enviar") t.sendText(p.texto, true);
  else if (t) t.show(false);
}

function ligar() {
  if (!ativo) return;
  const t = token();
  if (!t) { setTimeout(ligar, 10000); return; }
  const req = http.get({ host: "127.0.0.1", port: porta, path: "/v1/extensao/eventos",
    headers: { "Authorization": "Bearer " + t, "Accept": "text/event-stream" } }, (res) => {
    if (res.statusCode !== 200) { res.resume(); setTimeout(ligar, 10000); return; }
    res.setEncoding("utf8");
    let resto = "";
    res.on("data", (pedaco) => {
      resto += pedaco;
      let fim;
      while ((fim = resto.indexOf("\n\n")) >= 0) {
        const bloco = resto.slice(0, fim);
        resto = resto.slice(fim + 2);
        const linha = bloco.split("\n").find((l) => l.startsWith("data: "));
        if (bloco.includes("event: pedido") && linha) {
          try { tratar(JSON.parse(linha.slice(6))); } catch (e) {}
        }
      }
    });
    res.on("end", () => setTimeout(ligar, 3000));
    res.on("error", () => setTimeout(ligar, 3000));
  });
  req.on("error", () => setTimeout(ligar, 3000));
  pedidoAtual = req;
}

function activate() { janela = crypto.randomBytes(8).toString("hex"); ligar(); }
function deactivate() { ativo = false; if (pedidoAtual) pedidoAtual.destroy(); }

module.exports = { activate, deactivate };
