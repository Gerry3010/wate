package tbsock

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"testing"
)

func TestServerAnswersTheWayTerminalBrowserReads(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "tb.sock")
	s, err := Listen(sock, func(r Request) (any, error) {
		switch r.Method {
		case "hello":
			return map[string]any{"name": "wate", "methods": []string{"split"}}, nil
		case "split":
			return nil, nil
		}
		return nil, errors.New("no such method " + r.Method)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// terminal-browser opens a connection per call, writes one line and reads one back.
	call := func(line string) map[string]any {
		t.Helper()
		c, err := net.Dial("unix", sock)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		if _, err := c.Write([]byte(line + "\n")); err != nil {
			t.Fatal(err)
		}
		got, err := bufio.NewReader(c).ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(got), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	hello := call(`{"id":"a1","method":"hello","params":{"protocol":1},"caller":{"pid":7,"cwd":"/"}}`)
	if hello["id"] != "a1" || hello["result"].(map[string]any)["name"] != "wate" {
		t.Errorf("hello: %v", hello)
	}
	// A call with nothing to return still answers with an object: split's result is checked
	// against z.object({}), which an absent result fails.
	split := call(`{"id":"a2","method":"split","params":{}}`)
	if res, ok := split["result"].(map[string]any); !ok || len(res) != 0 || split["error"] != nil {
		t.Errorf("split: %v", split)
	}
	bad := call(`{"id":"a3","method":"nope"}`)
	if bad["id"] != "a3" || bad["error"].(map[string]any)["message"] != "no such method nope" || bad["result"] != nil {
		t.Errorf("unknown method: %v", bad)
	}
}

func TestSocketIsNotTakenForAControlSocket(t *testing.T) {
	// ctl.FindSocket globs wate-*.sock and deletes what has no live pid in its name.
	name := filepath.Base(SocketPath("/run/user/1000", 4242))
	if ok, _ := filepath.Match("wate-*.sock", name); ok {
		t.Fatalf("%s looks like a control socket", name)
	}
}
