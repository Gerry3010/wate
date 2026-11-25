package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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
	// By is "root" when the root agent asked for the round, empty when the user did.
	By string `json:"by,omitempty"`
}

// RestartRow is one line of the dialog: an agent, and what it said.
type RestartRow struct {
	Pane    string `json:"pane"`
	Title   string `json:"title"`
	Status  string `json:"status"`
	Verdict string `json:"verdict"`
	Minutes int    `json:"minutes"`
	Note    string `json:"note"`
	// Asked is set once the question has reached the agent, by a hook or typed at its prompt.
	Asked bool `json:"asked"`
}

// RestartStatus is the whole picture, for the dialog.
type RestartStatus struct {
	Pending bool   `json:"pending"`
	Reason  string `json:"reason"`
	// Relaunch and Window say what the round ends in — a restart, closing one window, or a
	// quit — so the dialog can call it by its name.
	Relaunch bool   `json:"relaunch"`
	Window   string `json:"window"`
	// By is "root" when the root agent asked for the round.
	By         string       `json:"by"`
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
	// asking is set while the loop that types the question at idle prompts runs (keepAsking).
	asking bool
	// typedRound and typedAt remember, for the round that started at typedRound, when the
	// question was typed at each prompt; that is what askAtOnce counts.
	typedRound string
	typedAt    map[string]time.Time
	// expiry fires at the deadline, to put the decision in front of the user.
	expiry *time.Timer
	// onExpired stands in for telling the user in tests.
	onExpired func(RestartStatus)

	// recMu serialises changes to the record on disk: the hook path, the answers and the
	// asking loop all read it, change it and write it back.
	recMu sync.Mutex
	// prompt types at a session's prompt (AskAtPrompts). Without it only the hook path asks.
	prompt promptOps
	// listSessions stands in for the running sessions in tests.
	listSessions func() []agent.Session
	// beforeRelaunch runs just before a restart quits (PrepareRelaunch): it checks that the
	// installed wate runs and writes down what was running. An error calls the restart off.
	beforeRelaunch func(RestartRecord) error
}

// askEvery is how often a pending round looks for agents that sit idle at their prompt.
const askEvery = 2 * time.Second

// askAtOnce is how many idle agents are asked at the same time. Each one asked starts a turn
// of its own; thirty of them starting in the same second kept every core busy and the desktop
// stuttering. The next one is asked as soon as one answers.
const askAtOnce = 4

// answerWithin is how long an asked agent keeps its place among askAtOnce. One that has not
// answered by then is not holding up the rest any longer.
const answerWithin = 30 * time.Second

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
	s.mu.Lock()
	if s.expiry != nil {
		s.expiry.Stop()
		s.expiry = nil
	}
	s.mu.Unlock()
}

// AskAtPrompts lets the round type its question at the prompt of agents that sit idle, through
// the panes c reaches. Without it they would only hear of it at their next hook, and an idle
// agent has none: it calls no tool and ends no turn.
//
// A function rather than a method: every exported method of a bound service becomes a
// frontend binding, and this one is wiring.
func AskAtPrompts(s *RestartService, c *CtlService) {
	if s == nil || s.agents == nil || c == nil {
		return
	}
	s.prompt = c.promptOps(s.agents.ClaudePid)
}

// Announce starts the round: busy agents are told at their next hook, idle ones at their
// prompt.
func (s *RestartService) Announce(reason string, minutes int, relaunch bool) RestartStatus {
	return s.start(reason, minutes, relaunch, "", "")
}

// announceForRoot starts a restart round the root agent asked for. The same round as the
// user's: every agent is asked, and the restart happens only once all of them have said go,
// or the user decides at the deadline. The root agent asked, so it counts as having said go.
// It does not take over a round that is already running.
func (s *RestartService) announceForRoot(reason string, minutes int) (RestartStatus, error) {
	if rec, pending := readRestart(); pending {
		what := "a restart"
		if rec.Window != "" {
			what = "closing a window"
		}
		return RestartStatus{}, fmt.Errorf("a round for %s is already running; wait for it, or ask the user", what)
	}
	return s.start(reason, minutes, true, "", "root"), nil
}

// cancelForRoot calls off a round the root agent asked for. One the user started is theirs.
func (s *RestartService) cancelForRoot() (RestartStatus, error) {
	rec, pending := readRestart()
	if !pending {
		return RestartStatus{}, errors.New("no restart round is running")
	}
	if rec.By != "root" {
		return RestartStatus{}, errors.New("this round was started by the user; only they can call it off")
	}
	return s.Cancel(), nil
}

