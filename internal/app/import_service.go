package app

import (
	"fmt"

	"github.com/Gerry3010/wate/internal/config"
	"github.com/Gerry3010/wate/internal/importer"
)

// ImportService lets the settings pane pull theme, font, window and tab settings from other
// terminal emulators installed on this machine.
type ImportService struct {
	cfg *ConfigService
}

func NewImportService(cfg *ConfigService) *ImportService { return &ImportService{cfg: cfg} }

func (ImportService) ServiceName() string { return "ImportService" }

func (s *ImportService) env() importer.Env { return importer.SystemEnv(userThemesDir()) }

// Detect lists terminals with importable configuration.
func (s *ImportService) Detect() []importer.Source {
	return importer.Detect(s.env())
}

// Apply imports the chosen items: settings are written to config.toml and applied live,
// a theme lands in the user themes dir, tabs are returned for the frontend to open.
func (s *ImportService) Apply(source string, keys []string) (importer.Result, error) {
	res, err := importer.Apply(s.env(), source, keys)
	if err != nil {
		return res, err
	}
	if len(res.Settings) > 0 {
		if err := config.Set(s.cfg.path, res.Settings); err != nil {
			return res, fmt.Errorf("write config: %w", err)
		}
		s.cfg.Reload()
	}
	return res, nil
}
