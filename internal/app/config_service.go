package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/Gerry3010/wate/internal/config"
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
	cfg := c.Current()
	cmd := cfg.Claude.Command
	if cmd == "" {
		cmd = "claude"
	}
	args := strings.Fields(cmd)
	if resume != "" {
		args = append(args, "--resume", resume)
	}
	if cfg.Claude.MCP {
		if exe, err := os.Executable(); err == nil {
			spec := map[string]any{"mcpServers": map[string]any{
				"wate": map[string]any{"command": exe, "args": []string{"mcp"}},
			}}
			if blob, err := json.Marshal(spec); err == nil {
				args = append(args, "--mcp-config", string(blob))
			}
		}
	}
	return args
}
