package app

import (
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// ask without a window falls back to an app-wide broadcast, and there is no application in a
// unit test — so these drive the registry directly, which is the part with the concurrency in it.

// oneID returns the single pending question's id, waiting for it to appear.
func oneID(t *testing.T, b *PaneBridge) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		b.mu.Lock()
		for id := range b.waiting {
			b.mu.Unlock()
			return id
		}
		b.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	t.Fatal("no question was registered")
	return ""
}

func TestReplyReachesTheCaller(t *testing.T) {
	b := NewPaneBridge()
	done := make(chan struct{})
	var got string
	var err error
	go func() {
		got, err = b.ask(nil, "read", "pane-1", "{}")
		close(done)
	}()
	b.Reply(oneID(t, b), "hello", "")
	<-done
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	if got != "hello" {
		t.Fatalf("got %q, want %q", got, "hello")
	}
}

func TestAnErrorReplyBecomesAnError(t *testing.T) {
	b := NewPaneBridge()
	done := make(chan struct{})
	var err error
	go func() {
		_, err = b.ask(nil, "read", "pane-1", "{}")
		close(done)
	}()
	b.Reply(oneID(t, b), "", "no such pane")
	<-done
	if err == nil || err.Error() != "no such pane" {
		t.Fatalf("err = %v, want \"no such pane\"", err)
	}
}

func TestAnUnansweredQuestionTimesOutAndIsForgotten(t *testing.T) {
	b := NewPaneBridge()
	start := time.Now()
	_, err := b.ask(nil, "read", "pane-1", "{}")
	if err == nil || !strings.Contains(err.Error(), "did not answer") {
		t.Fatalf("err = %v, want a timeout", err)
	}
	// Comfortably under the ctl client's five seconds: that is the whole point of the budget.
	if d := time.Since(start); d > 4*time.Second {
		t.Fatalf("waited %v, which leaves the caller no room", d)
	}
	b.mu.Lock()
	left := len(b.waiting)
	b.mu.Unlock()
	if left != 0 {
		t.Fatalf("%d questions left behind after a timeout", left)
	}
}

func TestALateReplyIsDroppedQuietly(t *testing.T) {
	b := NewPaneBridge()
	// Nobody is waiting: this must not block, panic or leave anything behind.
	b.Reply("nobody-is-waiting", "data", "")
	b.mu.Lock()
	left := len(b.waiting)
	b.mu.Unlock()
	if left != 0 {
		t.Fatalf("a stray reply registered %d entries", left)
	}
}

func TestQuestionsDoNotMixThemselvesUp(t *testing.T) {
	const n = 50
	b := NewPaneBridge()
	ids := make([]string, 0, n)
	answers := make([]string, n)
	var wg sync.WaitGroup

	// Register the questions one at a time so each id can be matched to its expected answer;
	// the replies then all go out at once, from their own goroutines.
	for i := 0; i < n; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := b.ask(nil, "read", "pane", "{}")
			if err != nil {
				t.Errorf("ask %d: %v", i, err)
				return
			}
			answers[i] = got
		}()
		ids = append(ids, waitForNew(t, b, ids))
	}

	var replies sync.WaitGroup
	for i, id := range ids {
		i, id := i, id
		replies.Add(1)
		go func() {
			defer replies.Done()
			b.Reply(id, idAnswer(i), "")
		}()
	}
	replies.Wait()
	wg.Wait()

	// Every caller got *an* answer, and no answer was handed out twice.
	seen := map[string]bool{}
	for i, a := range answers {
		if a == "" {
			t.Fatalf("caller %d got nothing", i)
		}
		if seen[a] {
			t.Fatalf("answer %q was handed to two callers", a)
		}
		seen[a] = true
	}
}

func idAnswer(i int) string { return "answer-" + strconv.Itoa(i) }

// waitForNew returns the id that appeared since `known` was taken.
func waitForNew(t *testing.T, b *PaneBridge, known []string) string {
	t.Helper()
	have := map[string]bool{}
	for _, id := range known {
		have[id] = true
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		b.mu.Lock()
		for id := range b.waiting {
			if !have[id] {
				b.mu.Unlock()
				return id
			}
		}
		b.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	t.Fatal("no new question appeared")
	return ""
}
