package app

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

// Notice is one entry in wate's own notification list, the bell in the tab bar.
type Notice struct {
	ID string `json:"id"`
	// Key groups notices about the same thing: a newer one with the same key replaces the
	// older, so a session that finishes twice is one entry, not two.
	Key string `json:"key"`
	// Kind is what the notice is about: "waiting" (a session needs the user), "finished",
	// "root" (the root agent's word) or "agent" (a session's own notice).
	Kind  string `json:"kind"`
	Title string `json:"title"`
	Body  string `json:"body"`
	// Pane is where a click leads; From is the pane that posted it, empty for wate itself.
	Pane string    `json:"pane"`
	From string    `json:"from"`
	At   time.Time `json:"at"`
	Read bool      `json:"read"`
}

// NoticeList is the bell's content, newest first.
type NoticeList struct {
	Notices []Notice `json:"notices"`
	Unread  int      `json:"unread"`
}

// BellRequest asks a window to open its bell (a click on the desktop summary).
type BellRequest struct{}

// NoticeChange goes out with every change. Banner is the notice that just came in, for a
// window that has the focus to show; the others only update their bell.
type NoticeChange struct {
	NoticeList
	Banner *Notice `json:"banner"`
}

const (
	// maxNotices keeps the list from growing for ever; old ones are not worth scrolling to.
	maxNotices = 100
	// summaryDelay gathers the notices of a burst (thirty sessions finishing within a few
	// seconds) into one desktop notification instead of thirty updates of it.
	summaryDelay = 1500 * time.Millisecond
	// summaryID is the one desktop notification wate keeps up while it is in the background.
	summaryID = "wate-notices"
)

// desktop is the part of the desktop notification service the bell uses.
type desktop interface {
	SendNotification(notifications.NotificationOptions) error
	RemoveNotification(identifier string) error
}

// NoticeService keeps the bell's list and decides how a notice reaches the user: as a banner
// when a wate window has the focus, else as a single desktop notification that sums up what
// is unread. GNOME holds back an app's notifications while that app has the focus and shows
// them all at once when it loses it, which is how leaving wate used to bring up a stack.
type NoticeService struct {
	mu   sync.Mutex
	list []Notice
	seq  int

	desktop desktop
	windows *WindowService
	pty     *PtyService
	now     func() time.Time
	// focused says whether a wate window has the focus (AgentService knows).
	focused func() bool
	// emit sends an event to every window; tests catch it instead.
	emit func(name string, data any)

	summary      *time.Timer
	summaryShown bool
	// gather is how long a burst is gathered before the desktop hears of it (summaryDelay).
	gather time.Duration
}

func NewNoticeService(d *notifications.NotificationService, windows *WindowService, pty *PtyService) *NoticeService {
	s := &NoticeService{windows: windows, pty: pty, now: time.Now, gather: summaryDelay}
	if d != nil {
		s.desktop = d
		d.OnNotificationResponse(s.onDesktopClick)
	}
	return s
}

func (s *NoticeService) ServiceName() string { return "NoticeService" }

// List is the bell's content.
func (s *NoticeService) List() NoticeList {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listLocked(nil)
}

// listWhere is the part of the list keep lets through.
func (s *NoticeService) listWhere(keep func(Notice) bool) NoticeList {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listLocked(keep)
}

// listLocked copies the notices keep lets through (all for nil).
func (s *NoticeService) listLocked(keep func(Notice) bool) NoticeList {
	out := NoticeList{Notices: []Notice{}}
	for _, n := range s.list {
		if keep != nil && !keep(n) {
			continue
		}
		out.Notices = append(out.Notices, n)
		if !n.Read {
			out.Unread++
		}
	}
	return out
}

// MarkRead marks the notices with these ids as read.
func (s *NoticeService) MarkRead(ids []string) {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	_ = s.mark(func(n Notice) bool { return want[n.ID] })
}

// MarkAllRead marks every notice as read.
func (s *NoticeService) MarkAllRead() { _ = s.mark(func(Notice) bool { return true }) }

// Clear empties the list.
func (s *NoticeService) Clear() {
	s.mu.Lock()
	s.list = nil
	s.mu.Unlock()
	s.changed(nil)
}

// Open is a click on a notice: it is read, and its pane comes to the front.
func (s *NoticeService) Open(id string) {
	var pane string
	s.mu.Lock()
	for i := range s.list {
		if s.list[i].ID == id {
			s.list[i].Read = true
			pane = s.list[i].Pane
		}
	}
	s.mu.Unlock()
	s.changed(nil)
	if pane != "" {
		s.focusPane(pane)
	}
}

// post adds a notice and brings it to the user. It replaces an older one with the same key.
func (s *NoticeService) post(n Notice) Notice {
	s.mu.Lock()
	s.seq++
	n.ID = fmt.Sprintf("n%d", s.seq)
	n.At = s.now()
	n.Read = false
	list := make([]Notice, 0, len(s.list)+1)
	list = append(list, n)
	for _, o := range s.list {
		if n.Key == "" || o.Key != n.Key {
			list = append(list, o)
		}
	}
	if len(list) > maxNotices {
		list = list[:maxNotices]
	}
	s.list = list
	s.mu.Unlock()

	if s.isFocused() {
		s.changed(&n)
	} else {
		s.changed(nil)
		s.scheduleSummary()
	}
	return n
}

// resolve marks the notices under key as read: what they were about is over (the session went
// back to work, or ended).
func (s *NoticeService) resolve(key string) {
	_ = s.mark(func(n Notice) bool { return n.Key == key })
}

// seen marks the notices about a pane as read: the user is looking at it.
func (s *NoticeService) seen(pane string) {
	if pane == "" {
		return
	}
	_ = s.mark(func(n Notice) bool { return n.Pane == pane })
}

