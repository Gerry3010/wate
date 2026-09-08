package app

import (
	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/Gerry3010/wate/internal/opener"
)

// OpenerService resolves and opens click targets for the frontend.
type OpenerService struct{}

func (OpenerService) ServiceName() string { return "OpenerService" }

// Resolve classifies terminal text candidates relative to cwd (batched per line).
func (OpenerService) Resolve(cwd string, raws []string) []opener.Target {
	out := make([]opener.Target, len(raws))
	for i, r := range raws {
		out[i] = opener.Resolve(cwd, r)
	}
	return out
}

// Open hands a path to the default application.
func (OpenerService) Open(path string) error { return opener.OpenExternal(path) }

// OpenURL opens a URL in the default browser.
func (OpenerService) OpenURL(url string) error { return application.Get().Browser.OpenURL(url) }
