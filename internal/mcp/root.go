package mcp

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Gerry3010/wate/internal/ctl"
)

type sessionOpenIn struct {
	Cwd    string `json:"cwd" jsonschema:"the project directory to start Claude Code in (absolute, or starting with ~)"`
	Prompt string `json:"prompt,omitempty" jsonschema:"the first prompt for the new session: what it should do, with everything it needs to know, since it starts with none of your context"`
	Tab    string `json:"tab,omitempty" jsonschema:"the tab the session belongs in, by id or title (see wate_tabs); it is added there as a split. A title no tab has yet makes a new tab of that name; empty makes a new tab named after the directory"`
	Focus  bool   `json:"focus,omitempty" jsonschema:"bring the new session to the front (and close your panel); true when the user asked you for the session, false when you open it on your own"`
}

type notifyIn struct {
	Title string `json:"title" jsonschema:"a few words: what needs the user"`
	Body  string `json:"body,omitempty" jsonschema:"one or two sentences: what happened and what the user should do"`
	Pane  string `json:"pane,omitempty" jsonschema:"the pane the notification is about; a click on it takes the user there"`
}

type sessionPaneIn struct {
	Pane string `json:"pane" jsonschema:"the session's pane id (see wate_sessions)"`
}

type sessionMoveIn struct {
	Pane string `json:"pane" jsonschema:"the session's pane id (see wate_sessions)"`
	Tab  string `json:"tab" jsonschema:"the tab to move it into, by id or title; a title no tab has yet makes a new tab of that name"`
}

// registerRoot adds the root agent's tools. They reach across every tab and window, which is
// why wate answers them only for the root agent's own pane.
func registerRoot(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "wate_sessions",
		Description: "List every Claude Code session running in wate, in all tabs and windows: pane, " +
			"tab, directory, title, status and phase (working, permission = blocked on the user, done), " +
			"whether it ended on a question, context usage, how many minutes it has sat unattended, " +
			"its session id and transcript path, and whether you opened it. Read a transcript's last " +
			"lines to see what a session is up to.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, any, error) {
		return call(ctl.Request{Cmd: "sessions"})
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "wate_tabs",
		Description: "List the tabs of every wate window with their titles and panes (directory, what " +
			"runs there, the Claude session's title). The user names tabs after what belongs together; " +
			"use this to decide where a new session goes and whether one sits in the wrong tab.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, any, error) {
		return call(ctl.Request{Cmd: "tabs"})
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "wate_session_open",
		Description: "Start Claude Code in a project directory with a first prompt, as a split in the " +
			"tab it belongs in (or a new tab). It stays in the background unless you set focus. A " +
			"directory Claude Code does not trust yet always comes to the front, because the session " +
			"first asks the user whether to trust it. The user is notified when it opens in the " +
			"background, and the number you may have running at once is limited. Returns pane and tab.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in sessionOpenIn) (*mcp.CallToolResult, any, error) {
		data, _ := json.Marshal(sessionOpenIn{Cwd: strings.TrimSpace(in.Cwd), Prompt: in.Prompt, Tab: in.Tab, Focus: in.Focus})
		return call(ctl.Request{Cmd: "session-open", Data: data})
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "wate_session_focus",
		Description: "Bring a session's pane to the front, in whichever window and tab it is, and close " +
			"your panel. This moves what the user is looking at: do it when they ask to see a session.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in sessionPaneIn) (*mcp.CallToolResult, any, error) {
		return call(ctl.Request{Cmd: "session-focus", Target: in.Pane})
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "wate_session_move",
		Description: "Move a session's pane into another tab of its window. This rearranges the user's " +
			"layout: propose a move first and only make it once the user has agreed.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in sessionMoveIn) (*mcp.CallToolResult, any, error) {
		return call(ctl.Request{Cmd: "session-move", Target: in.Pane, Name: in.Tab})
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

// call sends one request and hands back what wate answered.
func call(r ctl.Request) (*mcp.CallToolResult, any, error) {
	out, err := send(r)
	if err != nil {
		return nil, nil, err
	}
	return text(out), nil, nil
}