// AnnounceClose starts the round for closing the window it is called from — the close
// dialog's "wait". The window's agents are asked the same question; wate is not restarted,
// and the other windows are left out of it.
func (s *RestartService) AnnounceClose(ctx context.Context, minutes int) RestartStatus {
	return s.start("", minutes, false, callerWindow(ctx), "")
}

func (s *RestartService) start(reason string, minutes int, relaunch bool, window WindowID, by string) RestartStatus {
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
		By:        by,
	}
	if root := s.rootPane(); by == "root" && root != "" {
		rec.Answers[root] = RestartAnswer{Pane: root, Verdict: "go", Note: "asked for this restart", At: now.Format(time.RFC3339)}
	}
	if err := writeRestart(rec); err != nil {
		slog.Warn("restart: could not record the request", "err", err)
	}
	s.mu.Lock()
	s.allowed = false
	s.mu.Unlock()
	s.armExpiry(rec)
	st := s.settle()
	s.announce(st)
	s.keepAsking()
	return st
}

// Extend gives the agents more time: the deadline moves to minutes from now, and the user is
// asked again when that runs out. The expired dialog's "wait".
func (s *RestartService) Extend(minutes int) RestartStatus {
	if minutes <= 0 {
		minutes = 5
	}
	s.recMu.Lock()
	rec, ok := readRestart()
	if ok {
		rec.Deadline = time.Now().Add(time.Duration(minutes) * time.Minute).Format(time.RFC3339)
		if err := writeRestart(rec); err != nil {
			slog.Warn("restart: could not move the deadline", "err", err)
		}
	}
	s.recMu.Unlock()
	if !ok {
		return RestartStatus{}
	}
	s.armExpiry(rec)
	s.resolveExpiry()
	st := s.settle()
	s.announce(st)
	return st
}

// armExpiry sets the timer for rec's deadline, replacing any earlier one.
func (s *RestartService) armExpiry(rec RestartRecord) {
	at, err := time.Parse(time.RFC3339, rec.Deadline)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.expiry != nil {
		s.expiry.Stop()
	}
	s.expiry = time.AfterFunc(max(0, time.Until(at)), func() { s.expire(rec.StartedAt, rec.Deadline) })
}

// expire is the deadline running out. wate does not end anything by itself: an agent that has
// not answered is a reason to look, not a reason to cut it off. So the round stays, and the
// user is asked, in the middle of the window and, with wate in the background, in the bell.
func (s *RestartService) expire(startedAt, deadline string) {
	rec, ok := readRestart()
	if !ok || rec.StartedAt != startedAt || rec.Deadline != deadline {
		return
	}
	st := s.settle()
	s.announce(st)
	if !st.Pending || (st.Waiting == 0 && st.Unanswered == 0) {
		// Everyone agreed after all; the countdown takes it from here.
		return
	}
	slog.Info("restart: deadline passed", "waiting", st.Waiting, "unanswered", st.Unanswered)
	if s.onExpired != nil {
		s.onExpired(st)
		return
	}
	var w *application.WebviewWindow
	if s.windows != nil {
		if rec.Window != "" {
			w, _ = s.windows.byID(WindowID(rec.Window))
		} else {
			w, _ = s.windows.windowFor("", "")
		}
	}
	emitTo(w, "restart:expired", st)
	if s.agents != nil {
		s.agents.Notify(restartNoticeKey, expiredTitle(st), expiredBody(st), "")
	}
}

// restartNoticeKey is the bell's entry for a round whose time is up.
const restartNoticeKey = "wate:restart"

func (s *RestartService) resolveExpiry() {
	if s.agents != nil && s.agents.notices != nil {
		s.agents.notices.resolve(restartNoticeKey)
	}
}

// expiredTitle and expiredBody are the bell's words for a round whose time is up.
func expiredTitle(st RestartStatus) string {
	switch {
	case st.Window != "":
		return "Time's up: close the window?"
	case st.Relaunch:
		return "Time's up: restart wate?"
	}
	return "Time's up: quit wate?"
}

