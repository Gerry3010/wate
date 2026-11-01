package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Gerry3010/wate/internal/ctl"
)

// watcher is `wate ctl watch`: the root agent's alarm clock. It asks wate for what deserves the
// root agent's attention every couple of seconds and prints each event as one JSON line.
//
// With once it ends after the first batch. That ending is the point: the root agent runs it as
// a background command, and a background command finishing is what wakes an idle Claude Code
// session. The root agent deals with the batch and starts the next watch. Nothing is lost in
// between — wate keeps the queue, not the watcher.
type watcher struct {
	sock  string
	token string
	// every is the pause between two questions.
	every time.Duration
	// gather is how long a first event waits for others: what goes wrong tends to go wrong in
	// bunches, and one wake-up for the bunch is cheaper than one per event.
	gather time.Duration
	// giveUp is how long wate may be unreachable before the watch fails. wate going away
	// takes the root agent with it, so this is for the odd hiccup, not for restarts.
	giveUp time.Duration
	out    io.Writer
	errOut io.Writer
	now    func() time.Time
	sleep  func(time.Duration)
}

func runWatch(args []string) int {
	once := false
	for _, a := range args {
		switch a {
		case "--once":
			once = true
		default:
			fmt.Fprintf(os.Stderr, "wate: watch does not understand %q\n", a)
			return 2
		}
	}
	sock, err := ctl.FindSocket()
	if err != nil {
		fmt.Fprintln(os.Stderr, "wate:", err)
		return 1
	}
	w := watcher{
		sock: sock, token: os.Getenv("WATE_PANE_TOKEN"),
		every: 2 * time.Second, gather: 2 * time.Second, giveUp: 30 * time.Second,
		out: os.Stdout, errOut: os.Stderr, now: time.Now, sleep: time.Sleep,
	}
	return w.run(once)
}

func (w watcher) run(once bool) int {
	var failingSince, firstAt time.Time
	var batch []json.RawMessage
	for {
		events, err := w.ask()
		switch {
		case err == errRefused:
			return 1
		case err != nil:
			if failingSince.IsZero() {
				failingSince = w.now()
			}
			if w.now().Sub(failingSince) >= w.giveUp {
				w.print(batch)
				fmt.Fprintln(w.errOut, "wate: watch gave up:", err)
				return 1
			}
		default:
			failingSince = time.Time{}
			if len(events) > 0 && firstAt.IsZero() {
				firstAt = w.now()
			}
			batch = append(batch, events...)
		}
		if len(batch) > 0 {
			if !once {
				w.print(batch)
				batch, firstAt = nil, time.Time{}
			} else if w.now().Sub(firstAt) >= w.gather {
				w.print(batch)
				return 0
			}
		}
		w.sleep(w.every)
	}
}

// errRefused is wate saying no: there is no point asking again.
var errRefused = fmt.Errorf("refused")

// ask fetches the pending events.
func (w watcher) ask() ([]json.RawMessage, error) {
	resp, err := ctl.Send(w.sock, ctl.Request{Cmd: "attention", Token: w.token})
	if err != nil {
		return nil, err
	}
	if !resp.OK {
		fmt.Fprintln(w.errOut, "wate:", resp.Error)
		return nil, errRefused
	}
	// Data arrives decoded into Go's generic types; one round trip back to JSON gives the
	// events as they were sent.
	blob, err := json.Marshal(resp.Data)
	if err != nil {
		return nil, err
	}
	var events []json.RawMessage
	if err := json.Unmarshal(blob, &events); err != nil {
		return nil, err
	}
	return events, nil
}

func (w watcher) print(batch []json.RawMessage) {
	for _, e := range batch {
		fmt.Fprintln(w.out, string(e))
	}
}
