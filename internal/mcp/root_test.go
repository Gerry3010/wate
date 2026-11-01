package mcp

import (
	"context"
	"slices"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// connect runs a server with or without the root tools and returns a client session to it.
func connect(t *testing.T, root bool) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	s := mcp.NewServer(&mcp.Implementation{Name: "wate", Version: "test"}, nil)
	register(s)
	registerRun(s)
	if root {
		registerRoot(s)
	}
	ct, st := mcp.NewInMemoryTransports()
	if _, err := s.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func toolNames(t *testing.T, cs *mcp.ClientSession) []string {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	return names
}

func TestOnlyTheRootServerOffersTheRootTools(t *testing.T) {
	rootTools := []string{"wate_sessions", "wate_session_open", "wate_notify"}
	plain := toolNames(t, connect(t, false))
	withRoot := toolNames(t, connect(t, true))
	for _, name := range rootTools {
		if slices.Contains(plain, name) {
			t.Errorf("an ordinary session is offered %s", name)
		}
		if !slices.Contains(withRoot, name) {
			t.Errorf("the root agent lacks %s", name)
		}
	}
	if !slices.Contains(withRoot, "wate_pane_split") {
		t.Error("the root agent lost the pane tools")
	}
}

func TestRootToolsSendWhatWateExpects(t *testing.T) {
	sock, seen := fakeWate(t)
	t.Setenv("WATE_SOCKET", sock)
	t.Setenv("WATE_PANE_ID", "root")
	t.Setenv("WATE_PANE_TOKEN", "tok-root")
	cs := connect(t, true)
	ctx := context.Background()
	for _, call := range []mcp.CallToolParams{
		{Name: "wate_sessions", Arguments: map[string]any{}},
		{Name: "wate_session_open", Arguments: map[string]any{"cwd": " ~/work ", "prompt": "fix the build"}},
		{Name: "wate_notify", Arguments: map[string]any{"title": "Stuck", "body": "needs a password", "pane": "p7"}},
	} {
		res, err := cs.CallTool(ctx, &call)
		if err != nil || res.IsError {
			t.Fatalf("%s: %v %+v", call.Name, err, res)
		}
	}
	got := *seen
	if len(got) != 3 {
		t.Fatalf("requests: %+v", got)
	}
	if got[0].Cmd != "sessions" || got[0].Token != "tok-root" {
		t.Errorf("sessions: %+v", got[0])
	}
	if got[1].Cmd != "session-open" || got[1].Path != "~/work" || got[1].Text != "fix the build" {
		t.Errorf("session-open: %+v", got[1])
	}
	if got[2].Cmd != "notify" || got[2].Name != "Stuck" || got[2].Text != "needs a password" || got[2].Target != "p7" {
		t.Errorf("notify: %+v", got[2])
	}
}
