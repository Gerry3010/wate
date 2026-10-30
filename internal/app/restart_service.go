package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/Gerry3010/wate/internal/agent"
	"github.com/Gerry3010/wate/internal/config"
)

// RestartAnswer is one agent's reply to "I would like to restart".
type RestartAnswer struct {
	Pane    string `json:"pane"`
	Verdict string `json:"verdict"` // "go" or "wait"
	Minutes int    `json:"minutes"`
	Note    string `json:"note"`
	At      string `json:"at"` // RFC3339
}

// RestartRecord is the pending restart, on disk.
//
// On disk because the hook that carries the news to an agent runs as a separate process, and
// because a wate that dies halfway through this leaves behind an explanation of what it was
// in the middle of.
type RestartRecord struct {
	Reason    string                   `json:"reason"`
	StartedAt string                   `json:"started_at"`
	Deadline  string                   `json:"deadline"`
	Answers   map[string]RestartAnswer `json:"answers"`
	// Relaunch is what the user asked for: start again afterwards, or only close.
	Relaunch bool `json:"relaunch"`
	// Window is the window the user wants to close, when that is what the round is for: only
	// the agents in it are asked, and the end of it closes that window and nothing else. Empty
	// means the whole of wate.
	Window string `json:"window,omitempty"`
	// Notified counts how often each pane has been told, so a busy agent is not reminded on
	// every single tool call.
	Notified map[string]int `json:"notified"`
}

// RestartRow is one line of the dialog: an agent, and what it said.
type RestartRow struct {
	Pane    string `json:"pane"`
	Title   string `json:"title"`
	Status  string `json:"status"`
	Verdict string `json:"verdict"`
	Minutes int    `json:"minutes"`
	Note    string `json:"note"`
}

// RestartStatus is the whole picture, for the dialog.
type RestartStatus struct {
	Pending bool   `json:"pending"`
	Reason  string `json:"reason"`
	// Relaunch and Window say what the round ends in — a restart, closing one window, or a
	// quit — so the dialog can call it by its name.
	Relaunch   bool         `json:"relaunch"`
	Window     string       `json:"window"`
	Deadline   string       `json:"deadline"`
	Expired    bool         `json:"expired"`
	Rows       []RestartRow `json:"rows"`
	Waiting    int          `json:"waiting"`
	Agreed     int          `json:"agreed"`
	Unanswered int          `json:"unanswered"`
	// GoAt is when the countdown will start the restart by itself (RFC3339), empty while
	// anybody is still being waited on.
	GoAt string `json:"go_at"`
}

// howOftenToTell is how many times one announcement may reach the same agent: once when it is
// made, and once more as the deadline comes up. Beyond that it is nagging.
const howOftenToTell = 2

// RestartService runs the "may I restart?" round.
//
// It never ends anything by itself. The deadline only changes what the dialog says; the
// decision stays with the user, which is the whole point — an agent that does not answer is a
// reason to look, not a reason to kill it.
type RestartService struct {
	agents  *AgentService
	windows *WindowService
	cfg     func() config.Config

	mu sync.Mutex
	// allowed is set by the dialog's own button. Until then a quit is held back, so a restart
	// that is still being arranged cannot be finished by accident.
	allowed bool
	// relaunch means: start again once this process is done.
	relaunch bool
	// allowClose holds the windows whose "quit anyway" has been pressed, so the close that
	// follows goes straight through instead of asking a second time.
	allowClose map[WindowID]bool
	// countdown runs once every agent has agreed; goAt is when it will fire.
	countdown *time.Timer
	goAt      time.Time
}

func NewRestartService(agents *AgentService, windows *WindowService, cfg func() config.Config) *RestartService {
	return &RestartService{agents: agents, windows: windows, cfg: cfg, allowClose: map[WindowID]bool{}}
}

func (s *RestartService) ServiceName() string { return "RestartService" }

func (s *RestartService) ServiceStartup(context.Context, application.ServiceOptions) error {
	// A record left behind by a previous run describes a restart that has already happened.
	if rec, ok := readRestart(); ok {
		slog.Info("dropping a restart left over from the last run", "reason", rec.Reason)
		s.clear()
	}
	return nil
}

func restartPath() string { return filepath.Join(config.StateDir(), "restart.json") }

func readRestart() (RestartRecord, bool) {
	var r RestartRecord
	data, err := os.ReadFile(restartPath())
	if err != nil {
		return r, false
	}
	if json.Unmarshal(data, &r) != nil {
		return r, false
	}
	if r.Answers == nil {
		r.Answers = map[string]RestartAnswer{}
	}
	if r.Notified == nil {
		r.Notified = map[string]int{}
	}
	return r, true
}

