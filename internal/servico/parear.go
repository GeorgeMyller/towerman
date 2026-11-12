package servico

import (
	"html"
	"net/http"
	"strconv"
	"strings"
	"time"

	"rsc.io/qr"
)

// QR desenha o código no terminal com meios-blocos: duas linhas de módulos por linha de texto,
// sobre fundo branco, para ler com a câmara do iPhone em qualquer tema do terminal.
func QR(texto string) (string, error) {
	c, err := qr.Encode(texto, qr.M)
	if err != nil {
		return "", err
	}
	const margem = 2
	preto := func(x, y int) bool {
		x, y = x-margem, y-margem
		return x >= 0 && y >= 0 && x < c.Size && y < c.Size && c.Black(x, y)
	}
	var b strings.Builder
	n := c.Size + 2*margem
	for y := 0; y < n; y += 2 {
		b.WriteString("\x1b[30;47m")
		for x := 0; x < n; x++ {
			cima, baixo := preto(x, y), preto(x, y+1)
			switch {
			case cima && baixo:
				b.WriteString("█")
			case cima:
				b.WriteString("▀")
			case baixo:
				b.WriteString("▄")
			default:
				b.WriteString(" ")
			}
		}
		b.WriteString("\x1b[0m\n")
	}
	return b.String(), nil
}

func (s *Servico) qrPareamento(w http.ResponseWriter, r *http.Request) {
	if s.PortaRede() == 0 {
		http.Error(w, "porta de rede desligada", http.StatusConflict)
		return
	}
	codigo, _ := s.aparelhos.novoCodigo(timeAgora())
	c, err := qr.Encode(s.urlPareamento(codigo), qr.M)
	if err != nil {
		http.Error(w, "qr", http.StatusInternalServerError)
		return
	}
	c.Scale = 8
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(c.PNG())
}

// paginaParear mostra o QR numa página própria (o deck usa um diálogo com o mesmo QR). A janela
// do deck não tem barra do browser, por isso a página tem sempre «Voltar ao deck» e volta sozinha
// quando aparece um aparelho novo em /v1/aparelhos.
func (s *Servico) paginaParear(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	strings.NewReplacer("{{NOME}}", html.EscapeString(nomeMaquina()), "{{T}}", strconv.FormatInt(timeAgora().UnixNano(), 10)).
		WriteString(w, paginaParearHTML)
}

const paginaParearHTML = `<!doctype html><html lang="pt-BR"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="dark"><title>Towerman · Parear iPhone</title>
<style>*{box-sizing:border-box}body{margin:0;min-height:100vh;display:grid;place-items:center;background:#0b1626;color:#e3ebf5;font:15px/1.5 system-ui,-apple-system,"Segoe UI",Ubuntu,sans-serif}
main{text-align:center;width:100%;max-width:440px;padding:24px 16px}
.qr{background:#fff;padding:12px;border-radius:14px;width:min(324px,100%);aspect-ratio:1;margin:0 auto;display:block}
.qr img{width:100%;height:100%;image-rendering:pixelated;display:block}.qr.off img{opacity:.08}
h1{font-size:20px;margin:0 0 6px}p{color:#8ea3c0;margin:10px 0}#st.ok{color:#5bbf8a;font-weight:600}#st.err{color:#f0645a}
.acts{display:flex;gap:8px;justify-content:center;flex-wrap:wrap;margin-top:14px}
.btn{display:inline-flex;align-items:center;min-height:40px;padding:6px 14px;border-radius:8px;border:1px solid #26405f;background:#12223a;color:#e3ebf5;text-decoration:none;font:inherit;cursor:pointer}
.btn:hover{border-color:#5aa9ff}.btn.blue{background:#5aa9ff;border-color:#5aa9ff;color:#0b1626;font-weight:600}
:focus-visible{outline:2px solid #5aa9ff;outline-offset:2px}</style></head>
<body><main><h1>Parear o iPhone com {{NOME}}</h1>
<p>No iPhone, aponte a câmara (ou o Towerman › Adicionar computador) para o código. O iPhone tem de estar na mesma rede Wi-Fi.</p>
<div class="qr" id="qr"><img id="img" src="/v1/pareamento/qr.png?t={{T}}" alt="Código QR de pareamento"></div>
<p id="st" role="status">O código vale 2 minutos e só serve uma vez.</p>
<div class="acts"><a class="btn blue" href="/deck" id="voltar">← Voltar ao deck</a><a class="btn" href="/parear">Gerar outro código</a></div>
</main>
<script>
"use strict";
var conhecidos = null, feito = false, st = document.getElementById("st");
document.getElementById("img").onerror = function () {
  document.getElementById("qr").className = "qr off";
  st.className = "err";
  st.textContent = "Não foi possível gerar o código (a porta de rede pode estar desligada).";
};
document.addEventListener("keydown", function (e) { if (e.key === "Escape") location.href = "/deck"; });
function sondar() {
  if (feito) return;
  fetch("/v1/aparelhos", { credentials: "same-origin", cache: "no-store" }).then(function (r) { return r.ok ? r.json() : null; }).then(function (l) {
    if (!Array.isArray(l)) return;
    if (conhecidos === null) { conhecidos = {}; l.forEach(function (a) { conhecidos[a.id] = 1; }); return; }
    var novo = l.filter(function (a) { return !conhecidos[a.id]; })[0];
    if (!novo) return;
    feito = true;
    document.getElementById("qr").className = "qr off";
    st.className = "ok";
    st.textContent = "Pareado: " + (novo.nome || "iPhone") + ". A voltar ao deck…";
    setTimeout(function () { location.href = "/deck"; }, 2500);
  }).catch(function () {});
}
sondar();
setInterval(sondar, 2500);
setTimeout(function () { if (!feito) { document.getElementById("qr").className = "qr off"; st.className = ""; st.textContent = "O código expirou. Gere outro."; } }, 120000);
</script></body></html>`

var timeAgora = func() time.Time { return time.Now() }
