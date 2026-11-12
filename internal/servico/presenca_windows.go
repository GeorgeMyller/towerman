//go:build windows

package servico

// presencaViva: no Windows não há flock para sondar a marca do Antigravity; vale o PID guardado.
func presencaViva(dir, sessionID string) estadoPresenca { return marcaDesconhecida }
