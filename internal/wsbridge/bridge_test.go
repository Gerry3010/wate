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
