package agent

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
)

// askTail is how much of the transcript ReadAsking looks at. Only the last message matters,
// and that is never far from the end.
const askTail = 64 * 1024

// ReadAsking reports whether a session's last word was a question for the user: an
// AskUserQuestion nobody has answered yet, or a reply that ends on a question mark.
//
// Read from the transcript rather than guessed from the hooks, because no hook says it: a
// turn that ends on "Shall I push it?" stops exactly like one that ends on "Done.".
func ReadAsking(path string) bool {
	if path == "" {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return false
	}
	start := max(st.Size()-askTail, 0)
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return false
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return false
	}
	lines := bytes.Split(data, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		var entry struct {
			Type    string `json:"type"`
			IsMeta  bool   `json:"isMeta"`
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if len(lines[i]) == 0 || json.Unmarshal(lines[i], &entry) != nil {
			continue
		}
		switch entry.Type {
		case "user":
			// Hook output and system notes are filed as user lines too; they are not an answer.
			if entry.IsMeta {
				continue
			}
			// Anything the user (or a tool result) said after Claude's last word answers it.
			return false
		case "assistant":
			asking, decided := assistantAsks(entry.Message.Content)
			if decided {
				return asking
			}
			// Thinking only: the words are in an earlier line of the same message.
		}
	}
	return false
}

// assistantAsks reads one assistant line. decided is false when the line carries nothing to
// go by (a thinking block on its own).
func assistantAsks(raw json.RawMessage) (asking, decided bool) {
	var blocks []struct {
		Type string `json:"type"`
		Name string `json:"name"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		var text string
		if json.Unmarshal(raw, &text) != nil {
			return false, false
		}
		return endsOnQuestion(text), true
	}
	for i := len(blocks) - 1; i >= 0; i-- {
		b := blocks[i]
		switch b.Type {
		case "tool_use":
			// An open AskUserQuestion is a question by definition; any other tool waiting here
			// is a permission prompt, which is input of a different kind.
			return b.Name == "AskUserQuestion", true
		case "text":
			if strings.TrimSpace(b.Text) != "" {
				return endsOnQuestion(b.Text), true
			}
		}
	}
	return false, false
}

func endsOnQuestion(s string) bool {
	s = strings.TrimRight(strings.TrimSpace(s), "*_`) \n")
	return strings.HasSuffix(s, "?") || strings.HasSuffix(s, "？")
}
