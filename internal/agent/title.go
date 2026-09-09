package agent

import (
	"encoding/json"
	"io"
	"os"
	"strings"
)

// headBytes is how much of a transcript we scan for a title; the interesting entries
// (the compaction summary and the first prompt) are at the start.
const headBytes = 256 * 1024

// titleLen caps the title at a length that still fits a pane-wide notice.
const titleLen = 64

// ReadTitle names a Claude Code session the way `claude --resume` does: its summary when the
// session was compacted, otherwise the first thing the user asked. "" when the transcript
// holds neither yet.
func ReadTitle(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, headBytes))
	if err != nil {
		return ""
	}
	prompt := ""
	for _, line := range strings.Split(string(data), "\n") {
		if len(line) < 2 || line[0] != '{' {
			continue
		}
		var entry struct {
			Type    string `json:"type"`
			Summary string `json:"summary"`
			Message struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal([]byte(line), &entry) != nil {
			continue
		}
		switch entry.Type {
		case "summary":
			if s := clean(entry.Summary); s != "" {
				return s
			}
		case "user":
			if prompt == "" {
				prompt = clean(userText(entry.Message.Content))
			}
		}
	}
	return prompt
}

// userText pulls the plain text out of a user message: content is either a string or a list
// of blocks. Tool results and the CLI's own injected blocks are not prompts.
func userText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	for _, b := range blocks {
		if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
			return b.Text
		}
	}
	return ""
}

// clean turns a message into a one-line title, dropping the wrappers Claude Code adds around
// slash commands, hook output and system notes.
func clean(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	for _, skip := range []string{"<", "Caveat:", "This session is being continued", "[Request interrupted"} {
		if strings.HasPrefix(s, skip) {
			return ""
		}
	}
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return truncate(s, titleLen)
}
