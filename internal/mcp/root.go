package mcp

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Gerry3010/wate/internal/ctl"
)

type sessionOpenIn struct {
	Cwd               string `json:"cwd" jsonschema:"the project directory to start Claude Code in (absolute, or starting with ~)"`
	Prompt            string `json:"prompt,omitempty" jsonschema:"the first prompt for the new session: what it should do, with everything it needs to know, since it starts with none of your context"`
	Tab               string `json:"tab,omitempty" jsonschema:"the tab the session belongs in, by id or title (see wate_tabs); it is added there as a split. A title no tab has yet makes a new tab of that name; empty makes a new tab named after the directory"`
	Focus             bool   `json:"focus,omitempty" jsonschema:"bring the new session to the front (and close your panel); true when the user asked you for the session, false when you open it on your own"`
	RemoteControl     bool   `json:"remote_control,omitempty" jsonschema:"start the session with Remote Control on, so the user can continue it in the Claude app or at claude.ai/code; only when the user asked for it"`
	RemoteControlName string `json:"remote_control_name,omitempty" jsonschema:"the name the session shows under in Remote Control; empty for the directory's name. Setting it switches Remote Control on"`
}

type remoteControlIn struct {
	Pane string `json:"pane" jsonschema:"the session's pane id (see wate_sessions)"`
	Name string `json:"name,omitempty" jsonschema:"the name the session shows under in Remote Control; empty leaves it to Claude Code"`
}

type tabColorIn struct {
	Tab   string `json:"tab" jsonschema:"the tab, by id or by its title (see wate_tabs)"`
	Color string `json:"color" jsonschema:"red, orange, yellow, green, cyan, blue, violet, purple, pink or gray, or #rrggbb; empty takes the colour away"`
}

type restartIn struct {
	Minutes int    `json:"minutes,omitempty" jsonschema:"how long the agents get to answer; default the user's restart_deadline"`
	Reason  string `json:"reason" jsonschema:"one line on why, shown to the user and to every agent that is asked"`
}

type resumeIn struct {
	SessionID string `json:"session_id,omitempty" jsonschema:"the Claude Code session to bring back (from wate_restart_manifest); empty brings back every one that is offered"`
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
			"its session id and transcript path, whether you opened it, and whether Remote Control is " +
			"on (with the URL it can be continued at). Read a transcript's last lines to see what a " +
			"session is up to.",
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
			"background, and the number you may have running at once is limited. With remote_control " +
			"it starts with Remote Control on. Returns pane and tab.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in sessionOpenIn) (*mcp.CallToolResult, any, error) {
		in.Cwd = strings.TrimSpace(in.Cwd)
		data, _ := json.Marshal(in)
		return call(ctl.Request{Cmd: "session-open", Data: data})
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "wate_session_remote_control",
		Description: "Switch Remote Control on in a running session, so the user can continue it in the " +
			"Claude app or at claude.ai/code: wate enters /remote-control at its prompt, as the " +
			"sidebar's menu does. Only while the session waits at an empty prompt; otherwise it " +
			"refuses with busy, waiting (a dialog is open) or input not empty (the user may be " +
			"typing), and nothing is typed. Returns the URL once it is connected; a session that " +
			"has it on already just reports its URL.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in remoteControlIn) (*mcp.CallToolResult, any, error) {
		return call(ctl.Request{Cmd: "session-remote-control", Target: in.Pane, Name: in.Name})
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "wate_session_focus",
		Description: "Bring a session's pane to the front, in whichever window and tab it is, and close " +
			"your panel. This moves what the user is looking at: do it when they ask to see a session.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in sessionPaneIn) (*mcp.CallToolResult, any, error) {
		return call(ctl.Request{Cmd: "session-focus", Target: in.Pane})
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "wate_restart",
		Description: "Ask for wate to be restarted, through the same round as the user's \"Prepare restart…\": every " +
			"agent is asked and answers go or wait, and wate restarts by itself once all of them have said go. " +
			"You count as having said go. If the deadline passes first, the user decides; you cannot force " +
			"it. Before it quits, wate checks that the installed wate runs (and calls the restart off if not) " +
			"and writes down what was running; when you are back, you are told so. Refused while another " +
			"round is running. Only when the user wants it or agreed to it, for example to load a new build.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in restartIn) (*mcp.CallToolResult, any, error) {
		return call(ctl.Request{Cmd: "restart-root", Lines: in.Minutes, Text: in.Reason})
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "wate_restart_status",
		Description: "How a restart round stands: who asked, the deadline, and each agent's answer.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, any, error) {
		return call(ctl.Request{Cmd: "restart-status"})
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "wate_restart_cancel",
		Description: "Call off the restart round you started. A round the user started is theirs to call off.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, any, error) {
		return call(ctl.Request{Cmd: "restart-cancel"})
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "wate_restart_manifest",
		Description: "What was running when wate last restarted, written just before it quit: who asked and " +
			"why, every window's tabs in order with title and colour, and per pane its directory, the Claude " +
			"session in it (id and title), whether you opened it and whether Remote Control was on. Pane and " +
			"tab ids change with a restart; match sessions by session_id and tabs by position and title.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, any, error) {
		return call(ctl.Request{Cmd: "restart-manifest"})
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "wate_session_resume",
		Description: "Bring back a Claude Code session in the pane it ran in, the way its Resume block does after a " +
			"restart: by session id, or every session that is offered when you leave it empty. A session " +
			"that is running already is not offered, so nothing runs twice. Lists what was resumed.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in resumeIn) (*mcp.CallToolResult, any, error) {
		return call(ctl.Request{Cmd: "session-resume", Name: in.SessionID})
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "wate_tab_color",
		Description: "Give a tab a colour, as the tab's own menu does, or take it away with an empty " +
			"colour. wate_tabs lists every tab's colour, so you can note them and put them back after " +
			"a tab was lost. The colours belong to the user's way of sorting their tabs: set them when " +
			"the user asks, or to restore what they had.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in tabColorIn) (*mcp.CallToolResult, any, error) {
		return call(ctl.Request{Cmd: "tab-color", Target: in.Tab, Name: in.Color})
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "wate_session_move",
		Description: "Move a session's pane into another tab of its window. This rearranges the user's " +
			"layout: propose a move first and only make it once the user has agreed.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in sessionMoveIn) (*mcp.CallToolResult, any, error) {
		return call(ctl.Request{Cmd: "session-move", Target: in.Pane, Name: in.Tab})
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