func expiredBody(st RestartStatus) string {
	var parts []string
	if st.Unanswered > 0 {
		parts = append(parts, fmt.Sprintf("%d %s not answered", st.Unanswered, plural(st.Unanswered, "agent has", "agents have")))
	}
	if st.Waiting > 0 {
		parts = append(parts, fmt.Sprintf("%d %s for more time", st.Waiting, plural(st.Waiting, "agent asks", "agents ask")))
	}
	return strings.Join(parts, ", ") + ". wate is waiting for your decision."
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
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
	s.resolveExpiry()
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
	s.resolveExpiry()
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
	s.recMu.Lock()
	// Read again under the lock: the record may have changed since the checks above.
	if rec, ok = readRestart(); !ok {
		s.recMu.Unlock()
		return fmt.Errorf("nothing is waiting on an answer")
	}
	rec.Answers[pane] = RestartAnswer{
		Pane: pane, Verdict: verdict, Minutes: minutes, Note: note,
		At: time.Now().Format(time.RFC3339),
	}
	err := writeRestart(rec)
	s.recMu.Unlock()
	if err != nil {
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
	st := RestartStatus{Pending: true, Reason: rec.Reason, Deadline: rec.Deadline, Relaunch: rec.Relaunch, Window: rec.Window, By: rec.By}
	if t, err := time.Parse(time.RFC3339, rec.Deadline); err == nil {
		st.Expired = time.Now().After(t)
	}
	for _, sess := range s.sessions() {
		if !s.concerns(rec, sess.TabID, sess.PaneID) {
			continue
		}
		row := RestartRow{Pane: sess.PaneID, Title: sess.Title, Status: string(sess.Status), Asked: rec.Notified[sess.PaneID] > 0}
		if sess.PaneID != "" && sess.PaneID == s.rootPane() {
			// Its title is the prompt wate started it with, which says nothing in this list.
			row.Title = "Root agent"
		}
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

// rootPane is the root agent's pane, "" when none runs.
func (s *RestartService) rootPane() string {
	if s.agents == nil || s.agents.pty == nil {
		return ""
	}
	return s.agents.pty.RootPane()
}

func (s *RestartService) sessions() []agent.Session {
	if s.listSessions != nil {
		return s.listSessions()
	}
	if s.agents == nil {
		return nil
	}
	return s.agents.List()
}

// TellPane is the text to hand an agent in this pane, or "" when there is nothing to say.
//
// Called from the hook path, which reaches an agent while it works: at its next tool call, or
// as its turn ends. An agent idle at its prompt has neither and is asked by keepAsking
// instead. Each announcement reaches a pane twice at most, by either way: once when it is made
// and once as the deadline comes up.
func (s *RestartService) TellPane(pane string) string {
	if pane == "" {
		return ""
	}
	s.recMu.Lock()
	defer s.recMu.Unlock()
	rec, ok := readRestart()
	if !ok {
		return ""
	}
	// Closing one window is no business of the agents in the others.
	if !dueToBeTold(rec, pane, time.Now()) || !s.concerns(rec, "", pane) {
		return ""
	}
	rec.Notified[pane]++
	if err := writeRestart(rec); err != nil {
		slog.Warn("restart: could not record the telling", "err", err)
	}
	return question(rec, false)
}

// dueToBeTold says whether the pane should hear of the round now: it has not answered, and it
// has not been told yet, or once and the deadline is close. The second telling is held back so
// the two do not land on top of each other during one busy minute.
func dueToBeTold(rec RestartRecord, pane string, now time.Time) bool {
	if _, answered := rec.Answers[pane]; answered {
		return false
	}
	switch told := rec.Notified[pane]; {
	case told >= howOftenToTell:
		return false
	case told == 1:
		t, err := time.Parse(time.RFC3339, rec.Deadline)
		return err == nil && t.Sub(now) <= time.Minute
	}
	return true
}

// question is what an agent is asked. idle marks one that sits at its prompt: the question
// reaches it typed in as a prompt of its own, so it is one line, and it says that nothing
// else is wanted from that turn.
func question(rec RestartRecord, idle bool) string {
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
	ask := fmt.Sprintf(
		"%s, which will end this session.%s The user is waiting for an answer until %s. "+
			"Answer once, now, with the wate_restart_respond tool, or by running "+
			"`wate ctl restart-respond go` (or `wait <minutes>`) in a shell: "+
			"go if you are at a good stopping point, wait <minutes> if you need a little longer, "+
			"saying roughly how long. Nothing will be ended without the user pressing the button, "+
			"so an honest estimate is more useful than a quick yes.",
		what, why, when)
	if idle {
		return agent.WatePrompt + ask + " If anything of yours still runs in the background (a command, " +
			"an agent, a monitor), say wait. Only answer; do not pick up other work because of this message."
	}
	return ask + " Carry on with your work either way."
}

// keepAsking starts the loop that asks the agents the hook path cannot reach, unless it runs
// already. It ends with the round.
func (s *RestartService) keepAsking() {
	s.mu.Lock()
	if s.asking || s.prompt.write == nil {
		s.mu.Unlock()
		return
	}
	s.asking = true
	s.mu.Unlock()
	go func() {
		for {
			pending := s.askIdle()
			// Decided under the lock that start takes before it would start another loop, so a
			// round that begins just as this one ends is not left without one.
			s.mu.Lock()
			if !pending {
				if _, pending = readRestart(); !pending {
					s.asking = false
					s.mu.Unlock()
					return
				}
			}
			s.mu.Unlock()
			time.Sleep(askEvery)
		}
	}()
}

// askIdle types the question at the prompt of every agent in the round that is due to hear of
// it and sits idle with an empty input box. Busy agents are left to the hook path, and an
// agent with a dialog open or text in its input box is tried again on the next pass. It
// reports whether a round is still pending.
func (s *RestartService) askIdle() bool {
	rec, ok := readRestart()
	if !ok {
		return false
	}
	if s.gone(rec) {
		s.clear()
		return false
	}
	room := s.roomToAsk(rec, time.Now())
	if room == 0 {
		return true
	}
	var claimed []string
	for _, sess := range s.sessions() {
		if len(claimed) == room {
			break
		}
		pane := sess.PaneID
		if pane == "" || !dueToBeTold(rec, pane, time.Now()) || !s.concerns(rec, sess.TabID, pane) {
			continue
		}
		pid := s.prompt.pid(pane)
		if pid == 0 {
			continue
		}
		st, err := s.prompt.state(pid)
		if err != nil || promptIsFree(s.prompt, pane, st) != nil {
			continue
		}
		if s.claim(rec.StartedAt, pane, 1) {
			claimed = append(claimed, pane)
		}
	}
	if len(claimed) == 0 {
		return true
	}
	// Each pane has a terminal of its own, so the few asked at once are typed at side by side.
	line := question(rec, true)
	now := time.Now()
	s.mu.Lock()
	for _, pane := range claimed {
		s.typedAt[pane] = now
	}
	s.mu.Unlock()
	failed := make([]bool, len(claimed))
	var wg sync.WaitGroup
	for i, pane := range claimed {
		wg.Go(func() {
			if err := submitLine(s.prompt, pane, line); err != nil {
				slog.Warn("restart: could not ask at the prompt", "pane", pane, "err", err)
				failed[i] = true
				return
			}
			slog.Info("restart: asked an idle agent at its prompt", "pane", pane)
		})
	}
	wg.Wait()
	for i, pane := range claimed {
		if failed[i] {
			s.claim(rec.StartedAt, pane, -1)
			s.mu.Lock()
			delete(s.typedAt, pane)
			s.mu.Unlock()
		}
	}
	s.announce(s.settle())
	return true
}

// roomToAsk is how many more idle agents may be asked now: askAtOnce, less the ones typed at
// in this round that have neither answered nor had answerWithin to do so.
func (s *RestartService) roomToAsk(rec RestartRecord, now time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.typedRound != rec.StartedAt || s.typedAt == nil {
		s.typedRound, s.typedAt = rec.StartedAt, map[string]time.Time{}
	}
	busy := 0
	for pane, at := range s.typedAt {
		if _, answered := rec.Answers[pane]; !answered && now.Sub(at) < answerWithin {
			busy++
		}
	}
	return max(0, askAtOnce-busy)
}

// claim counts a telling of pane in the round that started at startedAt (by is 1), or takes
// one back that did not go through (by is -1). Counting before the typing keeps the hook path
// from telling the same agent at the same moment; it reports false when somebody got there
// first, or the round has changed.
func (s *RestartService) claim(startedAt, pane string, by int) bool {
	s.recMu.Lock()
	defer s.recMu.Unlock()
	rec, ok := readRestart()
	if !ok || rec.StartedAt != startedAt {
		return false
	}
	if by > 0 && !dueToBeTold(rec, pane, time.Now()) {
		return false
	}
	rec.Notified[pane] = max(0, rec.Notified[pane]+by)
	if err := writeRestart(rec); err != nil {
		slog.Warn("restart: could not record the telling", "err", err)
		return false
	}
	return true
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
//
// A restart first makes sure the wate that will start is one that runs, and writes down what
// is running now (beforeRelaunch). Quitting into a binary that does not start would leave the
// user with no terminal and nobody to tell them why.
func (s *RestartService) Proceed(relaunch bool) {
	if relaunch && s.beforeRelaunch != nil {
		rec, _ := readRestart()
		if err := s.beforeRelaunch(rec); err != nil {
			slog.Warn("restart called off", "err", err)
			s.calledOff(err)
			return
		}
	}
	s.mu.Lock()
	s.allowed = true
	s.relaunch = relaunch
	s.mu.Unlock()
	s.clear()
	if app := application.Get(); app != nil {
		app.Quit()
	}
}

// calledOff ends a round whose restart could not go ahead, and says why in the bell.
func (s *RestartService) calledOff(err error) {
	s.Cancel()
	if s.agents != nil {
		why := err.Error()
		if why != "" {
			why = strings.ToUpper(why[:1]) + why[1:]
		}
		s.agents.Notify(restartNoticeKey, "Restart called off", why+". wate keeps running.", "")
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
