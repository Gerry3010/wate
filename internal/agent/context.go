package agent

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Context is how full a Claude Code session's context window is, read from its transcript.
type Context struct {
	// Tokens currently in the context (input + cache read + cache creation of the latest turn).
	Tokens int `json:"tokens"`
	// Window is the model's context size in tokens (best guess, see WindowFor).
	Window int    `json:"window"`
	Model  string `json:"model"`
}

// Percent of the window in use (0 when unknown).
func (c Context) Percent() int {
	if c.Window <= 0 || c.Tokens <= 0 {
		return 0
	}
	return int(float64(c.Tokens)*100/float64(c.Window) + 0.5)
}

// tailBytes is how much of the transcript we scan backwards; assistant turns are frequent.
const tailBytes = 512 * 1024

// ReadContext finds the newest assistant message with token usage in a Claude Code transcript
// (`~/.claude/projects/<slug>/<session>.jsonl`). ok is false when the file has no usage yet.
func ReadContext(path string) (c Context, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return c, false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return c, false
	}
	start := st.Size() - tailBytes
	if start < 0 {
		start = 0
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return c, false
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return c, false
	}
	lines := bytes.Split(data, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		if !bytes.Contains(line, []byte(`"type":"assistant"`)) || !bytes.Contains(line, []byte(`"usage"`)) {
			continue
		}
		var entry struct {
			Type    string `json:"type"`
			Message struct {
				Model string `json:"model"`
				Usage struct {
					Input         int `json:"input_tokens"`
					CacheCreation int `json:"cache_creation_input_tokens"`
					CacheRead     int `json:"cache_read_input_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &entry) != nil || entry.Type != "assistant" {
			continue
		}
		u := entry.Message.Usage
		tokens := u.Input + u.CacheCreation + u.CacheRead
		if tokens == 0 {
			continue
		}
		return Context{Tokens: tokens, Model: entry.Message.Model}, true
	}
	return c, false
}

// WindowFor guesses the context window: the configured size wins; otherwise 200k, or 1M for
// models known to have it — and never less than what the transcript already shows in use.
func WindowFor(model string, configured, observed int) int {
	w := configured
	if w <= 0 {
		w = 200_000
		m := strings.ToLower(model)
		if strings.Contains(m, "fable") || strings.Contains(m, "mythos") || strings.Contains(m, "[1m]") || strings.HasSuffix(m, "-1m") {
			w = 1_000_000
		}
	}
	for observed > w {
		// Beyond the assumed window: the model must have a bigger one.
		if w < 1_000_000 {
			w = 1_000_000
		} else {
			w *= 2
		}
	}
	return w
}

// FindTranscript guesses the transcript of a session without hook information: the newest
// `.jsonl` in Claude Code's project directory for cwd (`~/.claude/projects/<cwd with / → ->`).
func FindTranscript(home, cwd string) string {
	if cwd == "" {
		return ""
	}
	slug := strings.NewReplacer("/", "-", ".", "-", " ", "-").Replace(cwd)
	dir := filepath.Join(home, ".claude", "projects", slug)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	best, bestTime := "", int64(0)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if t := info.ModTime().UnixNano(); t > bestTime {
			best, bestTime = filepath.Join(dir, e.Name()), t
		}
	}
	return best
}
