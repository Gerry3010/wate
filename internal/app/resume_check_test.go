package app

import (
	"sync"
	"testing"
	"time"
)

func TestAResumeThatDoesNotComeUpIsOfferedAgain(t *testing.T) {
	fast := resumeTiming{Within: 60 * time.Millisecond, Settle: 30 * time.Millisecond, Every: 5 * time.Millisecond}
	start := time.Now()
	running := func(pane string) int {
		el := time.Since(start)
		switch pane {
		case "ok": // comes up after a moment and stays
			if el > 10*time.Millisecond {
				return 1
			}
		case "flash": // comes up, then ends at once
			if el > 10*time.Millisecond && el < 20*time.Millisecond {
				return 1
			}
		}
		return 0 // "never": nothing ever starts
	}
	var mu sync.Mutex
	var failed []string
	confirmResumes([]resumedSession{{Pane: "ok"}, {Pane: "flash"}, {Pane: "never"}}, running, fast, func(r resumedSession) {
		mu.Lock()
		failed = append(failed, r.Pane)
		mu.Unlock()
	})
	if len(failed) != 2 || !contains(failed, "flash") || !contains(failed, "never") {
		t.Fatalf("failed: %v, want flash and never", failed)
	}
}
