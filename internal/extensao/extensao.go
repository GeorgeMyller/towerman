// Package extensao empacota a extensão do Towerman para IDEs da família VS Code e instala-a
// pelo CLI de cada IDE encontrado (code, antigravity-ide, cursor, codium).
package extensao

import (
	"archive/zip"
	"bytes"
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

//go:embed ficheiros/package.json ficheiros/extension.js ficheiros/README.md
var ficheiros embed.FS

// Publicador e nome visível no IDE. O ID não aceita espaços, por isso o nome vai à parte.
const (
	Publicador     = "georgemyller"
	NomePublicador = "George Myller"
	ID             = Publicador + ".towerman"
)

// idsAntigos são extensões do Towerman publicadas com outro ID; o install remove-as, para o
// IDE não ficar com duas.
var idsAntigos = []string{"vlm.towerman"}

const tiposConteudo = `<?xml version="1.0" encoding="utf-8"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension=".json" ContentType="application/json"/><Default Extension=".md" ContentType="text/markdown"/><Default Extension=".js" ContentType="application/javascript"/><Default Extension=".vsixmanifest" ContentType="text/xml"/></Types>`

const manifesto = `<?xml version="1.0" encoding="utf-8"?>
<PackageManifest Version="2.0.0" xmlns="http://schemas.microsoft.com/developer/vsx-schema/2011" xmlns:d="http://schemas.microsoft.com/developer/vsx-schema-design/2011">
  <Metadata>
    <Identity Language="en-US" Id="towerman" Version="%s" Publisher="%s"/>
    <DisplayName>Towerman</DisplayName>
    <Description xml:space="preserve">Liga os terminais do IDE ao Towerman.</Description>
    <Categories>Other</Categories>
    <Properties>
      <Property Id="Microsoft.VisualStudio.Code.Engine" Value="^1.80.0"/>
      <Property Id="Microsoft.VisualStudio.Code.ExtensionKind" Value="ui,workspace"/>
    </Properties>
  </Metadata>
  <Installation><InstallationTarget Id="Microsoft.VisualStudio.Code"/></Installation>
  <Dependencies/>
  <Assets>
    <Asset Type="Microsoft.VisualStudio.Code.Manifest" Path="extension/package.json" Addressable="true"/>
    <Asset Type="Microsoft.VisualStudio.Services.Content.Details" Path="extension/README.md" Addressable="true"/>
  </Assets>
</PackageManifest>`

func Versao() string {
	b, _ := ficheiros.ReadFile("ficheiros/package.json")
	i := bytes.Index(b, []byte(`"version": "`))
	if i < 0 {
		return "0.0.0"
	}
	resto := b[i+len(`"version": "`):]
	return string(resto[:bytes.IndexByte(resto, '"')])
}

// Empacotar escreve um .vsix e devolve o caminho.
func Empacotar(dir string) (string, error) {
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	escrever := func(nome string, dados []byte) error {
		w, err := z.Create(nome)
		if err != nil {
			return err
		}
		_, err = w.Write(dados)
		return err
	}
	if err := escrever("[Content_Types].xml", []byte(tiposConteudo)); err != nil {
		return "", err
	}
	if err := escrever("extension.vsixmanifest", []byte(fmt.Sprintf(manifesto, Versao(), Publicador))); err != nil {
		return "", err
	}
	for _, n := range []string{"package.json", "extension.js", "README.md"} {
		b, _ := ficheiros.ReadFile("ficheiros/" + n)
		if err := escrever("extension/"+n, b); err != nil {
			return "", err
		}
	}
	if err := z.Close(); err != nil {
		return "", err
	}
	p := filepath.Join(dir, "towerman-"+Versao()+".vsix")
	return p, os.WriteFile(p, buf.Bytes(), 0o644)
}

// clisDeIDE devolve os CLIs de IDEs da família VS Code. Só aceita scripts do CLI do VS Code
// (que correm em modo linha de comando), nunca executáveis que abririam uma janela.
func clisDeIDE() []string {
	home, _ := os.UserHomeDir()
	candidatos := []string{"code", "antigravity-ide", "cursor", "codium"}
	extras := []string{
		filepath.Join(home, ".local", "share", "antigravity-ide", "bin", "antigravity-ide"),
		filepath.Join(home, ".antigravity-ide", "antigravity-ide", "bin", "antigravity-ide"),
		"/usr/share/code/bin/code",
	}
	if runtime.GOOS == "darwin" {
		// O CLI dentro do pacote da app, para quem não o pôs no PATH.
		app := filepath.Join("Antigravity IDE.app", "Contents", "Resources", "app", "bin", "antigravity-ide")
		extras = append(extras, filepath.Join("/Applications", app), filepath.Join(home, "Applications", app))
	}
	vistos := map[string]bool{}
	var r []string
	aceitar := func(p string) {
		real, err := filepath.EvalSymlinks(p)
		if err != nil || vistos[real] {
			return
		}
		b, err := os.ReadFile(real)
		if err != nil || len(b) > 64*1024 || !bytes.Contains(b, []byte("ELECTRON_RUN_AS_NODE")) {
			return
		}
		vistos[real] = true
		r = append(r, real)
	}
	for _, c := range candidatos {
		if p, err := exec.LookPath(c); err == nil {
			aceitar(p)
		}
	}
	for _, p := range extras {
		aceitar(p)
	}
	return r
}

type Resultado struct {
	IDE  string
	Erro error
}

func nomeIDE(cli string) string { return strings.TrimSuffix(filepath.Base(cli), ".cmd") }

func Instalar(tmp string) ([]Resultado, error) {
	clis := clisDeIDE()
	if len(clis) == 0 {
		return nil, nil
	}
	vsix, err := Empacotar(tmp)
	if err != nil {
		return nil, err
	}
	var r []Resultado
	for _, c := range clis {
		out, err := exec.Command(c, "--install-extension", vsix, "--force").CombinedOutput()
		if err != nil {
			err = fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
		}
		r = append(r, Resultado{nomeIDE(c), err})
		if err == nil {
			for _, antigo := range idsAntigos {
				desinstalar(c, antigo)
			}
		}
	}
	return r, nil
}

// desinstalar remove a extensão id pelo CLI c; uma extensão que não está instalada não é erro.
func desinstalar(c, id string) error {
	out, err := exec.Command(c, "--uninstall-extension", id).CombinedOutput()
	if err != nil && strings.Contains(string(out), "is not installed") {
		return nil
	}
	if err != nil {
		err = fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return err
}

func Remover() []Resultado {
	var r []Resultado
	for _, c := range clisDeIDE() {
		for _, antigo := range idsAntigos {
			desinstalar(c, antigo)
		}
		out, err := exec.Command(c, "--uninstall-extension", ID).CombinedOutput()
		if err != nil && strings.Contains(string(out), "is not installed") {
			continue
		}
		if err != nil {
			err = fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
		}
		r = append(r, Resultado{nomeIDE(c), err})
	}
	return r
}
