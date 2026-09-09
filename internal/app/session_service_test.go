package app

import (
	"os"
	"testing"
)

func TestSessionServiceRoundTrip(t *testing.T) {
	t.Setenv("WATE_CONFIG_DIR", t.TempDir())
	var s SessionService
	id, err := s.Save("Work: Chattr & Co", `{"version":1,"active":0,"tabs":[{"tree":{"kind":"leaf","id":"p1"},"focused":"p1","panes":[{"id":"p1","kind":"terminal","cwd":"/tmp"}]}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if id != "work-chattr-co" {
		t.Errorf("id %q", id)
	}
	list := s.List()
	if len(list) != 1 || list[0].Name != "Work: Chattr & Co" || list[0].Tabs != 1 || list[0].Panes != 1 {
		t.Errorf("list: %+v", list)
	}
	got, err := s.Load(id)
	if err != nil || got == "" {
		t.Fatalf("load: %v %q", err, got)
	}
	if _, err := s.Save("", "{}"); err == nil {
		t.Error("empty name should fail")
	}
	if _, err := s.Save("x", "{nope"); err == nil {
		t.Error("invalid json should fail")
	}
	if err := s.Delete(id); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sessionsDir() + "/" + id + ".json"); !os.IsNotExist(err) {
		t.Error("session file still exists")
	}
}
