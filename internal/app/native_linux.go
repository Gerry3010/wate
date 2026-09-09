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

// GTK4 paints an opaque theme background behind the WebView; for a see-through window it
// must go. The CSS provider only affects this application.
static void wate_transparent_window(void) {
	GtkCssProvider *p = gtk_css_provider_new();
	gtk_css_provider_load_from_string(p, "window.background { background-color: transparent; }");
	gtk_style_context_add_provider_for_display(gdk_display_get_default(), GTK_STYLE_PROVIDER(p),
		GTK_STYLE_PROVIDER_PRIORITY_APPLICATION);
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

// setWindowTransparent makes the toolkit window background see-through (main thread only).
func setWindowTransparent() { C.wate_transparent_window() }
