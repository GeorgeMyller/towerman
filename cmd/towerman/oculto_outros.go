//go:build !windows

package main

// relancarSemConsola só faz sentido no Windows; nos outros sistemas, o serve corre como está.
func relancarSemConsola([]string) bool { return false }
