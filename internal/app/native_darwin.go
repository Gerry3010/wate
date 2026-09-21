package app

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>

// The frosted glass behind a translucent window is drawn by AppKit, not by us, and it takes its
// brightness from the window's NSAppearance — not from anything the web view paints. Left alone
// that follows the system setting, so a dark wate theme on a light Mac got a milky white pane.
static void wate_prefer_dark(int dark) {
	NSAppearanceName name = dark ? NSAppearanceNameDarkAqua : NSAppearanceNameAqua;
	[NSApplication sharedApplication].appearance = [NSAppearance appearanceNamed:name];
}
*/
import "C"

// setPreferDark points AppKit's appearance at the one the wate theme asks for, which is what
// decides how dark the translucent backdrop is drawn.
func setPreferDark(dark bool) {
	d := C.int(0)
	if dark {
		d = 1
	}
	C.wate_prefer_dark(d)
}

// Translucency is set when the window is built (see WindowOptions); nothing to do afterwards.
func setWindowTransparent() {}
