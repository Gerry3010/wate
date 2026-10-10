package app

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Access is what agents in a tab may do to one of its panes.
//
// It is per pane, not per agent. Two agents in the same tab see the same rights, because the
// alternative is a submenu for each of them in a list that changes as sessions come and go —
// and because the thing being described is really "this pane is open to the tab", not "this
// pane is open to that particular session".
type Access struct {
	Read   bool `json:"read"`
	Write  bool `json:"write"`
	Manage bool `json:"manage"`
}

// AccessService decides what an agent may do to a pane that is not its own.
//
// Nothing here is written to disk, deliberately: the agent a grant was made for does not
// survive a restart, and handing its successor the same rights would be an escalation that
// nobody asked for. Grants are made in the moment and die with the session.
type AccessService struct {
	pty *PtyService

	mu sync.Mutex
	// openedBy maps a pane to the pane that asked for it. The asker owns what it opened.
	openedBy map[string]string
	// granted maps a pane to what the user has opened it up to.
	granted map[string]Access
}

func NewAccessService(pty *PtyService) *AccessService {
	return &AccessService{pty: pty, openedBy: map[string]string{}, granted: map[string]Access{}}
}

func (a *AccessService) ServiceName() string { return "AccessService" }

func (a *AccessService) ServiceStartup(context.Context, application.ServiceOptions) error {
	return nil
}

// AccessState is what the pane's own status bar shows: who opened it, and what has been
// opened up to the agents in its tab.
type AccessState struct {
	Pane   string `json:"pane"`
	Owner  string `json:"owner"`
	Access Access `json:"access"`
}

// Opened records that `owner` asked for `pane`, which makes it the owner.
func (a *AccessService) Opened(owner, pane string) {
	if owner == "" || pane == "" {
		return
	}
	a.mu.Lock()
	a.openedBy[pane] = owner
	a.mu.Unlock()
	a.announce(pane)
}

// State is everything the frontend needs to draw one pane's bar.
func (a *AccessService) State(pane string) AccessState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return AccessState{Pane: pane, Owner: a.openedBy[pane], Access: a.granted[pane]}
}

// announce tells the windows that a pane's standing changed, so the bar repaints without
// anybody polling for it.
func (a *AccessService) announce(pane string) {
	if app := application.Get(); app != nil {
		app.Event.Emit("access:changed", a.State(pane))
	}
}

// Grant sets what agents in the pane's tab may do to it. Bound: the pane's own menu calls this.
func (a *AccessService) Grant(pane string, access Access) {
	a.mu.Lock()
	if access == (Access{}) {
		delete(a.granted, pane)
	} else {
		a.granted[pane] = access
	}
	a.mu.Unlock()
	slog.Info("agent access", "pane", pane, "read", access.Read, "write", access.Write, "manage", access.Manage)
	a.announce(pane)
}

// Of is what has been granted on a pane (not counting ownership).
func (a *AccessService) Of(pane string) Access {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.granted[pane]
}

// Owner is the pane that opened this one, if any.
func (a *AccessService) Owner(pane string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.openedBy[pane]
}

// Forget drops a pane, as owner and as target. Called when the pane goes away.
func (a *AccessService) Forget(pane string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.granted, pane)
	delete(a.openedBy, pane)
	for p, owner := range a.openedBy {
		if owner == pane {
			delete(a.openedBy, p)
		}
	}
}

// Right is one of the three things an agent can be allowed to do.
type Right string

const (
	RightRead   Right = "read"
	RightWrite  Right = "write"
	RightManage Right = "manage"
)

// Allow answers whether `caller` may do `want` to `target`, and says why not when it may not.
//
// Three rules, in order: a pane may always act on itself; a pane may do anything to a pane it
// opened; anything else needs the user to have granted it, and only inside the same tab.
func (a *AccessService) Allow(caller, target string, want Right) error {
	if caller == "" {
		return fmt.Errorf("no caller pane: run this from inside a wate pane")
	}
	if target == "" {
		return fmt.Errorf("no pane named")
	}
	if caller == target {
		return nil
	}
	ct, okc := a.pty.TabForPane(caller)
	tt, okt := a.pty.TabForPane(target)
	if !okc || !okt {
		return fmt.Errorf("no such pane %q", target)
	}
	if ct != tt {
		return fmt.Errorf("pane %q is in another tab", target)
	}
	if a.Owner(target) == caller {
		return nil
	}
	g := a.Of(target)
	switch want {
	case RightRead:
		if g.Read {
			return nil
		}
	case RightWrite:
		if g.Write {
			return nil
		}
	case RightManage:
		if g.Manage {
			return nil
		}
	}
	return fmt.Errorf("pane %q has not been opened up for %s — grant it in the pane's menu", target, want)
}

// Owns reports whether the caller opened this pane (or is it).
func (a *AccessService) Owns(caller, target string) bool {
	return caller != "" && (caller == target || a.Owner(target) == caller)
}

// Submits reports whether what the caller types is sent as it stands — Enter and all.
//
// Its own panes always are. For somebody else's, the line is Read and Write *together*. A user
// who has ticked both has handed the pane over: the agent can already put a command on the
// prompt and watch what comes of it, so holding back the Return key buys no safety, it only
// leaves the agent waiting for a keypress that will never be explained to it. Write on its own
// stays text-only, which keeps "put it on my prompt and let me look before it runs" as
// something you can actually grant.
func (a *AccessService) Submits(caller, target string) bool {
	if a.Owns(caller, target) {
		return true
	}
	g := a.Of(target)
	return g.Read && g.Write
}

// typable strips everything that would make a shell act on what was typed.
//
// This is what a bare Write grant comes to: the agent may put text on the prompt, and the user
// presses Enter. It is the reviewable half of writing, for the case where you want a command
// offered rather than run. Add Read to it and the pane is handed over for good — see Submits,
// which is what decides whether this filter runs at all.
func typable(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}
