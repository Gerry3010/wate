package root

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Trusted reports whether Claude Code trusts dir: whether "do you trust the files in this
// folder?" was answered with yes for it or for a directory above it. A session started in a
// directory that is not trusted stops on that question before anything else — before any hook
// fires, so wate cannot see it waiting. When Claude Code's state cannot be read the answer is
// yes: there is nothing to go on, and a wrong answer either way costs no more than a tab that
// does or does not come to the front.
func Trusted(dir string) bool {
	return trustedIn(stateFile(), dir)
}

// stateFile is where Claude Code keeps per-directory state, the trust answers among it.
func stateFile() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, ".claude.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude.json")
}

func trustedIn(stateFile, dir string) bool {
	data, err := os.ReadFile(stateFile)
	if err != nil {
		return true
	}
	var st struct {
		Projects map[string]struct {
			Trusted bool `json:"hasTrustDialogAccepted"`
		} `json:"projects"`
	}
	if json.Unmarshal(data, &st) != nil {
		return true
	}
	dir = filepath.Clean(dir)
	for {
		if p, ok := st.Projects[dir]; ok && p.Trusted {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}
