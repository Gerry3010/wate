package agent

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Context is how full a Claude Code session's context window is, read from its transcript.
type Context struct {
	// Tokens currently in the context (input + cache read + cache creation of the latest turn).
	Tokens int `json:"tokens"`
	// Window is the model's context size in tokens (see WindowFor).
	Window int    `json:"window"`
	Model  string `json:"model"`
	// The latest turn's usage: what Tokens is made of, plus what the model wrote.
	Input         int `json:"input"`
	CacheCreation int `json:"cache_creation"`
	CacheRead     int `json:"cache_read"`
	Output        int `json:"output"`
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
					Output        int `json:"output_tokens"`
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
		return Context{Tokens: tokens, Model: entry.Message.Model, Input: u.Input, CacheCreation: u.CacheCreation, CacheRead: u.CacheRead, Output: u.Output}, true
	}
	return c, false
}

// WindowFor is the context window a session has: the configured size wins; otherwise the one
// Claude Code gives the model (NativeWindow), and never less than what the transcript already
// shows in use.
func WindowFor(model string, configured, observed int) int {
	w := configured
	if w <= 0 {
		w = NativeWindow(model)
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

// modelVersion splits a model id such as claude-opus-4-7 or claude-haiku-4-5-20251001 into family,
// major and minor version; a date is no minor version.
var modelVersion = regexp.MustCompile(`^claude-(opus|sonnet|haiku|fable|mythos)-(\d+)(?:-(\d{1,2}))?(?:$|[-\[])`)

// NativeWindow is the context window Claude Code opens a model with, after its own model table
// (Claude Code 2.1.322): Opus from 4.7, Sonnet and Haiku from 5, and every Fable and Mythos have
// 1M from the first turn. They do not start at 200k and switch over. The older models have 200k
// unless picked with the [1m] suffix, which the transcript does not record, so such a session
// only shows its 1M by growing past 200k (see WindowFor). With
// CLAUDE_CODE_DISABLE_1M_CONTEXT set, Claude Code keeps every model at 200k; [claude]
// context_window = 200000 tells wate so.
func NativeWindow(model string) int {
	m := strings.ToLower(model)
	if strings.Contains(m, "[1m]") || strings.HasSuffix(m, "-1m") || strings.Contains(m, "fable") || strings.Contains(m, "mythos") {
		return 1_000_000
	}
	g := modelVersion.FindStringSubmatch(m)
	if g == nil {
		return 200_000
	}
	major, _ := strconv.Atoi(g[2])
	minor, _ := strconv.Atoi(g[3])
	switch g[1] {
	case "opus":
		if major > 4 || major == 4 && minor >= 7 {
			return 1_000_000
		}
	case "sonnet", "haiku":
		if major >= 5 {
			return 1_000_000
		}
	}
	return 200_000
}

// FindTranscript guesses the transcript of a session without hook information: the newest
// `.jsonl` in Claude Code's project directory for cwd (`~/.claude/projects/<cwd with / → ->`)
// that was written after since and is not taken.
func FindTranscript(home, cwd string, since time.Time, taken map[string]bool) string {
	if cwd == "" {
		return ""
	}
	slug := strings.NewReplacer("/", "-", ".", "-", " ", "-").Replace(cwd)
	dir := filepath.Join(home, ".claude", "projects", slug)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	best, bestTime := "", since
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") || taken[path] {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if t := info.ModTime(); t.After(bestTime) {
			best, bestTime = path, t
		}
	}
	return best
}
