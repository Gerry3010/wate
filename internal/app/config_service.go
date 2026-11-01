package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/Gerry3010/wate/internal/config"
	"github.com/Gerry3010/wate/internal/root"
)

// ConfigService exposes the effective configuration to the frontend.
type ConfigService struct {
	path string
	mu   sync.RWMutex
	cfg  config.Config
	// Warning is a non-fatal problem with the config file (unknown keys), shown in the UI.
	warning string
	// InitialCwd is the directory given on the command line for the first tab.
	InitialCwd string
	// Secondary is true when another wate was already running at startup: this window starts
	// empty at the default size and leaves the saved session and geometry to the first one.
	Secondary bool
	// OnChange, if set, runs after every reload (used to sync native chrome with the theme).
	OnChange func()
}

func NewConfigService(path string) *ConfigService {
	s := &ConfigService{path: path}
	s.reload()
	return s
}

func (c *ConfigService) ServiceName() string { return "ConfigService" }

func (c *ConfigService) ServiceStartup(context.Context, application.ServiceOptions) error {
	if err := config.WriteDefaultIfMissing(c.path); err != nil {
		slog.Warn("could not write default config", "path", c.path, "err", err)
	}
	return nil
}

func (c *ConfigService) reload() {
	cfg, err := config.Load(c.path)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cfg = cfg
	c.warning = ""
	if err != nil {
		c.warning = err.Error()
		slog.Warn("config", "err", err)
	}
}

// Current returns the effective config (used by other services).
func (c *ConfigService) Current() config.Config {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cfg
}

// ConfigResponse is what the frontend receives.
type ConfigResponse struct {
	Config     config.Config `json:"config"`
	Path       string        `json:"path"`
	Warning    string        `json:"warning"`
	OS         string        `json:"os"`
	InitialCwd string        `json:"initial_cwd"`
	Secondary  bool          `json:"secondary"`
}

// Get returns the effective config plus metadata.
func (c *ConfigService) Get() ConfigResponse {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return ConfigResponse{Config: c.cfg, Path: c.path, Warning: c.warning, OS: goos, InitialCwd: c.InitialCwd, Secondary: c.Secondary}
}

// Reload re-reads the file and broadcasts config:changed.
func (c *ConfigService) Reload() ConfigResponse {
	c.reload()
	resp := c.Get()
	application.Get().Event.Emit("config:changed", resp)
	if c.OnChange != nil {
		c.OnChange()
	}
	return resp
}

// Set writes values into the config file and applies them immediately.
func (c *ConfigService) Set(values map[string]any) (ConfigResponse, error) {
	if err := config.Set(c.path, values); err != nil {
		return c.Get(), err
	}
	return c.Reload(), nil
}

// ClaudeArgs is the command line for starting Claude Code in a pane.
//
// Composed here rather than in the frontend because of the --mcp-config part: it needs this
// binary's own path. The server is handed to the sessions wate starts and to nobody else, so
// the user's ~/.claude/settings.json stays as they left it and a Claude running outside a pane
// is not offered tools that would fail. Note the absence of --strict-mcp-config: that would
// switch off the user's own MCP servers for the session, which is not wate's call to make.
func (c *ConfigService) ClaudeArgs(resume string) []string {
	return c.claudeArgs(resume, "", "")
}

// claudeArgs is ClaudeArgs with a first prompt and a session name (both may be empty).
//
// The prompt goes straight after the command: options like --mcp-config take any number of
// values and would swallow a prompt standing behind them.
func (c *ConfigService) claudeArgs(resume, prompt, name string) []string {
	cfg := c.Current()
	args := claudeCommand(cfg)
	if prompt != "" {
		args = append(args, prompt)
	}
	if resume != "" {
		args = append(args, "--resume", resume)
	}
	if name != "" {
		args = append(args, "--name", name)
	}
	if cfg.Claude.MCP {
		if exe, err := os.Executable(); err == nil {
			args = append(args, "--mcp-config", mcpConfig(exe, false))
		}
	}
	return args
}

// RootArgs is the command line for the root agent's pane. Its session is continued when there
// is one to continue, so restarting wate does not cost it what it knew.
func (c *ConfigService) RootArgs() ([]string, error) {
	cfg := c.Current()
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	id, resume, err := rootSession(config.StateDir(), func(id string) bool {
		_, err := TranscriptPath(id)
		return err == nil
	})
	if err != nil {
		return nil, err
	}
	custom := ""
	if p := cfg.Root.Instructions; p != "" {
		if b, err := os.ReadFile(p); err == nil {
			custom = string(b)
		} else {
			slog.Warn("root: instructions not readable, using the built-in ones", "path", p, "err", err)
		}
	}
	watch := watchCommand()
	return root.Args(cfg, id, resume, root.Instructions(custom, watch), watch, mcpConfig(exe, true)), nil
}

// rootSession is the root agent's session id: the saved one while its transcript is there to
// continue, else a fresh one, saved for next time.
func rootSession(stateDir string, exists func(string) bool) (id string, resume bool, err error) {
	path := filepath.Join(stateDir, "root.json")
	var saved struct {
		SessionID string `json:"session_id"`
	}
	if b, err := os.ReadFile(path); err == nil && json.Unmarshal(b, &saved) == nil &&
		saved.SessionID != "" && exists(saved.SessionID) {
		return saved.SessionID, true, nil
	}
	saved.SessionID = uuid.NewString()
	b, _ := json.Marshal(saved)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return "", false, err
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return "", false, err
	}
	return saved.SessionID, false, nil
}

// claudeCommand is the configured command for Claude Code, split into words.
func claudeCommand(cfg config.Config) []string {
	cmd := cfg.Claude.Command
	if cmd == "" {
		cmd = "claude"
	}
	return strings.Fields(cmd)
}

// mcpConfig is the --mcp-config value that hands a session wate's tools: the pane tools, and
// for the root agent its own on top.
func mcpConfig(exe string, root bool) string {
	args := []string{"mcp"}
	if root {
		args = append(args, "--root")
	}
	blob, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{
		"wate": map[string]any{"command": exe, "args": args},
	}})
	return string(blob)
}
