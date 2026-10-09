package app

import (
	"os"
	"runtime"

	"github.com/Gerry3010/wate/internal/pty"
)

// PreferStableRenderer picks GTK's OpenGL renderer on Linux. It has to run before the
// toolkit starts, because GSK reads this when it creates the first surface.
//
// GTK 4.22 defaults to Vulkan, and two of the three paths that import WebKit's dma-buf
// frames into GDK leak a little memory per frame and never give it back. Measured against
// a window drawing continuously under the same load: Vulkan grew the main process' glibc
// heap by 76 kB/s and Cairo by 113 kB/s, where OpenGL grew by nothing at all — and did it
// with a quarter of the CPU. Unattended, that is gigabytes over a few days of uptime, which
// is what put this machine into swap.
//
// Setting GSK_RENDERER yourself still wins; this only fills in a default.
func PreferStableRenderer() {
	if runtime.GOOS != "linux" {
		return
	}
	if _, set := os.LookupEnv("GSK_RENDERER"); set {
		return
	}
	if err := os.Setenv("GSK_RENDERER", "gl"); err != nil {
		return
	}
	pty.HideFromShells("GSK_RENDERER")
}
