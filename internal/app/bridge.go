package app

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// PaneRequest is emitted as "pane:request": a question for one window's frontend, which answers
// by calling PaneBridge.Reply with the same id.
type PaneRequest struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Pane string `json:"pane"`
	// Data is the request's arguments as a JSON object, opaque to Go.
	Data string `json:"data,omitempty"`
}

// askTimeout is how long a question waits for the WebView.
//
// It has to stay comfortably under the ctl client's own five seconds (see ctl.Send), so that a
// caller gets a clean "the window did not answer" instead of running into its deadline and
// losing the connection with nothing to show for it.
const askTimeout = 2500 * time.Millisecond

type paneReply struct {
	data string
	err  string
}

// PaneBridge asks the frontend something and waits for the answer.
//
// Everything else in this app talks to the frontend by emitting an event and moving on, because
// everything else is a command. A few things are questions — what is on this pane's screen? —
// and those need the answer to come back. The event carries an id, the answer carries it again,
// and this type keeps the channel in between.
type PaneBridge struct {
	mu      sync.Mutex
	waiting map[string]chan paneReply
}

func NewPaneBridge() *PaneBridge {
	return &PaneBridge{waiting: map[string]chan paneReply{}}
}

func (b *PaneBridge) ServiceName() string { return "PaneBridge" }

// ask puts a question to one window and waits for its answer.
//
// Unexported on purpose: an exported method taking a *WebviewWindow would drag Wails' own types
// into the generated bindings and collide with the window service's models.
func (b *PaneBridge) ask(w *application.WebviewWindow, kind, pane, data string) (string, error) {
	id := uuid.NewString()
	// Buffered, so a reply that lands just after the timeout neither blocks its caller nor
	// leaks a goroutine.
	ch := make(chan paneReply, 1)
	b.mu.Lock()
	b.waiting[id] = ch
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.waiting, id)
		b.mu.Unlock()
	}()

	emitTo(w, "pane:request", PaneRequest{ID: id, Kind: kind, Pane: pane, Data: data})

	select {
	case r := <-ch:
		if r.err != "" {
			return "", errors.New(r.err)
		}
		return r.data, nil
	case <-time.After(askTimeout):
		return "", errors.New("the window did not answer in time")
	}
}

// Reply hands an answer back. An id nobody is waiting for any more — the question timed out, or
// the window answered twice — is dropped without a word; the caller has already given up.
func (b *PaneBridge) Reply(id string, data string, errMsg string) {
	b.mu.Lock()
	ch, ok := b.waiting[id]
	if ok {
		delete(b.waiting, id)
	}
	b.mu.Unlock()
	if !ok {
		return
	}
	ch <- paneReply{data: data, err: errMsg}
}

// ServiceStartup satisfies Wails' service interface.
func (b *PaneBridge) ServiceStartup(context.Context, application.ServiceOptions) error { return nil }
