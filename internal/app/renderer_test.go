package app

import (
	"os"
	"runtime"
	"testing"
)

func TestPreferStableRendererFillsInADefault(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("GSK_RENDERER is a GTK setting")
	}
	t.Setenv("GSK_RENDERER", "")
	os.Unsetenv("GSK_RENDERER")
	PreferStableRenderer()
	if got := os.Getenv("GSK_RENDERER"); got != "gl" {
		t.Fatalf("GSK_RENDERER = %q, want gl", got)
	}
}

func TestPreferStableRendererLeavesAChoiceAlone(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("GSK_RENDERER is a GTK setting")
	}
	t.Setenv("GSK_RENDERER", "vulkan")
	PreferStableRenderer()
	if got := os.Getenv("GSK_RENDERER"); got != "vulkan" {
		t.Fatalf("GSK_RENDERER = %q, want the value that was already there", got)
	}
}
