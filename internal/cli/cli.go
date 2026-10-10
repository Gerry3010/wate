// Package cli implements the non-GUI subcommands: talking to a running wate.
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

	"github.com/Gerry3010/wate/internal/ctl"
	"github.com/Gerry3010/wate/internal/mcp"
)

const Usage = `wate — yet another terminal emulator

usage:
  wate [--config <file>] [<dir>]  start the app (in <dir>); a running wate opens <dir> as a new tab
  wate --new-window [<dir>]       open another window of the running wate (in <dir>)
  wate --standalone [<dir>]       start a separate instance instead of talking to the running one
  wate open <file>[:line[:col]]   open a file in the editor of the running wate
  wate ctl input <text>           type text into the current pane ($WATE_PANE_ID)
  wate ctl action <name>          run a keybind action (split_right, new_tab, ...)
  wate ctl pane-read [<lines>]    print what is on the pane's screen (default 200 lines)
  wate ctl pane-split [row|col] [<size>]
                                  open a split beside this pane (size: a fraction, or
                                  third/half/two-thirds)
  wate ctl split-ratio [row|col] <size>
                                  resize this pane's share of its divider
  wate ctl pane-write <text>      type text into a pane (--target <id> for another one)
  wate ctl pane-move new|<n>      move a pane into a tab of its own, or into tab <n>
  wate ctl pane-close             close a pane
  wate ctl pane-focus             focus a pane
  wate ctl pane-list              list the panes of this tab, as JSON
  wate ctl new-window [<dir>]     open another window
  wate ctl ping                   check the control socket
  wate theme import <file>        import a Ghostty/Alacritty/wate theme and activate it
  wate hook <event>               Claude Code hook entry point (reads JSON on stdin)
  wate install-hooks              register wate's hooks in ~/.claude/settings.json
  wate mcp                        Model Context Protocol server (stdio), for Claude Code

--pane <id> says who is asking (normally $WATE_PANE_ID); --target <id> says which pane to act
on. A pane may always act on itself and on panes it opened; anything else has to be allowed in
that pane's own menu.

The running instance is found via $WATE_SOCKET (set in every wate shell).
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
		req := ctl.Request{Cmd: "open", Pane: os.Getenv("WATE_PANE_ID"), Tab: os.Getenv("WATE_TAB_ID")}
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
		req := ctl.Request{
			Cmd:   args[1],
			Pane:  os.Getenv("WATE_PANE_ID"),
			Tab:   os.Getenv("WATE_TAB_ID"),
			Token: os.Getenv("WATE_PANE_TOKEN"),
		}
		var words []string
		for i := 2; i < len(args); i++ {
			if args[i] == "--pane" && i+1 < len(args) {
				req.Pane = args[i+1]
				i++
				continue
			}
			if args[i] == "--target" && i+1 < len(args) {
				req.Target = args[i+1]
				i++
				continue
			}
			words = append(words, args[i])
		}
		rest := strings.Join(words, " ")
		switch args[1] {
		case "input":
			req.Text = rest
		case "action":
			req.Name = rest
		case "new-window", "new-tab":
			req.Path = rest
		case "pane-read":
			if rest != "" {
				n, err := strconv.Atoi(rest)
				if err != nil {
					fmt.Fprintln(os.Stderr, "wate: pane-read takes a line count")
					return 2
				}
				req.Lines = n
			}
		case "pane-write":
			req.Text = rest
		case "pane-move":
			req.Name = rest
		case "pane-split", "split-ratio":
			for _, w := range words {
				switch {
				case w == "row" || w == "right" || w == "horizontal":
					req.Dir = "row"
				case w == "col" || w == "down" || w == "vertical":
					req.Dir = "col"
				default:
					r, ok := parseRatio(w)
					if !ok {
						fmt.Fprintf(os.Stderr, "wate: %s does not understand %q\n", args[1], w)
						return 2
					}
					req.Ratio = r
				}
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
	case "mcp":
		// Model Context Protocol over stdin/stdout, for a Claude Code session in a wate pane.
		if err := mcp.Run("dev"); err != nil {
			fmt.Fprintln(os.Stderr, "mcp:", err)
			return 1
		}
		return 0
	case "install-hooks":
		if err := InstallHooks(claudeSettingsPath()); err != nil {
			fmt.Fprintln(os.Stderr, "install-hooks:", err)
			return 1
		}
		fmt.Println("wate hooks installed in", claudeSettingsPath())
		return 0
	}
	fmt.Fprint(os.Stderr, Usage)
	return 2
}

func send(req ctl.Request) int {
	sock, err := ctl.FindSocket()
	if err != nil {
		fmt.Fprintln(os.Stderr, "wate:", err)
		return 1
	}
	resp, err := ctl.Send(sock, req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wate:", err)
		return 1
	}
	if !resp.OK {
		fmt.Fprintln(os.Stderr, "wate:", resp.Error)
		return 1
	}
	if resp.Data != nil {
		fmt.Println(resp.Data)
	}
	return 0
}

// runHook forwards a Claude Code hook payload to the app. Outside a wate shell
// ($WATE_SOCKET unset) it is a silent no-op so the hooks don't bother other terminals.
func runHook(event string) int {
	sock := os.Getenv("WATE_SOCKET")
	if sock == "" {
		return 0
	}
	data, _ := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	if !json.Valid(data) {
		data = nil
	}
	resp, err := ctl.Send(sock, ctl.Request{Cmd: "hook", Event: event, Pane: os.Getenv("WATE_PANE_ID"), Tab: os.Getenv("WATE_TAB_ID"), Data: data})
	// The app may be gone; never fail the hook because of us, but say why on stderr.
	if err != nil {
		fmt.Fprintln(os.Stderr, "wate hook:", err)
	} else if !resp.OK {
		fmt.Fprintln(os.Stderr, "wate hook:", resp.Error)
	}
	return 0
}

func claudeSettingsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "settings.json")
}

// parseRatio reads how big a pane should be: a fraction, or one of the three sizes worth a name.
func parseRatio(w string) (float64, bool) {
	switch w {
	case "third", "1/3":
		return 1.0 / 3.0, true
	case "half", "1/2":
		return 0.5, true
	case "two-thirds", "2/3":
		return 2.0 / 3.0, true
	}
	f, err := strconv.ParseFloat(w, 64)
	if err != nil || f <= 0 || f >= 1 {
		return 0, false
	}
	return f, true
}
