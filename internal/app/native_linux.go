//go:build linux

package app

/*
#cgo pkg-config: gtk4
#include <gtk/gtk.h>

static void wate_prefer_dark(int dark) {
	GtkSettings *s = gtk_settings_get_default();
	if (s != NULL) {
		g_object_set(G_OBJECT(s), "gtk-application-prefer-dark-theme", dark ? TRUE : FALSE, NULL);
	}
}
*/
import "C"

// setPreferDark tells GTK to draw its own chrome (the CSD title bar) in the dark variant.
// Must run on the GTK main thread.
func setPreferDark(dark bool) {
	v := 0
	if dark {
		v = 1
	}
	C.wate_prefer_dark(C.int(v))
}
