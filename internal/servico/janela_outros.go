//go:build !windows

package servico

// focarJanelaSO só existe no Windows; no macOS foca-se pelo app e no Linux pelo xdotool.
func focarJanelaSO([]int) bool { return false }
