// Package mcp exposes wate's panes to a Model Context Protocol client — in practice, to a
// Claude Code session running inside one of those panes.
//
// It is deliberately thin. Every tool is one control-socket request, and every decision about
// what is allowed is made by the running wate, not here: this process is a translator, and
// anything it can do, `wate ctl …` can do too. That is what keeps the feature debuggable from
// a shell rather than only from inside a conversation.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Gerry3010/wate/internal/config"
	"github.com/Gerry3010/wate/internal/ctl"
)

// Run speaks MCP on stdin/stdout until the client goes away.
func Run(version string) error {
	s := mcp.NewServer(&mcp.Implementation{Name: "wate", Version: version}, nil)
	register(s)
	registerRun(s)
	err := s.Run(context.Background(), &mcp.StdioTransport{})
	// The client closing its end is how this ends normally, not something to report.
	if err != nil && (errors.Is(err, io.EOF) || strings.Contains(err.Error(), "EOF")) {
		return nil
	}
	return err
}

func text(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}

// send puts one request to the running wate.
//
// The socket is resolved per call rather than once at startup: an agent outlives a wate
// restart, and the next instance listens somewhere else.
func send(r ctl.Request) (string, error) {
	r.Pane = os.Getenv("WATE_PANE_ID")
	r.Token = os.Getenv("WATE_PANE_TOKEN")
	if r.Pane == "" {
		return "", fmt.Errorf("not running inside a wate pane")
	}
	sock := os.Getenv("WATE_SOCKET")
	if sock == "" || !alive(sock) {
		if p, err := ctl.FindPrimary(config.StateDir()); err == nil {
			sock = p
		} else if p, err := ctl.FindSocket(); err == nil {
			sock = p
		} else {
			return "", fmt.Errorf("no running wate to talk to")
		}
	}
	resp, err := ctl.Send(sock, r)
	if err != nil {
		return "", err
	}
	if !resp.OK {
		return "", fmt.Errorf("%s", resp.Error)
	}
	if resp.Data == nil {
		return "", nil
	}
	if s, ok := resp.Data.(string); ok {
		return s, nil
	}
	b, err := json.Marshal(resp.Data)
	return string(b), err
}

func alive(sock string) bool {
	_, err := ctl.Send(sock, ctl.Request{Cmd: "ping"})
	return err == nil
}

// size turns what a model is likely to say into a fraction of a divider.
func size(s string) (float64, error) {
	switch strings.TrimSpace(strings.ToLower(s)) {
	case "", "half", "1/2":
		return 0.5, nil
	case "third", "1/3":
		return 1.0 / 3.0, nil
	case "two-thirds", "2/3":
		return 2.0 / 3.0, nil
	}
	var f float64
	if _, err := fmt.Sscanf(s, "%g", &f); err != nil || f <= 0 || f >= 1 {
		return 0, fmt.Errorf("size must be third, half, two-thirds or a fraction between 0 and 1, not %q", s)
	}
	return f, nil
}

type splitIn struct {
	Direction string `json:"direction,omitempty" jsonschema:"beside (a column to the right) or below (a row underneath); default beside"`
	Size      string `json:"size,omitempty" jsonschema:"how much of the space the new pane gets: third, half, two-thirds, or a fraction like 0.4"`
}

type paneIn struct {
	Pane string `json:"pane,omitempty" jsonschema:"the pane to act on; leave empty for your own"`
}

type readIn struct {
	Pane  string `json:"pane,omitempty" jsonschema:"the pane to read; leave empty for your own"`
	Lines int    `json:"lines,omitempty" jsonschema:"how many lines from the end of the output; default 200"`
}

type writeIn struct {
	Text string `json:"text" jsonschema:"the text to type"`
	Pane string `json:"pane,omitempty" jsonschema:"the pane to type into; leave empty for your own"`
}

type ratioIn struct {
	Size      string `json:"size" jsonschema:"third, half, two-thirds, or a fraction between 0 and 1"`
	Direction string `json:"direction,omitempty" jsonschema:"width (the column divider) or height (the row divider); default width"`
	Pane      string `json:"pane,omitempty" jsonschema:"the pane to resize; leave empty for your own"`
}

type respondIn struct {
	Verdict string `json:"verdict" jsonschema:"go if stopping now costs nothing, wait if you need longer"`
	Minutes int    `json:"minutes,omitempty" jsonschema:"roughly how many more minutes you need, when the verdict is wait"`
	Note    string `json:"note,omitempty" jsonschema:"one short line on what you are in the middle of"`
}

type runIn struct {
	Command string `json:"command" jsonschema:"the shell command to run"`
	Pane    string `json:"pane,omitempty" jsonschema:"the pane to run it in; leave empty to use (or open) your work pane"`
	Seconds int    `json:"seconds,omitempty" jsonschema:"how long to wait for it to finish before reporting back, default 60"`
}

