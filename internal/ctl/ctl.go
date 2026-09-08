// Package ctl is yate's control socket: a Unix socket speaking JSON lines that
// the yate CLI (and Claude Code hooks) use to talk to the running app.
package ctl

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Request is one command line.
type Request struct {
	Cmd string `json:"cmd"`
	// Pane targets a specific pane (YATE_PANE_ID); empty means the focused one.
	Pane string `json:"pane,omitempty"`
	Tab  string `json:"tab,omitempty"`
	// Text for "input"; Path/Line/Col for "open"; Name for "action"; Event/Data for "hook".
	Text  string          `json:"text,omitempty"`
	Path  string          `json:"path,omitempty"`
	Line  int             `json:"line,omitempty"`
	Col   int             `json:"col,omitempty"`
	Name  string          `json:"name,omitempty"`
	Event string          `json:"event,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
}

// Response is the reply line.
type Response struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	Data  any    `json:"data,omitempty"`
}

// Handler processes one request.
type Handler func(Request) Response

// SocketPath returns the socket for this app instance.
func SocketPath(pid int) string {
	return filepath.Join(runtimeDir(), fmt.Sprintf("yate-%d.sock", pid))
}

func runtimeDir() string {
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		return d
	}
	if runtime.GOOS == "darwin" {
		if d := os.Getenv("TMPDIR"); d != "" {
			return d
		}
	}
	return os.TempDir()
}

// Server listens on the socket and dispatches to h.
type Server struct {
	ln   net.Listener
	path string
}

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
			go s.serve(c, h)
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

func (s *Server) serve(c net.Conn, h Handler) {
	defer c.Close()
	sc := bufio.NewScanner(c)
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	enc := json.NewEncoder(c)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var req Request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			_ = enc.Encode(Response{Error: "bad request: " + err.Error()})
			continue
		}
		_ = enc.Encode(h(req))
	}
}

// FindSocket picks the socket to talk to: $YATE_SOCKET, else the newest live yate-*.sock.
func FindSocket() (string, error) {
	if p := os.Getenv("YATE_SOCKET"); p != "" {
		return p, nil
	}
	matches, _ := filepath.Glob(filepath.Join(runtimeDir(), "yate-*.sock"))
	var live []string
	for _, m := range matches {
		pidStr := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(m), "yate-"), ".sock")
		if pid, err := strconv.Atoi(pidStr); err == nil && processAlive(pid) {
			live = append(live, m)
		} else {
			_ = os.Remove(m) // stale
		}
	}
	if len(live) == 0 {
		return "", errors.New("no running yate found (is YATE_SOCKET set?)")
	}
	sort.Slice(live, func(i, j int) bool {
		si, _ := os.Stat(live[i])
		sj, _ := os.Stat(live[j])
		return si.ModTime().After(sj.ModTime())
	})
	return live[0], nil
}

func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

// Send delivers one request and returns the response.
func Send(path string, req Request) (Response, error) {
	c, err := net.DialTimeout("unix", path, 2*time.Second)
	if err != nil {
		return Response{}, err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	if err := json.NewEncoder(c).Encode(req); err != nil {
		return Response{}, err
	}
	var resp Response
	if err := json.NewDecoder(c).Decode(&resp); err != nil {
		return Response{}, err
	}
	return resp, nil
}
