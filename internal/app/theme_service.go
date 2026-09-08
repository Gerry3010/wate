package app

import (
	"path/filepath"

	"github.com/Gerry3010/yate/internal/config"
	"github.com/Gerry3010/yate/internal/theme"
)

// ThemeService resolves the configured theme for the frontend.
type ThemeService struct {
	cfg func() config.Config
}

func NewThemeService(cfg func() config.Config) *ThemeService { return &ThemeService{cfg: cfg} }

func (t *ThemeService) ServiceName() string { return "ThemeService" }

func userThemesDir() string { return filepath.Join(config.Dir(), "themes") }

// Get resolves the active theme; falls back to the default theme on error.
func (t *ThemeService) Get() (theme.Resolved, error) {
	name := t.cfg().General.Theme
	r, err := theme.Load(name, userThemesDir())
	if err != nil {
		fallback, ferr := theme.Load("catppuccin-mocha", "")
		if ferr != nil {
			return theme.Resolved{}, ferr
		}
		return fallback, err
	}
	return r, nil
}

// List returns the ids of all available themes.
func (t *ThemeService) List() []string { return theme.List(userThemesDir()) }

// Preview resolves any theme by id (for swatches in the settings pane).
func (t *ThemeService) Preview(id string) (theme.Resolved, error) {
	return theme.Load(id, userThemesDir())
}
