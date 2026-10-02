package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// A v1 session.json holds the frontend's blob directly. Upgrading must hand that blob back,
// not drop it — it is the user's tabs.
func TestSessionV1IsMigratedNotDiscarded(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WATE_CONFIG_DIR", dir)
	v1 := `{"version":1,"active":1,"tabs":[{"panes":[]},{"panes":[]}]}`
	if err := os.WriteFile(filepath.Join(dir, "session.json"), []byte(v1), 0o600); err != nil {
		t.Fatal(err)
	}

	blobs := (&StateService{}).LoadAll()
	if len(blobs) != 1 {
		t.Fatalf("want one window, got %d", len(blobs))
	}
	if blobs[0].Data != v1 {
		t.Fatalf("the v1 blob was altered:\n got %s\nwant %s", blobs[0].Data, v1)
	}
}

func TestSessionSlotsStayApart(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WATE_CONFIG_DIR", dir)

	// Save() needs a window from the context; go straight at the file instead.
	if err := writeState(stateFile{Version: 2, Windows: []WindowBlob{
		{Key: "w-1", Data: `{"one":true}`},
		{Key: "w-2", Data: `{"two":true}`},
	}}); err != nil {
		t.Fatal(err)
	}
	s := &StateService{}
	got := s.LoadAll()
	if len(got) != 2 || got[0].Data != `{"one":true}` || got[1].Data != `{"two":true}` {
		t.Fatalf("slots did not survive: %+v", got)
	}

	if err := s.Prune([]string{"w-2"}); err != nil {
		t.Fatal(err)
	}
	if got = s.LoadAll(); len(got) != 1 || got[0].Key != "w-2" {
		t.Fatalf("prune kept the wrong slots: %+v", got)
	}
}

// A v1 window.json is a bare WindowState. It has no version field, so a zero one after
// unmarshalling is what tells the two formats apart.
func TestWindowGeometryV1IsMigrated(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WATE_CONFIG_DIR", dir)
	v1, _ := json.Marshal(WindowState{X: 10, Y: 20, Width: 1200, Height: 800})
	if err := os.WriteFile(filepath.Join(dir, "window.json"), v1, 0o600); err != nil {
		t.Fatal(err)
	}

	st, ok := LoadWindowState()
	if !ok || st.Width != 1200 || st.X != 10 {
		t.Fatalf("v1 geometry lost: %+v ok=%v", st, ok)
	}
	if st, ok = LoadWindowStateFor("w-1"); !ok || st.Height != 800 {
		t.Fatalf("migrated record is not keyed w-1: %+v ok=%v", st, ok)
	}
}

func TestWindowGeometryPerWindow(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WATE_CONFIG_DIR", dir)

	geometry.put("w-1", WindowState{X: 1, Y: 1, Width: 900, Height: 600})
	geometry.put("w-2", WindowState{X: 2, Y: 2, Width: 1400, Height: 900})
	geometry.put("w-1", WindowState{X: 3, Y: 3, Width: 1000, Height: 700})

	a, okA := LoadWindowStateFor("w-1")
	b, okB := LoadWindowStateFor("w-2")
	if !okA || a.Width != 1000 || a.X != 3 {
		t.Fatalf("w-1 not updated in place: %+v", a)
	}
	if !okB || b.Width != 1400 {
		t.Fatalf("w-2 was clobbered by w-1's write: %+v", b)
	}
}

func TestUnusableGeometryIsIgnored(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WATE_CONFIG_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "window.json"), []byte(`{"width":10,"height":10}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := LoadWindowState(); ok {
		t.Fatal("a 10x10 window should not be restored")
	}
}
