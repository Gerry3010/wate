package wsbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/Gerry3010/wate/internal/pty"
)

func TestRejectsBadToken(t *testing.T) {
	s, err := New(pty.NewManager())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/pty/x?token=nope", s.Port()))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

func TestRoundTrip(t *testing.T) {
	m := pty.NewManager()
	s, err := New(m)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	sess, err := m.Spawn(pty.SpawnOptions{Command: []string{"/bin/sh", "-c", "read x; echo got:$x; stty size; exit 0"}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, fmt.Sprintf("ws://127.0.0.1:%d/pty/%s?token=%s", s.Port(), sess.ID, s.Token()), nil)
	if err != nil {
		t.Fatal(err)
	}
	ctl, _ := json.Marshal(control{Type: "resize", Cols: 132, Rows: 43})
	if err := c.Write(ctx, websocket.MessageText, ctl); err != nil {
		t.Fatal(err)
	}
	if err := c.Write(ctx, websocket.MessageBinary, []byte("ping\n")); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			if websocket.CloseStatus(err) == websocket.StatusNormalClosure {
				break
			}
			t.Fatalf("read: %v (so far %q)", err, out.String())
		}
		out.Write(data)
	}
	got := out.String()
	if !strings.Contains(got, "got:ping") || !strings.Contains(got, "43 132") {
		t.Fatalf("unexpected transcript %q", got)
	}
}

// A pane moving between windows reconnects to the same live session. The old socket must
// stand down, the new one must pick the stream up, and nothing may be lost in between —
// which is exactly what a second reader on the PTY master could not promise.
func TestTakeoverLosesNothing(t *testing.T) {
	m := pty.NewManager()
	s, err := New(m)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// Counts to 40 slowly enough that the handover lands in the middle of the run.
	sess, err := m.Spawn(pty.SpawnOptions{Command: []string{"/bin/sh", "-c", "i=1; while [ $i -le 40 ]; do echo line$i; i=$((i+1)); sleep 0.05; done; exit 0"}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	url := fmt.Sprintf("ws://127.0.0.1:%d/pty/%s?token=%s", s.Port(), sess.ID, s.Token())

	first, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	// Read a few chunks, then hand over mid-stream.
	deadline, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	for out.Len() < 40 {
		_, data, err := first.Read(deadline)
		if err != nil {
			t.Fatalf("first read: %v", err)
		}
		out.Write(data)
	}

	second, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	// The first socket is told it was taken over, not that the child exited.
	if _, _, err = first.Read(ctx); err == nil {
		t.Fatal("the first connection should have been closed by the takeover")
	}
	_ = first.CloseNow()

	for {
		_, data, err := second.Read(ctx)
		if err != nil {
			if websocket.CloseStatus(err) == websocket.StatusNormalClosure {
				break
			}
			t.Fatalf("second read: %v (so far %q)", err, out.String())
		}
		out.Write(data)
	}

	got := out.String()
	for i := 1; i <= 40; i++ {
		if !strings.Contains(got, fmt.Sprintf("line%d\r\n", i)) {
			t.Fatalf("line%d was lost across the handover; transcript:\n%s", i, got)
		}
	}
}

// Output produced while no window holds the pane is kept and delivered on reconnect.
func TestBacklogSurvivesAGap(t *testing.T) {
	m := pty.NewManager()
	sess, err := m.Spawn(pty.SpawnOptions{Command: []string{"/bin/sh", "-c", "read x; echo after:$x; exit 0"}})
	if err != nil {
		t.Fatal(err)
	}
	// Subscribe, then let go — as a window does when it hands a tab away.
	_, unsubscribe := sess.Subscribe()
	unsubscribe()

	if _, err := sess.File().WriteString("hello\n"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond) // let the child answer into nobody

	out, cancel := sess.Subscribe()
	defer cancel()
	var got strings.Builder
	timeout := time.After(5 * time.Second)
	for !strings.Contains(got.String(), "after:hello") {
		select {
		case chunk, ok := <-out.C:
			if !ok {
				t.Fatalf("stream ended before the backlog arrived: %q", got.String())
			}
			got.Write(chunk)
		case <-timeout:
			t.Fatalf("backlog never arrived: %q", got.String())
		}
	}
}
