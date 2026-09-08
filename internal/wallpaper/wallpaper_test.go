package wallpaper

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestServe(t *testing.T) {
	dir := t.TempDir()
	img := filepath.Join(dir, "w.png")
	os.WriteFile(img, []byte("\x89PNG fake"), 0o644)
	os.WriteFile(filepath.Join(dir, "w.txt"), []byte("no"), 0o644)

	cases := map[string]int{img: 200, filepath.Join(dir, "w.txt"): 415, "": 404, filepath.Join(dir, "missing.png"): 404}
	for p, want := range cases {
		h := &Handler{Path: func() string { return p }}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/wallpaper", nil))
		if rec.Code != want {
			t.Errorf("%q: status %d, want %d", p, rec.Code, want)
		}
	}
}
