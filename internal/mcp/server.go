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

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Gerry3010/wate/internal/config"
	"github.com/Gerry3010/wate/internal/ctl"
)

// Run speaks MCP on stdin/stdout until the client goes away.
func Run(version string) error {
	s := mcp.NewServer(&mcp.Implementation{Name: "wate", Version: version}, nil)
	register(s)
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
		Description: "Type text into a pane. In a pane you opened this is sent as it stands, so " +
			"end it with a newline to run a command. In a pane the user opened up to you, " +
			"newlines and control characters are dropped: the text lands on the prompt and the " +
			"user presses Enter." + scope,
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
