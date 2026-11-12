//go:build !windows

package hook

import "time"

// No macOS e no Linux, a inatividade vem do ioreg e do xprintidle (ver presenca.go).
func inatividadeSO() (time.Duration, bool) { return 0, false }
