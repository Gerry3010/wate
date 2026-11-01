package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Gerry3010/wate/internal/ctl"
)

// fakeQueue serves "attention" from a script of answers, one per question; past the end it
// answers with nothing pending.
func fakeQueue(t *testing.T, script ...ctl.Response) (sock string, tokens *[]string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "wate")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock = filepath.Join(dir, "w.sock")
	var mu sync.Mutex
	seen := []string{}
	srv, err := ctl.Listen(sock, func(r ctl.Request) ctl.Response {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, r.Token)
		if len(script) == 0 {
			return ctl.Response{OK: true, Data: []any{}}
		}
		next := script[0]
		script = script[1:]
		return next
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	return sock, &seen
}

// testWatcher runs on a clock that each pause moves forward.
func testWatcher(sock string, out, errOut *bytes.Buffer) watcher {
	now := time.Unix(1000, 0)
	return watcher{
		sock: sock, token: "tok-root", every: time.Second, gather: 2 * time.Second, giveUp: 10 * time.Second,
		out: out, errOut: errOut,
		now:   func() time.Time { return now },
		sleep: func(d time.Duration) { now = now.Add(d) },
	}
}

func event(kind string) map[string]any { return map[string]any{"kind": kind} }

func TestWatchOnceGathersABunchThenEnds(t *testing.T) {
	sock, tokens := fakeQueue(t,
		ctl.Response{OK: true, Data: []any{}},
		ctl.Response{OK: true, Data: []any{event("permission")}},
		ctl.Response{OK: true, Data: []any{event("question"), event("context")}},
		ctl.Response{OK: true, Data: []any{}},
	)
	var out, errOut bytes.Buffer
	if code := testWatcher(sock, &out, &errOut).run(true); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut.String())
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 || lines[0] != `{"kind":"permission"}` || lines[2] != `{"kind":"context"}` {
		t.Fatalf("printed %q", out.String())
	}
	for _, tok := range *tokens {
		if tok != "tok-root" {
			t.Fatalf("asked without the pane token: %q", *tokens)
		}
	}
}

func TestWatchStopsWhenRefused(t *testing.T) {
	sock, _ := fakeQueue(t,
		ctl.Response{OK: true, Data: []any{event("permission")}},
		ctl.Response{Error: "only the root agent may ask for attention"},
	)
	var out, errOut bytes.Buffer
	if code := testWatcher(sock, &out, &errOut).run(false); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out.String(), "permission") || !strings.Contains(errOut.String(), "only the root agent") {
		t.Fatalf("out %q, err %q", out.String(), errOut.String())
	}
}

func TestWatchGivesUpOnAWateThatIsGone(t *testing.T) {
	var out, errOut bytes.Buffer
	w := testWatcher(filepath.Join(t.TempDir(), "gone.sock"), &out, &errOut)
	if code := w.run(true); code != 1 || !strings.Contains(errOut.String(), "gave up") {
		t.Fatalf("exit %d, err %q", code, errOut.String())
	}
}