func writeRestart(r RestartRecord) error {
	p := restartPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func (s *RestartService) clear() {
	_ = os.Remove(restartPath())
}

// Announce starts the round: every running agent is told at its next hook.
func (s *RestartService) Announce(reason string, minutes int, relaunch bool) RestartStatus {
	return s.start(reason, minutes, relaunch, "")
}

// AnnounceClose starts the round for closing the window it is called from — the close
// dialog's "wait". The window's agents are asked the same question; wate is not restarted,
// and the other windows are left out of it.
func (s *RestartService) AnnounceClose(ctx context.Context, minutes int) RestartStatus {
	return s.start("", minutes, false, callerWindow(ctx))
}

func (s *RestartService) start(reason string, minutes int, relaunch bool, window WindowID) RestartStatus {
	if minutes <= 0 {
		minutes = 5
	}
	now := time.Now()
	rec := RestartRecord{
		Reason:    reason,
		StartedAt: now.Format(time.RFC3339),
		Deadline:  now.Add(time.Duration(minutes) * time.Minute).Format(time.RFC3339),
		Answers:   map[string]RestartAnswer{},
		Notified:  map[string]int{},
		Relaunch:  relaunch,
		Window:    string(window),
	}
	if err := writeRestart(rec); err != nil {
		slog.Warn("restart: could not record the request", "err", err)
	}
	s.mu.Lock()
	s.allowed = false
	s.mu.Unlock()
	st := s.settle()
	s.announce(st)
	return st
}

// countdownSeconds is how long to leave between the last agent agreeing and the restart.
// Zero means: wait for the button instead.
func (s *RestartService) countdownSeconds() int {
	if s.cfg == nil {
		return 10
	}
	return s.cfg().Claude.RestartCountdown
}

// settle takes the current picture and arms or disarms the countdown to match it.
//
// Once nobody is being waited on there is nothing left to decide: the user asked for the
// restart when they started the round, so it goes ahead on its own after a short pause —
// short enough not to be a wait, long enough to be called off.
func (s *RestartService) settle() RestartStatus {
	st := s.Status()
	secs := s.countdownSeconds()
	ready := st.Pending && st.Waiting == 0 && st.Unanswered == 0 && secs > 0

	s.mu.Lock()
	if !ready {
		if s.countdown != nil {
			s.countdown.Stop()
			s.countdown = nil
		}
		s.goAt = time.Time{}
		s.mu.Unlock()
		return st
	}
	if s.countdown != nil {
		// Already running; leave it be rather than restarting the clock under the user.
		goAt := s.goAt
		s.mu.Unlock()
		st.GoAt = goAt.Format(time.RFC3339)
		return st
	}
	s.goAt = time.Now().Add(time.Duration(secs) * time.Second)
	goAt := s.goAt
	s.countdown = time.AfterFunc(time.Duration(secs)*time.Second, s.fire)
	s.mu.Unlock()
	slog.Info("restart: everyone agreed, going in", "seconds", secs)
	st.GoAt = goAt.Format(time.RFC3339)
	return st
}

// fire is the countdown running out.
func (s *RestartService) fire() {
	s.mu.Lock()
	s.countdown = nil
	s.goAt = time.Time{}
	s.mu.Unlock()
	rec, ok := readRestart()
	if !ok {
		return
	}
	// Somebody may have started working again in the meantime.
	if st := s.Status(); st.Waiting > 0 || st.Unanswered > 0 {
		s.announce(s.settle())
		return
	}
	s.finish(rec)
}

// Finish is the dialog's own button: do now what the round was for.
func (s *RestartService) Finish() {
	if rec, ok := readRestart(); ok {
		s.finish(rec)
	}
}

func (s *RestartService) finish(rec RestartRecord) {
	if rec.Window == "" {
		s.Proceed(rec.Relaunch)
		return
	}
	s.closeWindow(WindowID(rec.Window))
}

// closeWindow ends a close round: the record goes, the window's own question counts as
// answered, and it closes like any other window — the last one taking wate with it.
func (s *RestartService) closeWindow(id WindowID) {
	s.mu.Lock()
	if s.countdown != nil {
		s.countdown.Stop()
		s.countdown = nil
	}
	s.goAt = time.Time{}
	s.mu.Unlock()
	s.clear()
	s.allowCloseFor(id)
	s.announce(RestartStatus{})
	if s.windows != nil {
		if w, ok := s.windows.byID(id); ok {
			w.Close()
		}
	}
}

// concerns says whether a pane is part of the round. A restart is about every pane; a close
// only about the panes in that window.
func (s *RestartService) concerns(rec RestartRecord, tabID, paneID string) bool {
	if rec.Window == "" || s.windows == nil {
		return true
	}
	return s.windows.windowForTab(tabID, paneID) == WindowID(rec.Window)
}

// gone reports a close round whose window has already gone some other way. Nothing is left
// to decide in it, and nobody would see it to call it off.
func (s *RestartService) gone(rec RestartRecord) bool {
	if rec.Window == "" || s.windows == nil {
		return false
	}
	return !s.windows.has(WindowID(rec.Window))
}

// Cancel drops the pending restart; everything carries on.
func (s *RestartService) Cancel() RestartStatus {
	s.clear()
	s.mu.Lock()
	s.allowed = false
	if s.countdown != nil {
		s.countdown.Stop()
		s.countdown = nil
	}
	s.goAt = time.Time{}
	s.mu.Unlock()
	st := s.Status()
	s.announce(st)
	return st
}

// Respond records one agent's answer.
func (s *RestartService) Respond(pane, verdict string, minutes int, note string) error {
	rec, ok := readRestart()
	if !ok {
		return fmt.Errorf("nothing is waiting on an answer")
	}
	if pane == "" {
		return fmt.Errorf("no pane: answer from inside the session that was asked")
	}
	if verdict != "go" && verdict != "wait" {
		return fmt.Errorf("say go or wait, not %q", verdict)
	}
	rec.Answers[pane] = RestartAnswer{
		Pane: pane, Verdict: verdict, Minutes: minutes, Note: note,
		At: time.Now().Format(time.RFC3339),
	}
	if err := writeRestart(rec); err != nil {
		return err
	}
	slog.Info("restart answer", "pane", pane, "verdict", verdict, "minutes", minutes)
	s.announce(s.settle())
	return nil
}

// Status merges the record with the sessions that are actually running.
func (s *RestartService) Status() RestartStatus {
	rec, ok := readRestart()
	if !ok {
		return RestartStatus{}
	}
	if s.gone(rec) {
		s.clear()
		return RestartStatus{}
	}
	st := RestartStatus{Pending: true, Reason: rec.Reason, Deadline: rec.Deadline, Relaunch: rec.Relaunch, Window: rec.Window}
	if t, err := time.Parse(time.RFC3339, rec.Deadline); err == nil {
		st.Expired = time.Now().After(t)
	}
	for _, sess := range s.sessions() {
		if !s.concerns(rec, sess.TabID, sess.PaneID) {
			continue
		}
		row := RestartRow{Pane: sess.PaneID, Title: sess.Title, Status: string(sess.Status)}
		if a, ok := rec.Answers[sess.PaneID]; ok {
			row.Verdict, row.Minutes, row.Note = a.Verdict, a.Minutes, a.Note
		}
		switch row.Verdict {
		case "go":
			st.Agreed++
		case "wait":
			st.Waiting++
		default:
			st.Unanswered++
		}
		st.Rows = append(st.Rows, row)
	}
	return st
}

func (s *RestartService) sessions() []agent.Session {
	if s.agents == nil {
		return nil
	}
	return s.agents.List()
}

// TellPane is the text to hand an agent in this pane, or "" when there is nothing to say.
//
// Called from the hook path, which is the only way into a running session that does not type
// into its terminal. Each announcement reaches a pane twice at most: once when it is made and
// once as the deadline comes up.
func (s *RestartService) TellPane(pane string) string {
	if pane == "" {
		return ""
	}
	rec, ok := readRestart()
	if !ok {
		return ""
	}
	if _, answered := rec.Answers[pane]; answered {
		return ""
	}
	// Closing one window is no business of the agents in the others.
	if !s.concerns(rec, "", pane) {
		return ""
	}
	told := rec.Notified[pane]
	if told >= howOftenToTell {
		return ""
	}
	// The second telling is held back until the deadline is close, so the two do not land on
	// top of each other during one busy minute.
	if told == 1 {
		t, err := time.Parse(time.RFC3339, rec.Deadline)
		if err != nil || time.Until(t) > time.Minute {
			return ""
		}
	}
	rec.Notified[pane] = told + 1
	if err := writeRestart(rec); err != nil {
		slog.Warn("restart: could not record the telling", "err", err)
	}
	// Closing a window needs no reason beyond itself; "no reason given" would read like one
	// was being kept back.
	why := ""
	if rec.Reason != "" {
		why = " Reason: " + rec.Reason + "."
	}
	when := rec.Deadline
	if t, err := time.Parse(time.RFC3339, rec.Deadline); err == nil {
		when = t.Format("15:04")
	}
	// Said as what it is: an agent asked about a restart expects to come back afterwards.
	what := "wate would like to restart"
	switch {
	case rec.Window != "":
		what = "The user would like to close this wate window"
	case !rec.Relaunch:
		what = "wate would like to quit"
	}
	return fmt.Sprintf(
		"%s, which will end this session.%s The user is "+
			"waiting for an answer until %s.\n"+
			"Answer once, now, with the wate_restart_respond tool, or by running "+
			"`wate ctl restart-respond go` (or `wait <minutes>`) in a shell:\n"+
			"  go — you are at a good stopping point\n"+
			"  wait <minutes> — you need a little longer; say roughly how long\n"+
			"Nothing will be ended without the user pressing the button, so an honest estimate "+
			"is more useful than a quick yes. Carry on with your work either way.",
		what, why, when)
}

// MayQuit is Wails' ShouldQuit: it holds a quit back only while a restart the user started is
// still being arranged, so the ordinary ways of closing wate are untouched.
func (s *RestartService) MayQuit() bool {
	s.mu.Lock()
	allowed := s.allowed
	s.mu.Unlock()
	if allowed {
		return true
	}
	// A window being closed is not wate being quit; holding the quit back for it would keep
	// wate alive for a question asked somewhere else.
	if rec, pending := readRestart(); !pending || rec.Window != "" {
		return true
	}
	// Only while there is still a window to decide in. Hold a quit back after the last one
	// has gone and wate would be alive with nothing on screen and no way to reach it, which
	// is a far worse failure than a restart finishing early.
	if s.windows != nil && s.windows.count() == 0 {
		return true
	}
	slog.Info("quit held back: a restart is still being arranged")
	return false
}

// Allow stops holding a quit back, without deciding anything else. A signal is an order, not
// a question, so the signal handler calls this before quitting.
func (s *RestartService) Allow() {
	s.mu.Lock()
	s.allowed = true
	s.mu.Unlock()
}

// Proceed is the dialog's own button: stop holding the quit back, and quit.
func (s *RestartService) Proceed(relaunch bool) {
	s.mu.Lock()
	s.allowed = true
	s.relaunch = relaunch
	s.mu.Unlock()
	s.clear()
	if app := application.Get(); app != nil {
		app.Quit()
	}
}

// Relaunch reports whether main should start wate again once this process is done.
func (s *RestartService) Relaunch() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.relaunch
}

