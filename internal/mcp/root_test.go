package mcp

import (
	"context"
	"encoding/json"
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
	rootTools := []string{"wate_sessions", "wate_tabs", "wate_session_open", "wate_session_focus", "wate_session_move", "wate_notify", "wate_session_remote_control"}
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
		{Name: "wate_session_open", Arguments: map[string]any{"cwd": " ~/work ", "prompt": "fix the build", "tab": "Hakerl", "focus": true, "remote_control": true}},
		{Name: "wate_notify", Arguments: map[string]any{"title": "Stuck", "body": "needs a password", "pane": "p7"}},
		{Name: "wate_session_remote_control", Arguments: map[string]any{"pane": "p7", "name": "nightly"}},
	} {
		res, err := cs.CallTool(ctx, &call)
		if err != nil || res.IsError {
			t.Fatalf("%s: %v %+v", call.Name, err, res)
		}
	}
	got := *seen
	if len(got) != 4 {
		t.Fatalf("requests: %+v", got)
	}
	if got[0].Cmd != "sessions" || got[0].Token != "tok-root" {
		t.Errorf("sessions: %+v", got[0])
	}
	var open sessionOpenIn
	if got[1].Cmd != "session-open" || json.Unmarshal(got[1].Data, &open) != nil ||
		open != (sessionOpenIn{Cwd: "~/work", Prompt: "fix the build", Tab: "Hakerl", Focus: true, RemoteControl: true}) {
		t.Errorf("session-open: %+v %s", got[1], got[1].Data)
	}
	if got[2].Cmd != "notify" || got[2].Name != "Stuck" || got[2].Text != "needs a password" || got[2].Target != "p7" {
		t.Errorf("notify: %+v", got[2])
	}
	if got[3].Cmd != "session-remote-control" || got[3].Target != "p7" || got[3].Name != "nightly" || got[3].Token != "tok-root" {
		t.Errorf("session-remote-control: %+v", got[3])
	}
}
