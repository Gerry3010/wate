package pty

import (
	"slices"
	"testing"
)

func TestHiddenVariablesDoNotReachShells(t *testing.T) {
	t.Setenv("WATE_HIDDEN_TEST", "1")
	t.Setenv("WATE_KEPT_TEST", "1")
	before := hiddenFromShells
	t.Cleanup(func() { hiddenFromShells = before })
	HideFromShells("WATE_HIDDEN_TEST")

	env := shellEnvironment()
	if slices.Contains(env, "WATE_HIDDEN_TEST=1") {
		t.Error("a hidden variable reached the shell environment")
	}
	if !slices.Contains(env, "WATE_KEPT_TEST=1") {
		t.Error("hiding one variable dropped another")
	}
}
