//go:build !windows

package main

// O instalador por duplo clique só existe no Windows (ver instalador_windows.go).
func instaladorDuploClique() bool { return false }
