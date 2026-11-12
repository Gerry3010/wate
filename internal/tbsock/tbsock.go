// Package tbsock serves terminal-browser's terminal socket: the Unix socket a terminal names in
// $TERMINAL_BROWSER_TERMINAL_SOCKET so that terminal-browser asks it, rather than guessing from
// its list of known terminals, which pane it is in and how to open one beside it.
//
// The protocol is terminal-browser's (shared/src/terminal-socket.ts in its sources, protocol 1):
// one JSON line per request, {"id", "method", "params", "caller": {"pid", "cwd"}}, answered with
// {"id", "result"} or {"id", "error": {"message"}}.
package tbsock

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Env is the variable terminal-browser looks for.
const Env = "TERMINAL_BROWSER_TERMINAL_SOCKET"

// SocketPath is where wate process pid serves terminal-browser, in dir (where its control
// socket is). Not wate-*.sock: `wate ctl` takes those for control sockets, and one whose name
// holds no pid for a stale one it may delete.
func SocketPath(dir string, pid int) string {
	return filepath.Join(dir, "tb-wate-"+strconv.Itoa(pid)+".sock")
}

// Request is one call.
type Request struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Caller struct {
		Pid int    `json:"pid"`
		Cwd string `json:"cwd"`
	} `json:"caller"`
}

type reply struct {
	ID     string    `json:"id"`
	Result any       `json:"result,omitempty"`
	Error  *replyErr `json:"error,omitempty"`
}

type replyErr struct {
	Message string `json:"message"`
}

// Handler answers a call with its result (an object: terminal-browser checks its shape) or an
// error, whose text terminal-browser shows the user.
type Handler func(Request) (any, error)

// Server listens on the socket.
type Server struct {
	ln   net.Listener
	path string
}

// Listen serves h on a socket at path, which only this user may use.
func Listen(path string, h Handler) (*Server, error) {
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	_ = os.Chmod(path, 0o600)
	s := &Server{ln: ln, path: path}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go serve(c, h)
		}
	}()
	return s, nil
}

func (s *Server) Path() string { return s.path }

func (s *Server) Close() error {
	err := s.ln.Close()
	_ = os.Remove(s.path)
	return err
}

func serve(c net.Conn, h Handler) {
	defer c.Close()
	sc := bufio.NewScanner(c)
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	enc := json.NewEncoder(c)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		_ = enc.Encode(answer(line, h))
	}
}

// answer runs one request line through h.
func answer(line string, h Handler) reply {
	var req Request
	if err := json.Unmarshal([]byte(line), &req); err != nil {
		return reply{Error: &replyErr{Message: "bad request: " + err.Error()}}
	}
	result, err := h(req)
	if err != nil {
		return reply{ID: req.ID, Error: &replyErr{Message: err.Error()}}
	}
	if result == nil {
		result = struct{}{}
	}
	return reply{ID: req.ID, Result: result}
}
