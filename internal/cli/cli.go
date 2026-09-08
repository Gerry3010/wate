// Package cli implements the non-GUI subcommands: talking to a running yate.
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/Gerry3010/yate/internal/ctl"
)

const Usage = `yate — yet another terminal emulator

usage:
  yate [--config <file>] [<dir>]  start the app (in <dir>); a running yate opens <dir> as a new tab
  yate open <file>[:line[:col]]   open a file in the editor of the running yate
  yate ctl input <text>           type text into the current pane ($YATE_PANE_ID)
  yate ctl action <name>          run a keybind action (split_right, new_tab, ...)
  yate ctl ping                   check the control socket
  yate theme import <file>        import a Ghostty/Alacritty/yate theme and activate it
  yate hook <event>               Claude Code hook entry point (reads JSON on stdin)
  yate install-hooks              register yate's hooks in ~/.claude/settings.json

The running instance is found via $YATE_SOCKET (set in every yate shell).
`

var lineCol = regexp.MustCompile(`^(.+?)(?::(\d+))?(?::(\d+))?$`)

// Run executes a subcommand and returns the exit code.
func Run(args []string) int {
	switch args[0] {
	case "open":
		if len(args) < 2 {
			fmt.Fprint(os.Stderr, Usage)
			return 2
		}
		req := ctl.Request{Cmd: "open", Pane: os.Getenv("YATE_PANE_ID"), Tab: os.Getenv("YATE_TAB_ID")}
		m := lineCol.FindStringSubmatch(args[1])
		req.Path = m[1]
		req.Line, _ = strconv.Atoi(m[2])
		req.Col, _ = strconv.Atoi(m[3])
		if !filepath.IsAbs(req.Path) {
			cwd, _ := os.Getwd()
			req.Path = filepath.Join(cwd, req.Path)
		}
		return send(req)
	case "ctl":
		if len(args) < 2 {
			fmt.Fprint(os.Stderr, Usage)
			return 2
		}
		req := ctl.Request{Cmd: args[1], Pane: os.Getenv("YATE_PANE_ID"), Tab: os.Getenv("YATE_TAB_ID")}
		rest := strings.Join(args[2:], " ")
		switch args[1] {
		case "input":
			req.Text = rest
		case "action":
			req.Name = rest
		}
		for i, a := range args[2:] {
			if a == "--pane" && i+3 < len(args) {
				req.Pane = args[i+3]
			}
		}
		return send(req)
	case "theme":
		if len(args) < 3 || args[1] != "import" {
			fmt.Fprint(os.Stderr, Usage)
			return 2
		}
		p := args[2]
		if !filepath.IsAbs(p) {
			cwd, _ := os.Getwd()
			p = filepath.Join(cwd, p)
		}
		return send(ctl.Request{Cmd: "import-theme", Path: p})
	case "hook":
		if len(args) < 2 {
			return 2
		}
		return runHook(args[1])
	case "install-hooks":
		if err := InstallHooks(claudeSettingsPath()); err != nil {
			fmt.Fprintln(os.Stderr, "install-hooks:", err)
			return 1
		}
		fmt.Println("yate hooks installed in", claudeSettingsPath())
		return 0
	}
	fmt.Fprint(os.Stderr, Usage)
	return 2
}

func send(req ctl.Request) int {
	sock, err := ctl.FindSocket()
	if err != nil {
		fmt.Fprintln(os.Stderr, "yate:", err)
		return 1
	}
	resp, err := ctl.Send(sock, req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "yate:", err)
		return 1
	}
	if !resp.OK {
		fmt.Fprintln(os.Stderr, "yate:", resp.Error)
		return 1
	}
	if resp.Data != nil {
		fmt.Println(resp.Data)
	}
	return 0
}

// runHook forwards a Claude Code hook payload to the app. Outside a yate shell
// ($YATE_SOCKET unset) it is a silent no-op so the hooks don't bother other terminals.
func runHook(event string) int {
	sock := os.Getenv("YATE_SOCKET")
	if sock == "" {
		return 0
	}
	data, _ := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	if !json.Valid(data) {
		data = nil
	}
	resp, err := ctl.Send(sock, ctl.Request{Cmd: "hook", Event: event, Pane: os.Getenv("YATE_PANE_ID"), Tab: os.Getenv("YATE_TAB_ID"), Data: data})
	// The app may be gone; never fail the hook because of us, but say why on stderr.
	if err != nil {
		fmt.Fprintln(os.Stderr, "yate hook:", err)
	} else if !resp.OK {
		fmt.Fprintln(os.Stderr, "yate hook:", resp.Error)
	}
	return 0
}

func claudeSettingsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "settings.json")
}
