package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Gerry3010/wate/internal/tbsock"
)

// fakeWindow is a tab with panes for terminalBrowser: it records what was asked of the window
// and typed into panes, and a split adds a pane.
type fakeWindow struct {
	panes  []PaneInfo
	asked  []string
	typed  []string
	focus  []string
	nextID string
}

func (f *fakeWindow) ops() tbOps {
	return tbOps{
		panes: func() []PaneInfo { return f.panes },
		ask: func(pane, kind string, args map[string]any) (string, error) {
			b, _ := json.Marshal(args)
			f.asked = append(f.asked, pane+" "+kind+" "+string(b))
			switch kind {
			case "split":
				f.panes = append(f.panes, PaneInfo{PaneID: f.nextID, TabID: "t1", TTY: "/dev/pts/9"})
				return f.nextID, nil
			case "neighbor":
				return "p2", nil
			}
			return "", nil
		},
		write: func(pane, text string) error {
			f.typed = append(f.typed, pane+" "+text)
			return nil
		},
		focus: func(pane string) error {
			f.focus = append(f.focus, pane)
			return nil
		},
	}
}

func call(t *testing.T, b *terminalBrowser, method, params string) (map[string]any, error) {
	t.Helper()
	res, err := b.handle(tbsock.Request{ID: "x", Method: method, Params: json.RawMessage(params)})
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(res)
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%s: %s is not an object", method, raw)
	}
	return out, nil
}

func newFakeWindow() *fakeWindow {
	return &fakeWindow{
		nextID: "p9",
		panes: []PaneInfo{
			{PaneID: "p1", TabID: "t1", TTY: "/dev/pts/3"},
			{PaneID: "p2", TabID: "t1", TTY: "/dev/pts/4"},
			{PaneID: "p5", TabID: "t2", TTY: "/dev/pts/7"},
		},
	}
}

func TestTerminalBrowserFindsThePaneByItsTTY(t *testing.T) {
	b := newTerminalBrowser(newFakeWindow().ops())
	hello, _ := call(t, b, "hello", `{"protocol":1}`)
	if hello["name"] != "wate" || len(hello["methods"].([]any)) != len(tbMethods) {
		t.Fatalf("hello: %v", hello)
	}
	got, _ := call(t, b, "getCurrentPane", `{"tty":"/dev/pts/4","cwd":"/"}`)
	if pane := got["pane"].(map[string]any); pane["id"] != "p2" || pane["tab"] != "t1" {
		t.Fatalf("by tty: %v", got)
	}
	// tmux or ssh in a pane: a tty wate does not own is no pane of wate's.
	if got, _ := call(t, b, "getCurrentPane", `{"tty":"/dev/pts/30"}`); got["pane"] != nil {
		t.Fatalf("a foreign tty: %v", got)
	}
	list, _ := call(t, b, "listPanes", `{"tty":null}`)
	if panes := list["panes"].([]any); len(panes) != 3 || panes[0].(map[string]any)["tty"] != "/dev/pts/3" {
		t.Fatalf("listPanes: %v", list)
	}
}

func TestTerminalBrowserSplitsWhereItIsAsked(t *testing.T) {
	for direction, want := range map[string]string{
		"right": `"before":false,"command":["tb","open"],"dir":"row"`,
		"left":  `"before":true,"command":["tb","open"],"dir":"row"`,
		"down":  `"before":false,"command":["tb","open"],"dir":"col"`,
		"up":    `"before":true,"command":["tb","open"],"dir":"col"`,
	} {
		f := newFakeWindow()
		b := newTerminalBrowser(f.ops())
		if _, err := call(t, b, "split", `{"from":{"id":"p1","tab":"t1"},"direction":"`+direction+`","command":["tb","open"],"size":null}`); err != nil {
			t.Fatalf("%s: %v", direction, err)
		}
		if len(f.asked) != 1 || !strings.HasPrefix(f.asked[0], "p1 split {") || !strings.Contains(f.asked[0], want) || strings.Contains(f.asked[0], "ratio") {
			t.Errorf("%s: asked %q", direction, f.asked)
		}
	}
	f := newFakeWindow()
	b := newTerminalBrowser(f.ops())
	if _, err := call(t, b, "split", `{"from":{"id":"p1","tab":"t1"},"direction":"right","command":["tb"],"size":0.4}`); err != nil || !strings.Contains(f.asked[0], `"ratio":0.4`) {
		t.Fatalf("size: %v %q", err, f.asked)
	}
	for _, bad := range []string{
		`{"from":{"id":"p1"},"direction":"sideways","command":["tb"]}`,
		`{"from":{"id":"p1"},"direction":"right","command":[]}`,
		`{"from":{"id":"gone"},"direction":"right","command":["tb"]}`,
	} {
		if _, err := call(t, b, "split", bad); err == nil {
			t.Errorf("let through: %s", bad)
		}
	}
	got, _ := call(t, b, "neighbor", `{"from":{"id":"p1","tab":"t1"},"direction":"right"}`)
	if pane := got["pane"].(map[string]any); pane["id"] != "p2" || pane["tab"] != "t1" {
		t.Fatalf("neighbor: %v", got)
	}
}

func TestTerminalBrowserTypesOnlyIntoThePaneItWasOpenedFrom(t *testing.T) {
	f := newFakeWindow()
	b := newTerminalBrowser(f.ops())
	// Before any browser was opened, nothing may be typed or focused.
	if _, err := call(t, b, "sendText", `{"pane":"p1","text":"hi"}`); err == nil {
		t.Fatal("typed with no browser open")
	}
	if _, err := call(t, b, "split", `{"from":{"id":"p1","tab":"t1"},"direction":"right","command":["tb"]}`); err != nil {
		t.Fatal(err)
	}
	f.asked = nil
	if _, err := call(t, b, "sendText", `{"pane":"p1","text":"the button"}`); err != nil {
		t.Fatal(err)
	}
	// A paste the window delivers, not raw keys: a line break in it must not send it off.
	if len(f.asked) != 1 || f.asked[0] != `p1 paste {"text":"the button"}` {
		t.Fatalf("asked %q", f.asked)
	}
	if _, err := call(t, b, "pasteKey", `{"pane":"p1"}`); err != nil || len(f.typed) != 1 || f.typed[0] != "p1 \x16" {
		t.Fatalf("pasteKey: %v %q", err, f.typed)
	}
	for _, pane := range []string{"p1", "p9"} {
		if _, err := call(t, b, "focusPane", `{"pane":"`+pane+`"}`); err != nil {
			t.Fatalf("focus %s: %v", pane, err)
		}
	}
	// Any other pane is the user's.
	for _, method := range []string{"sendText", "pasteKey", "focusPane"} {
		if _, err := call(t, b, method, `{"pane":"p2","text":"x"}`); err == nil {
			t.Errorf("%s into p2 let through", method)
		}
	}
	// Once the browser is closed, its opener is an ordinary pane again.
	f.panes = f.panes[:3]
	if _, err := call(t, b, "sendText", `{"pane":"p1","text":"late"}`); err == nil {
		t.Fatal("typed after the browser closed")
	}
	if _, err := call(t, b, "teleport", `{}`); err == nil {
		t.Fatal("an unknown method answered")
	}
}
