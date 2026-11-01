package mcp

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Gerry3010/wate/internal/ctl"
)

type sessionOpenIn struct {
	Cwd    string `json:"cwd" jsonschema:"the project directory to start Claude Code in (absolute, or starting with ~)"`
	Prompt string `json:"prompt,omitempty" jsonschema:"the first prompt for the new session: what it should do, with everything it needs to know, since it starts with none of your context"`
}

type notifyIn struct {
	Title string `json:"title" jsonschema:"a few words: what needs the user"`
	Body  string `json:"body,omitempty" jsonschema:"one or two sentences: what happened and what the user should do"`
	Pane  string `json:"pane,omitempty" jsonschema:"the pane the notification is about; a click on it takes the user there"`
}

// registerRoot adds the root agent's tools. They reach across every tab and window, which is
// why wate answers them only for the root agent's own pane.
func registerRoot(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "wate_sessions",
		Description: "List every Claude Code session running in wate, in all tabs and windows: pane, " +
			"directory, title, status and phase (working, permission = blocked on the user, done), " +
			"whether it ended on a question, context usage, how many minutes it has sat unattended, " +
			"its session id and transcript path, and whether you opened it. Read a transcript's last " +
			"lines to see what a session is up to.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, any, error) {
		out, err := send(ctl.Request{Cmd: "sessions"})
		if err != nil {
			return nil, nil, err
		}
		return text(out), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "wate_session_open",
		Description: "Start Claude Code in a new tab, in the background, in a project directory, " +
			"with a first prompt. The user gets a notification that you did. The number of sessions " +
			"you may have running at once is limited. Returns the new pane and tab.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in sessionOpenIn) (*mcp.CallToolResult, any, error) {
		out, err := send(ctl.Request{Cmd: "session-open", Path: strings.TrimSpace(in.Cwd), Text: in.Prompt})
		if err != nil {
			return nil, nil, err
		}
		return text(out), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "wate_notify",
		Description: "Put up a desktop notification for the user. Name the pane it is about, so a " +
			"click takes them there. A newer notification about the same pane replaces the older. " +
			"Use it for what needs the user, not for news.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in notifyIn) (*mcp.CallToolResult, any, error) {
		if _, err := send(ctl.Request{Cmd: "notify", Name: in.Title, Text: in.Body, Target: in.Pane}); err != nil {
			return nil, nil, err
		}
		return text("Notified."), nil, nil
	})
}