type emptyIn struct{}

// dirOf maps the words a model reaches for onto the layout's two directions.
func dirOf(s string, fallback string) string {
	switch strings.TrimSpace(strings.ToLower(s)) {
	case "beside", "right", "row", "width", "horizontal":
		return "row"
	case "below", "down", "col", "column", "height", "vertical":
		return "col"
	}
	return fallback
}

const scope = "\n\nOnly panes in your own tab can be reached. You may do anything to panes you " +
	"opened yourself; any other pane has to be opened up to you by the user, in that pane's menu."

func register(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "wate_pane_split",
		Description: "Open a new terminal pane next to yours, in your own tab, and return its id. " +
			"Use it as a workspace: run a build, tail a log, keep a server going. You own what " +
			"you open and can write to it, read it and close it without asking." + scope,
	}, func(_ context.Context, _ *mcp.CallToolRequest, in splitIn) (*mcp.CallToolResult, any, error) {
		ratio := 0.0
		if in.Size != "" {
			f, err := size(in.Size)
			if err != nil {
				return nil, nil, err
			}
			ratio = f
		}
		id, err := send(ctl.Request{Cmd: "pane-split", Dir: dirOf(in.Direction, "row"), Ratio: ratio})
		if err != nil {
			return nil, nil, err
		}
		return text("Opened pane " + id), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "wate_pane_list",
		Description: "List the panes of your tab: their ids, what each is running, and where it " +
			"is looking. Start here when you need a pane id.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, any, error) {
		out, err := send(ctl.Request{Cmd: "pane-list"})
		if err != nil {
			return nil, nil, err
		}
		return text(out), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "wate_pane_read",
		Description: "Read what is on a pane's screen, as plain text — the live screen, so a " +
			"full-screen program's display comes back as it looks." + scope,
	}, func(_ context.Context, _ *mcp.CallToolRequest, in readIn) (*mcp.CallToolResult, any, error) {
		out, err := send(ctl.Request{Cmd: "pane-read", Target: in.Pane, Lines: in.Lines})
		if err != nil {
			return nil, nil, err
		}
		return text(out), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "wate_pane_write",
		Description: "Type text into a pane. In a pane you opened, and in one the user has given " +
			"you both Read and Write, this is sent as it stands, so end it with a newline to run " +
			"a command. With Write alone, newlines and control characters are dropped: the text " +
			"lands on the prompt and the user presses Enter." + scope,
	}, func(_ context.Context, _ *mcp.CallToolRequest, in writeIn) (*mcp.CallToolResult, any, error) {
		if _, err := send(ctl.Request{Cmd: "pane-write", Target: in.Pane, Text: in.Text}); err != nil {
			return nil, nil, err
		}
		return text("Typed."), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "wate_pane_ratio",
		Description: "Give a pane a share of the space it divides with its neighbour." + scope,
	}, func(_ context.Context, _ *mcp.CallToolRequest, in ratioIn) (*mcp.CallToolResult, any, error) {
		f, err := size(in.Size)
		if err != nil {
			return nil, nil, err
		}
		if _, err := send(ctl.Request{Cmd: "split-ratio", Target: in.Pane, Dir: dirOf(in.Direction, "row"), Ratio: f}); err != nil {
			return nil, nil, err
		}
		return text("Resized."), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "wate_pane_close",
		Description: "Close a pane. Tidy up the ones you opened when you are done with them." + scope,
	}, func(_ context.Context, _ *mcp.CallToolRequest, in paneIn) (*mcp.CallToolResult, any, error) {
		if _, err := send(ctl.Request{Cmd: "pane-close", Target: in.Pane}); err != nil {
			return nil, nil, err
		}
		return text("Closed."), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "wate_restart_respond",
		Description: "Answer the user's question about restarting wate, which would end this " +
			"session. Say \"go\" when you are at a point where stopping costs nothing, or " +
			"\"wait\" with a rough number of minutes when you are in the middle of something. " +
			"Nothing is ended without the user pressing the button, so an honest estimate is " +
			"worth more than a quick yes. Answer once and carry on working.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in respondIn) (*mcp.CallToolResult, any, error) {
		v := strings.TrimSpace(strings.ToLower(in.Verdict))
		if v != "go" && v != "wait" {
			return nil, nil, fmt.Errorf("verdict must be go or wait, not %q", in.Verdict)
		}
		if _, err := send(ctl.Request{Cmd: "restart-respond", Name: v, Lines: in.Minutes, Text: in.Note}); err != nil {
			return nil, nil, err
		}
		return text("Answered: " + v), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "wate_pane_focus",
		Description: "Bring a pane's tab to the front and put the cursor in it. This moves what " +
			"the user is looking at, so use it when you want them to see something." + scope,
	}, func(_ context.Context, _ *mcp.CallToolRequest, in paneIn) (*mcp.CallToolResult, any, error) {
		if _, err := send(ctl.Request{Cmd: "pane-focus", Target: in.Pane}); err != nil {
			return nil, nil, err
		}
		return text("Focused."), nil, nil
	})
}

