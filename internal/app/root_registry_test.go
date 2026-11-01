package app

import "testing"

func TestThereIsOneRootAndOnlyItsTokenCounts(t *testing.T) {
	p := panesIn(t, map[string]string{"root": "t1", "other": "t2"})
	p.paneOfToken["tok-root"], p.tokenOfPane["root"] = "root", "tok-root"
	p.paneOfToken["tok-other"], p.tokenOfPane["other"] = "other", "tok-other"

	if p.IsRootToken("tok-root") {
		t.Fatal("a root token before any root was claimed")
	}
	if err := p.claimRoot("root"); err != nil {
		t.Fatal(err)
	}
	if err := p.claimRoot("root"); err != nil {
		t.Fatalf("the same pane spawning again: %v", err)
	}
	if err := p.claimRoot("other"); err == nil {
		t.Fatal("a second root was let in")
	}
	if !p.IsRootToken("tok-root") || p.IsRootToken("tok-other") || p.IsRootToken("") || p.IsRootToken("root") {
		t.Fatal("token check: only the root pane's token may pass, never a pane id")
	}
	p.releaseRoot("other")
	if p.RootPane() != "root" {
		t.Fatal("another pane released the root's seat")
	}
	p.releaseRoot("root")
	if p.RootPane() != "" || p.IsRootToken("tok-root") {
		t.Fatal("seat still taken after release")
	}
}
