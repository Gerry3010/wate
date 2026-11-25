package app

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

// fakeDesktop records what would have gone to the desktop.
type fakeDesktop struct {
	mu      sync.Mutex
	sent    []notifications.NotificationOptions
	removed []string
}

func (d *fakeDesktop) SendNotification(o notifications.NotificationOptions) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sent = append(d.sent, o)
	return nil
}

func (d *fakeDesktop) RemoveNotification(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.removed = append(d.removed, id)
	return nil
}

func (d *fakeDesktop) counts() (int, int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.sent), len(d.removed)
}

// bell is a NoticeService with a fake desktop and a focus the test sets.
type bell struct {
	*NoticeService
	desk    *fakeDesktop
	focus   bool
	mu      sync.Mutex
	changes []NoticeChange
}

func newBell() *bell {
	b := &bell{desk: &fakeDesktop{}}
	b.NoticeService = &NoticeService{desktop: b.desk, now: time.Now, gather: 20 * time.Millisecond}
	b.NoticeService.focused = func() bool { b.mu.Lock(); defer b.mu.Unlock(); return b.focus }
	b.NoticeService.emit = func(_ string, data any) {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.changes = append(b.changes, data.(NoticeChange))
	}
	return b
}

func (b *bell) setFocus(f bool) { b.mu.Lock(); b.focus = f; b.mu.Unlock() }

func (b *bell) lastChange() NoticeChange {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.changes[len(b.changes)-1]
}

func TestABurstReachesTheDesktopAsOneNotification(t *testing.T) {
	b := newBell()
	for i := range 30 {
		b.post(Notice{Key: fmt.Sprintf("claude:p%d", i), Kind: "finished", Title: fmt.Sprintf("session %d", i), Body: "Finished"})
	}
	time.Sleep(100 * time.Millisecond)
	if sent, _ := b.desk.counts(); sent != 1 {
		t.Fatalf("%d desktop notifications for one burst, want 1", sent)
	}
	o := b.desk.sent[0]
	if o.ID != summaryID || o.Title != "30 notifications in wate" || o.Data["bell"] != true {
		t.Errorf("summary: %+v", o)
	}
}

func TestOneUnreadNoticeSaysItselfOnTheDesktop(t *testing.T) {
	b := newBell()
	b.post(Notice{Kind: "waiting", Title: "Fix the login", Body: "Waiting for you", Pane: "p1"})
	time.Sleep(60 * time.Millisecond)
	if sent, _ := b.desk.counts(); sent != 1 {
		t.Fatalf("sent %d", sent)
	}
	if o := b.desk.sent[0]; o.Title != "Fix the login" || o.Data["pane"] != "p1" {
		t.Errorf("got %+v", o)
	}
}

func TestWhileWateHasTheFocusANoticeIsABanner(t *testing.T) {
	b := newBell()
	b.setFocus(true)
	b.post(Notice{Kind: "finished", Title: "x"})
	time.Sleep(60 * time.Millisecond)
	if sent, _ := b.desk.counts(); sent != 0 {
		t.Errorf("%d desktop notifications while wate had the focus", sent)
	}
	if ch := b.lastChange(); ch.Banner == nil || ch.Banner.Title != "x" || ch.Unread != 1 {
		t.Errorf("change: %+v", ch)
	}
}

func TestComingBackToWateTakesTheSummaryDown(t *testing.T) {
	b := newBell()
	b.post(Notice{Kind: "finished", Title: "x"})
	time.Sleep(60 * time.Millisecond)
	b.setFocus(true)
	b.focusGained()
	if _, removed := b.desk.counts(); removed != 1 {
		t.Errorf("summary not withdrawn")
	}
	// The notice itself stays in the bell until it is read.
	if l := b.List(); l.Unread != 1 {
		t.Errorf("unread %d, want 1", l.Unread)
	}
}

func TestANewerNoticeReplacesTheOlderOfTheSameKey(t *testing.T) {
	b := newBell()
	b.setFocus(true)
	b.post(Notice{Key: "claude:p1", Title: "first"})
	b.post(Notice{Key: "claude:p2", Title: "other"})
	b.post(Notice{Key: "claude:p1", Title: "second"})
	l := b.List()
	if len(l.Notices) != 2 || l.Notices[0].Title != "second" || l.Notices[1].Title != "other" {
		t.Errorf("list: %+v", l.Notices)
	}
}

func TestLookingAtAPaneReadsItsNotices(t *testing.T) {
	b := newBell()
	b.setFocus(true)
	b.post(Notice{Key: "claude:p1", Title: "a", Pane: "p1"})
	b.post(Notice{Key: "claude:p2", Title: "b", Pane: "p2"})
	b.seen("p1")
	l := b.List()
	if l.Unread != 1 || !l.Notices[1].Read || l.Notices[0].Read {
		t.Errorf("list: %+v", l)
	}
}

func TestReadingEverythingTakesTheSummaryDown(t *testing.T) {
	b := newBell()
	b.post(Notice{Title: "a"})
	b.post(Notice{Title: "b"})
	time.Sleep(60 * time.Millisecond)
	b.MarkAllRead()
	time.Sleep(60 * time.Millisecond)
	if sent, removed := b.desk.counts(); sent != 1 || removed != 1 {
		t.Errorf("sent %d removed %d, want the summary up once and down once", sent, removed)
	}
}

func TestTheListStaysBounded(t *testing.T) {
	b := newBell()
	b.setFocus(true)
	for range maxNotices + 20 {
		b.post(Notice{Title: "x"})
	}
	if n := len(b.List().Notices); n != maxNotices {
		t.Errorf("%d notices kept", n)
	}
}