// workPane is the pane this server opened for running things in, reused across calls so a
// session does not end up with a row of abandoned terminals.
var workPane string

// needsYou spots the prompts that cannot be answered from here — a password, a passphrase, a
// host key, a second factor. They all mean the same thing: the pane is waiting for the user.
var needsYou = []string{
	"password", "passphrase", "verification code", "(yes/no", "fingerprint",
}

// waitingForUser looks at the last line only, and only when it reads like a question left
// hanging: something that asks and then stops, with the cursor after it.
//
// Looking at the whole screen catches the command that was typed as readily as the prompt it
// produced — `grep password config.yml` would stop every run — and a password asked for ten
// minutes ago is still up there in the scrollback.
func waitingForUser(screen string) bool {
	lines := strings.Split(strings.TrimRight(screen, " \t\r\n"), "\n")
	if len(lines) == 0 {
		return false
	}
	last := strings.ToLower(strings.TrimRight(lines[len(lines)-1], " \t"))
	if last == "" {
		return false
	}
	// A prompt ends where the answer would go.
	if !strings.HasSuffix(last, ":") && !strings.HasSuffix(last, "?") && !strings.HasSuffix(last, "]") {
		return false
	}
	for _, p := range needsYou {
		if strings.Contains(last, p) {
			return true
		}
	}
	return false
}

// paneIsBusy reports the name of what a pane is running, "" for the shell itself.
func paneIsBusy(pane string) (string, error) {
	out, err := send(ctl.Request{Cmd: "pane-status", Target: pane})
	if err != nil {
		return "", err
	}
	var st struct {
		Name string `json:"name"`
	}
	if json.Unmarshal([]byte(out), &st) != nil {
		return "", nil
	}
	return st.Name, nil
}

func registerRun(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "wate_pane_run",
		Description: "Run a shell command in a pane beside you and wait for it to finish, then " +
			"return what it printed. Use this for anything that needs the user — sudo, an ssh " +
			"passphrase, a host key, a second factor: the command runs where they can see it, " +
			"and when it asks for something only they can type, this returns straight away, " +
			"brings the pane to the front and tells you so. Call it again afterwards with an " +
			"empty command to pick the output back up. Without a pane it uses (and if need be " +
			"opens) one work pane, which it then keeps using. It needs a pane of your own, or " +
			"one the user has given you both Read and Write: with Write alone the command is " +
			"only typed, never sent." + scope,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in runIn) (*mcp.CallToolResult, any, error) {
		pane := in.Pane
		if pane == "" {
			pane = workPane
		}
		if pane == "" {
			id, err := send(ctl.Request{Cmd: "pane-split", Dir: "col", Ratio: 1.0 / 3.0})
			if err != nil {
				return nil, nil, err
			}
			workPane, pane = id, id
		}
		if in.Command != "" {
			if _, err := send(ctl.Request{Cmd: "pane-write", Target: pane, Text: in.Command + "\n"}); err != nil {
				return nil, nil, err
			}
		}

		seconds := in.Seconds
		if seconds <= 0 {
			seconds = 60
		}
		begin := time.Now()
		deadline := begin.Add(time.Duration(seconds) * time.Second)
		started := false
		for time.Now().Before(deadline) {
			select {
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			case <-time.After(400 * time.Millisecond):
			}
			screen, err := send(ctl.Request{Cmd: "pane-read", Target: pane, Lines: 40})
			if err != nil {
				return nil, nil, err
			}
			if waitingForUser(screen) {
				// Put it in front of them; this is the whole point of running it in a pane.
				_, _ = send(ctl.Request{Cmd: "pane-focus", Target: pane})
				return text("Pane " + pane + " is waiting for you to type something (a password, " +
					"a passphrase or a confirmation). It is in front of you now. The last lines:\n\n" +
					screen), nil, nil
			}
			busy, err := paneIsBusy(pane)
			if err != nil {
				return nil, nil, err
			}
			if busy != "" {
				started = true
				continue
			}
			// Back at the shell. A command that has not got going yet gets a couple of
			// seconds to appear, so something short-lived is not mistaken for nothing at all.
			if started || time.Since(begin) > 2*time.Second {
				out, err := send(ctl.Request{Cmd: "pane-read", Target: pane, Lines: 60})
				if err != nil {
					return nil, nil, err
				}
				return text(out), nil, nil
			}
		}
		out, _ := send(ctl.Request{Cmd: "pane-read", Target: pane, Lines: 40})
		return text("Still running after the time allowed. The last lines:\n\n" + out), nil, nil
	})
}
