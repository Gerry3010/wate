package app

import (
	"context"
	"log/slog"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/Gerry3010/yate/internal/config"
)

// ConfigService exposes the effective configuration to the frontend.
type ConfigService struct {
	path string
	mu   sync.RWMutex
	cfg  config.Config
	// Warning is a non-fatal problem with the config file (unknown keys), shown in the UI.
	warning string
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
	Config  config.Config `json:"config"`
	Path    string        `json:"path"`
	Warning string        `json:"warning"`
	OS      string        `json:"os"`
}

// Get returns the effective config plus metadata.
func (c *ConfigService) Get() ConfigResponse {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return ConfigResponse{Config: c.cfg, Path: c.path, Warning: c.warning, OS: goos}
}

// Reload re-reads the file and broadcasts config:changed.
func (c *ConfigService) Reload() ConfigResponse {
	c.reload()
	resp := c.Get()
	application.Get().Event.Emit("config:changed", resp)
	return resp
}
