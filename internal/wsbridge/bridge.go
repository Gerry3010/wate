// Package wsbridge streams PTY bytes to the webview over a loopback WebSocket.
//
// Wails' event bus is JSON-over-IPC and too slow for terminal throughput, so each
// pane opens one binary WebSocket to 127.0.0.1:<random port>/pty/<session id>?token=<token>.
// Binary frames carry raw bytes both ways; text frames are small JSON control
// messages from the client (currently only {"type":"resize","cols":N,"rows":N}).
package wsbridge

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/Gerry3010/wate/internal/pty"
)

type control struct {
	Type string `json:"type"`
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

// Server is the loopback WebSocket endpoint.
type Server struct {
	sessions *pty.Manager
	token    string
	ln       net.Listener
	srv      *http.Server
}

func New(sessions *pty.Manager) (*Server, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	s := &Server{sessions: sessions, token: hex.EncodeToString(buf), ln: ln}
	mux := http.NewServeMux()
	mux.HandleFunc("/pty/", s.handle)
	s.srv = &http.Server{Handler: mux}
	go func() { _ = s.srv.Serve(ln) }()
	return s, nil
}

// Port the listener is bound to.
func (s *Server) Port() int { return s.ln.Addr().(*net.TCPAddr).Port }

// Token clients must present; handed to the frontend through a bound service method only.
func (s *Server) Token() string { return s.token }

func (s *Server) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return s.srv.Shutdown(ctx)
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("token") != s.token {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/pty/")
	sess, ok := s.sessions.Get(id)
	if !ok {
		http.Error(w, "no such session", http.StatusNotFound)
		return
	}
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// Origin is the webview's scheme (wails://, http://wails.localhost, or the Vite dev server).
		InsecureSkipVerify: true,
		CompressionMode:    websocket.CompressionDisabled,
	})
	if err != nil {
		return
	}
	// ctx ends when the client goes away; ptyEOF fires when the child closes its side.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	ptyEOF := make(chan struct{})

	// PTY → socket
	go func() {
		defer close(ptyEOF)
		buf := make([]byte, 32*1024)
		for {
			n, err := sess.File().Read(buf)
			if n > 0 {
				if werr := c.Write(ctx, websocket.MessageBinary, buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// socket → PTY
	go func() {
		defer cancel()
		for {
			typ, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			switch typ {
			case websocket.MessageBinary:
				if _, err := sess.File().Write(data); err != nil {
					return
				}
			case websocket.MessageText:
				var msg control
				if err := json.Unmarshal(data, &msg); err != nil {
					continue
				}
				if msg.Type == "resize" && msg.Cols > 0 && msg.Rows > 0 {
					if err := sess.Resize(msg.Cols, msg.Rows); err != nil {
						slog.Debug("resize failed", "id", id, "err", err)
					}
				}
			}
		}
	}()

	select {
	case <-ptyEOF:
		// Let the child finish so the exit code is known, but don't hang on a stuck process.
		select {
		case <-sess.Done():
		case <-time.After(2 * time.Second):
		}
		_ = c.Close(websocket.StatusNormalClosure, fmt.Sprintf("exit:%d", sess.ExitCode()))
	case <-ctx.Done():
		_ = c.Close(websocket.StatusGoingAway, "closed")
	}
}

// readAll is a test helper; kept unexported.
