//go:build !windows

package instalar

// O atalho do menu Iniciar só existe no Windows (ver atalho_windows.go).
func instalarAtalhoWindows(string) (string, error) { return "", nil }
func removerAtalhoWindows()                        {}