// Forget drops the record as the app goes down, so the next start is not greeted by it.
func (s *RestartService) Forget() { s.clear() }

func (s *RestartService) announce(st RestartStatus) {
	if app := application.Get(); app != nil {
		app.Event.Emit("restart:changed", st)
	}
}

// CloseRequest is emitted as "window:confirm-close" when a window with work in it is closed.
type CloseRequest struct {
	Agents int `json:"agents"`
}

// ConfirmClose decides whether a window may close.
//
// Closing a window ends the shells in it, and with them any Claude Code session that was
// working. That is a reasonable thing to want and an awful thing to do by accident, so the
// first attempt is turned into a question and the answer comes back as AllowClose.
func (s *RestartService) ConfirmClose(id WindowID) bool {
	s.mu.Lock()
	if s.allowClose[id] {
		delete(s.allowClose, id)
		s.mu.Unlock()
		return true
	}
	allowed := s.allowed
	s.mu.Unlock()
	// A quit the user has already approved elsewhere (the restart dialog, a signal) is not
	// asked about again.
	if allowed {
		return true
	}
	n := s.agentsIn(id)
	if n == 0 {
		return true
	}
	if s.windows != nil {
		if w, ok := s.windows.byID(id); ok {
			emitTo(w, "window:confirm-close", CloseRequest{Agents: n})
			slog.Info("close held back", "window", id, "agents", n)
			return false
		}
	}
	return true
}

// AllowClose is the dialog's own button: let the next close of this window through.
func (s *RestartService) AllowClose(ctx context.Context) {
	s.allowCloseFor(callerWindow(ctx))
}

// callerWindow is the window a bound call came from.
func callerWindow(ctx context.Context) WindowID {
	if w, ok := ctx.Value(application.WindowKey).(application.Window); ok && w != nil {
		return WindowID(w.Name())
	}
	return ""
}

func (s *RestartService) allowCloseFor(id WindowID) {
	s.mu.Lock()
	s.allowClose[id] = true
	s.mu.Unlock()
}

// agentsIn counts the Claude Code sessions whose pane lives in this window.
func (s *RestartService) agentsIn(id WindowID) int {
	if s.agents == nil || s.windows == nil {
		return 0
	}
	n := 0
	for _, sess := range s.agents.List() {
		if s.windows.windowForTab(sess.TabID, sess.PaneID) == id {
			n++
		}
	}
	return n
}