// mark sets Read on the unread notices match picks, tells everyone if any were, and says how
// many.
func (s *NoticeService) mark(match func(Notice) bool) int {
	s.mu.Lock()
	marked := 0
	for i := range s.list {
		if !s.list[i].Read && match(s.list[i]) {
			s.list[i].Read = true
			marked++
		}
	}
	s.mu.Unlock()
	if marked == 0 {
		return 0
	}
	s.changed(nil)
	// A desktop summary that is up counts what is unread; it has to follow, or go. One that is
	// not up stays down: reading one notice is no reason to bring up the others again.
	s.mu.Lock()
	shown := s.summaryShown
	s.mu.Unlock()
	if shown {
		s.scheduleSummary()
	}
	return marked
}

// focusGained is a wate window getting the focus: from now on notices show as banners, and
// the desktop summary has said what it had to.
func (s *NoticeService) focusGained() {
	s.mu.Lock()
	if s.summary != nil {
		s.summary.Stop()
		s.summary = nil
	}
	shown := s.summaryShown
	s.summaryShown = false
	s.mu.Unlock()
	if shown {
		s.withdraw()
	}
}

func (s *NoticeService) isFocused() bool { return s.focused != nil && s.focused() }

func (s *NoticeService) changed(banner *Notice) {
	s.mu.Lock()
	ch := NoticeChange{NoticeList: s.listLocked(nil), Banner: banner}
	s.mu.Unlock()
	if s.emit != nil {
		s.emit("notices:changed", ch)
		return
	}
	if a := application.Get(); a != nil {
		a.Event.Emit("notices:changed", ch)
	}
}

// scheduleSummary brings the desktop summary up to date a moment from now, so a burst of
// notices makes one update.
func (s *NoticeService) scheduleSummary() {
	if s.desktop == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.summary != nil {
		return
	}
	s.summary = time.AfterFunc(s.gather, s.showSummary)
}

// showSummary puts up (or takes down) the one desktop notification that sums up what is
// unread. Nothing goes to the desktop while a wate window has the focus: there, the banner
// has already said it.
func (s *NoticeService) showSummary() {
	s.mu.Lock()
	s.summary = nil
	var unread []Notice
	for _, n := range s.list {
		if !n.Read {
			unread = append(unread, n)
		}
	}
	shown := s.summaryShown
	s.mu.Unlock()
	if s.isFocused() {
		return
	}
	if len(unread) == 0 {
		if shown {
			s.withdraw()
		}
		return
	}
	opts := summaryOptions(unread)
	if err := s.desktop.SendNotification(opts); err != nil {
		slog.Debug("notification", "err", err)
		return
	}
	s.mu.Lock()
	s.summaryShown = true
	s.mu.Unlock()
}

// summaryOptions is the desktop notification for a list of unread notices, newest first. One
// says itself and leads to its pane; several are counted, and a click opens the bell.
func summaryOptions(unread []Notice) notifications.NotificationOptions {
	if len(unread) == 1 {
		n := unread[0]
		return notifications.NotificationOptions{
			ID: summaryID, Title: n.Title, Body: n.Body,
			Data: map[string]interface{}{"pane": n.Pane},
		}
	}
	const shown = 3
	var lines []string
	for i, n := range unread {
		if i == shown {
			lines = append(lines, fmt.Sprintf("and %d more", len(unread)-shown))
			break
		}
		line := n.Title
		if n.Body != "" {
			line += ": " + firstLine(n.Body, 70)
		}
		lines = append(lines, line)
	}
	return notifications.NotificationOptions{
		ID:    summaryID,
		Title: fmt.Sprintf("%d notifications in wate", len(unread)),
		Body:  strings.Join(lines, "\n"),
		Data:  map[string]interface{}{"bell": true},
	}
}

func (s *NoticeService) withdraw() {
	if s.desktop == nil {
		return
	}
	if err := s.desktop.RemoveNotification(summaryID); err != nil {
		slog.Debug("withdraw notification", "err", err)
	}
}

// onDesktopClick follows a click on the desktop notification: to the pane it was about, or
// into wate with the bell open when it counted several.
func (s *NoticeService) onDesktopClick(r notifications.NotificationResult) {
	if r.Error != nil || r.Response.ActionIdentifier != notifications.DefaultActionIdentifier {
		return
	}
	s.mu.Lock()
	s.summaryShown = false
	s.mu.Unlock()
	if bell, _ := r.Response.UserInfo["bell"].(bool); bell {
		s.raise(true)
		return
	}
	if pane, _ := r.Response.UserInfo["pane"].(string); pane != "" {
		s.seen(pane)
		s.focusPane(pane)
		return
	}
	// About no pane in particular (a restart round waiting for a decision): wate itself.
	s.raise(false)
}

// focusPane brings a pane to the front wherever it is now: its window, its tab, the focus in it.
func (s *NoticeService) focusPane(pane string) {
	if s.windows == nil {
		return
	}
	tab := ""
	if s.pty != nil {
		tab, _ = s.pty.TabForPane(pane)
	}
	win := s.windows.windowForTab(tab, pane)
	if win == "" {
		return
	}
	if err := s.windows.FocusPane(string(win), tab, pane); err != nil {
		slog.Debug("focus pane", "pane", pane, "err", err)
	}
}

// raise brings the window used last to the front, and opens its bell when bell is set.
func (s *NoticeService) raise(bell bool) {
	if s.windows == nil {
		return
	}
	w, ok := s.windows.windowFor("", "")
	if !ok {
		return
	}
	application.InvokeSync(func() {
		if w.IsMinimised() {
			w.UnMinimise()
		}
		w.Focus()
	})
	if bell {
		w.EmitEvent("notices:open", BellRequest{})
	}
}
