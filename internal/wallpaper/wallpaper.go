// Package wallpaper serves the configured background image to the webview.
package wallpaper

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Handler serves the image returned by Path at its mount route.
type Handler struct {
	Path func() string
}

var allowedExt = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".webp": true, ".gif": true, ".avif": true, ".bmp": true}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := h.Path()
	if p == "" {
		http.NotFound(w, r)
		return
	}
	if !allowedExt[strings.ToLower(filepath.Ext(p))] {
		http.Error(w, "unsupported image type", http.StatusUnsupportedMediaType)
		return
	}
	f, err := os.Open(p)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=60")
	http.ServeContent(w, r, filepath.Base(p), st.ModTime(), f)
}
