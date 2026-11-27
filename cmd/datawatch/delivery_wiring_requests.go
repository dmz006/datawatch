// Wiring of the request store (delivery job 4). Owner: part A. The store opens
// only after every other sealed store has opened, and writes nothing before
// that.

package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/acceptance"
	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/delivery"
	"github.com/dmz006/datawatch/internal/secfile"
	"github.com/dmz006/datawatch/internal/session"
)

// requestsDir is the request store's folder under the data folder.
const requestsDir = "queue.d"

// requestStore is the open store, or nil.
func (w *deliveryWiring) requestStore() *delivery.Requests {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.requests
}

// requestBook is the store as part C's list probe reads it: nil, not an empty
// pointer, until it is open.
func (w *deliveryWiring) requestBook() delivery.RequestBook {
	if r := w.requestStore(); r != nil {
		return r
	}
	return nil
}

// requestSettings reads the live delivery block.
func requestSettings(cfg *config.Config) delivery.RequestSettings {
	c := cfg.Delivery
	byKind := map[delivery.RequestKind]time.Duration{}
	for kind, d := range c.RequestDueByKind() {
		byKind[delivery.RequestKind(kind)] = d
	}
	return delivery.RequestSettings{
		TakeBy: c.RequestTakeBy(), Due: c.RequestDue(), DueByKind: byKind,
		ReminderInterval:        c.ReminderInterval(),
		RemindersPerSessionHour: c.SessionReminderLimit(),
		RemindersPerHostHour:    c.HostReminderLimit(),
	}
}

// requestSessions reads datawatch's session records.
type requestSessions struct{ m *session.Manager }

// sessionLive reports a session datawatch has not seen end.
func sessionLive(state session.State) bool {
	return state != session.StateComplete && state != session.StateFailed && state != session.StateKilled
}

// Info answers for a full session ID only.
func (b requestSessions) Info(id string) (delivery.SessionInfo, bool) {
	s, ok := b.m.GetSession(id)
	if !ok || s.FullID != id {
		return delivery.SessionInfo{}, false
	}
	return delivery.SessionInfo{ID: s.FullID, Family: s.BackendFamily, ProjectDir: s.ProjectDir, Live: sessionLive(s.State)}, true
}

// requestHolds says why a session gets no reminder and why its clocks stop.
// Each source is a function, nil when that part is not running. A burn guard
// holds work by setting a usage back-off, so usageBlocked covers it.
type requestHolds struct {
	session func(id string) (*session.Session, bool)
	// paused: the operator paused all work, or this project.
	paused func(projectDir string) bool
	// usageBlocked: a usage back-off (the operator's, a limit's, or the burn
	// guard's) holds this backend and model.
	usageBlocked func(backend, model string) bool
	// budgetBlocked: the backend's dollar budget is spent.
	budgetBlocked func(backend string) bool
	// automationAtCeiling: the Automation is at its dollar ceiling.
	automationAtCeiling func(automationID string) bool
	// automationOf names the Automation of an Automaton.
	automationOf func(automatonID string) string
}

// Held implements delivery.Holds.
func (h requestHolds) Held(sessionID string, scope delivery.Scope) (string, bool) {
	var sess *session.Session
	if sessionID != "" && h.session != nil {
		sess, _ = h.session(sessionID)
	}
	dir := ""
	if sess != nil {
		dir = sess.ProjectDir
	}
	if h.paused != nil && h.paused(dir) {
		return delivery.WhyHeldPause, true
	}
	if sess != nil {
		if sess.State == session.StateRateLimited {
			return delivery.WhyHeldRateLimit, true
		}
		seen := map[string]bool{}
		for _, backend := range []string{sess.LLMRef, sess.BackendFamily} {
			if backend == "" || seen[backend] {
				continue
			}
			seen[backend] = true
			if h.usageBlocked != nil && h.usageBlocked(backend, sess.Model) {
				return delivery.WhyHeldUsage, true
			}
			if h.budgetBlocked != nil && h.budgetBlocked(backend) {
				return delivery.WhyHeldUsage, true
			}
		}
	}
	if h.automationAtCeiling == nil {
		return "", false
	}
	var automations []string
	if scope.Kind == delivery.ScopeAutomation {
		automations = append(automations, scope.ID)
	}
	if h.automationOf != nil {
		if scope.Kind == delivery.ScopeAutomaton {
			automations = append(automations, h.automationOf(scope.ID))
		}
		if sess != nil && sess.PRDID != "" {
			automations = append(automations, h.automationOf(sess.PRDID))
		}
	}
	for _, a := range automations {
		if a != "" && h.automationAtCeiling(a) {
			return delivery.WhyHeldUsage, true
		}
	}
	return "", false
}

// holdsFor builds the hold sources the daemon has.
func (w *deliveryWiring) holdsFor(d deliveryDeps) requestHolds {
	h := requestHolds{
		session:      d.sessions.GetSession,
		usageBlocked: func(backend, model string) bool { ok, _ := autonomousBackoff.BlockedModel(backend, model); return ok },
		budgetBlocked: func(backend string) bool {
			if autonomousBudget == nil {
				return false
			}
			ok, _ := autonomousBudget.Blocked(backend)
			return ok
		},
	}
	if p := d.pause; p != nil {
		h.paused = func(dir string) bool { _, blocked := p.Blocked(dir); return blocked }
	}
	if m := d.autonomy; m != nil {
		h.automationOf = func(id string) string {
			if prd, ok := m.Store().GetPRD(id); ok {
				return prd.AutomationID
			}
			return ""
		}
		h.automationAtCeiling = func(id string) bool { return automationAtCeiling(m, id) }
	}
	return h
}

func automationAtCeiling(m *autonomouspkg.Manager, id string) bool {
	if m.AutomationBudget() == nil {
		return false
	}
	a, ok := m.GetAutomation(id)
	if !ok || a == nil {
		return false
	}
	v := m.BudgetViewFor(a)
	return v != nil && v.CeilingReached
}

// acceptanceOptions are the "done when" lists as datawatch runs them for
// requests: sealed with the delivery store key, receipts and versions read
// from the request records, and no command run.
func acceptanceOptions(cfg *config.Config, key []byte, book func() delivery.RequestBook) acceptance.Options {
	return acceptance.Options{
		Path:     filepath.Join(expandHome(cfg.DataDir), "acceptance.json"),
		Key:      key,
		Mode:     func() string { return cfg.Acceptance.Mode },
		Settings: func() acceptance.Settings { return acceptanceSettings(cfg) },
		Runner:   delivery.RefusingRunner{},
		Resolver: delivery.NewRequestResolver(book),
		Probes:   map[string]acceptance.Probe{acceptance.MethodReceipt: delivery.NewReceiptProbe(book)},
	}
}

// openRequests opens the store, repairing a damaged one. A sound store is only
// read. An error means requests stay off; it never stops the daemon.
func (w *deliveryWiring) openRequests(d deliveryDeps, engine *delivery.Engine, lists *acceptance.Service, adopt bool) (*delivery.Requests, *delivery.RepairReport, error) {
	sessions := requestSessions{d.sessions}
	holds := w.holdsFor(d)
	keeper := delivery.NewRoleKeeper(sessions)
	verifier := delivery.NewListsVerifier(lists)
	settings := func() delivery.RequestSettings { return requestSettings(d.cfg) }
	opts := delivery.RequestOptions{
		Dir: filepath.Join(expandHome(d.cfg.DataDir), requestsDir), Key: w.key.Key(), AdoptPlain: adopt,
		Host: d.cfg.Hostname, Interval: d.cfg.Delivery.Interval(), Settings: settings,
		Roles: keeper, Verifier: verifier, Sessions: sessions, Holds: holds,
		Notifier: delivery.NewNotifier(delivery.NotifierOptions{Typer: newRequestTyper(d), Holds: holds,
			Sessions: sessions, Roles: keeper, Verifier: verifier, Settings: settings}),
		Starter:      requestHolderStarter(d),
		RulesVersion: requestRulesVersion(d),
	}
	if engine != nil {
		opts.Record = engine.Record
		opts.Backlog = func(key string) (int, bool) {
			for _, e := range engine.Report().Backlog {
				if e.Key == key {
					return e.Episode, true
				}
			}
			return 0, false
		}
	}
	return delivery.OpenRequestsRepaired(opts)
}

// tell logs, raises a system alert, and, with a delivery list, lists it there.
func (w *deliveryWiring) tell(d deliveryDeps, e *delivery.Engine, entry, state, title, body string) {
	log.Printf("[delivery] %s: %s", title, body)
	if d.alert != nil {
		d.alert(title, body)
	}
	if e == nil {
		return
	}
	if err := e.Record(delivery.Candidate{Kind: delivery.KindAlarm, ID: entry, State: state, Problem: state,
		Invariant: delivery.InvUnopenable, Since: time.Now(), Sticky: true, WaitsOn: delivery.WaitsOnOperator}); err != nil {
		log.Printf("[delivery] %s held in memory until it can be saved: %v", entry, err)
	}
}

func (w *deliveryWiring) tellRepaired(d deliveryDeps, e *delivery.Engine, r *delivery.RepairReport) {
	w.tell(d, e, "requests_repaired:"+filepath.Base(r.Folder), delivery.WhyRepaired,
		"Delivery request store repaired", r.Text())
}

func (w *deliveryWiring) tellRequestsOff(d deliveryDeps, e *delivery.Engine, err error) {
	w.tell(d, e, "requests_off", "requests_off", "Delivery requests are off",
		"The delivery request store in "+filepath.Join(expandHome(d.cfg.DataDir), requestsDir)+
			" cannot be used or repaired safely. Nothing in it was changed. Delivery requests are off, and so is "+
			"the work queue if it had moved there. Everything else runs. Reason: "+err.Error())
}

func (w *deliveryWiring) tellKeyUnusable(d deliveryDeps, err error) {
	w.tell(d, nil, "requests_off", "requests_off", "Delivery requests are off",
		"datawatch cannot use the delivery store key. Delivery requests are off, and so is the delivery check, "+
			"whose records that key seals. Done-when lists are unavailable, and so is the work queue if it had moved "+
			"into queue.d. Nothing under queue.d or delivery was changed. Everything else runs. Reason: "+err.Error())
}

// requestSource is the delivery check's "requests" source.
func (w *deliveryWiring) requestSource() delivery.Source {
	return delivery.Source{Name: "requests", Collect: func(ctx context.Context, now time.Time) ([]delivery.Candidate, error) {
		r := w.requestStore()
		if r == nil {
			return nil, errors.New("the request store is not open")
		}
		return r.Candidates(ctx, now)
	}}
}

// requestsHealthy keeps the heartbeat stale while requests cannot be saved.
func (w *deliveryWiring) requestsHealthy() error {
	if r := w.requestStore(); r != nil {
		return r.Healthy()
	}
	return nil
}

// moveQueue puts the older work queue into the request store. When it cannot,
// the queue stays where it is: in queue.json, or, once moved, unreadable.
func moveQueue(d deliveryDeps, reqs *delivery.Requests) {
	if d.queue == nil {
		return
	}
	path := queueStorePath(d.cfg)
	moved := 0
	err := d.queue.MoveTo(func() (session.QueueRecords, error) {
		n, err := reqs.ImportQueue(path)
		moved = n
		return reqs, err
	})
	switch {
	case err != nil:
		log.Printf("[delivery] the work queue was not moved into %s: %v", requestsDir, err)
	case moved > 0:
		log.Printf("[delivery] the work queue moved into %s (%d items) and queue.json is now a marker. "+
			"Overwriting is not reliable on an SSD or a copy-on-write disk, so the old plain content may remain on disk. "+
			"To go back: datawatch queue export", requestsDir, moved)
	}
}

// startRequests wires the open store: the surfaces, the queue, the sources,
// and the check every interval.
func (w *deliveryWiring) startRequests(ctx context.Context, d deliveryDeps, reqs *delivery.Requests) {
	w.mu.Lock()
	w.requests, w.ctx = reqs, ctx
	w.mu.Unlock()
	if d.http != nil {
		d.http.SetRequests(reqs)
	}
	moveQueue(d, reqs)
	startRequestSources(ctx, d, reqs)
	startRequestRules(ctx, d, reqs)
	delay := d.firstPassDelay
	if delay == 0 {
		delay = deliveryFirstPassDelay
	}
	interval := d.cfg.Delivery.Interval()
	go func() {
		t := time.NewTimer(delay)
		defer t.Stop()
		last := ""
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			line := ""
			if err := reqs.Tick(ctx); err != nil {
				line = err.Error()
			}
			// Logged when it changes. Errors hold IDs and codes only.
			if line != last && line != "" {
				log.Printf("[delivery] requests: %s", line)
			}
			last = line
			t.Reset(interval)
		}
	}()
}

// sessionEnded tells the store that datawatch saw a session end. One it misses
// is found by the next check.
func (w *deliveryWiring) sessionEnded(fullID string) {
	w.mu.Lock()
	reqs, ctx := w.requests, w.ctx
	w.mu.Unlock()
	if reqs == nil {
		return
	}
	// Not in the caller's own call: the session manager may be busy.
	go func() {
		if err := reqs.HolderEnded(ctx, fullID); err != nil {
			log.Printf("[delivery] requests: session %s ended: %v", fullID, err)
		}
	}()
}

// openLists opens the "done when" lists with the delivery store key and wires
// them to the web server. A store that cannot be used leaves them unavailable
// and is never replaced.
func openLists(d deliveryDeps, key []byte, adopt bool, book func() delivery.RequestBook) *acceptance.Service {
	opts := acceptanceOptions(d.cfg, key, book)
	if key != nil {
		data, err := os.ReadFile(opts.Path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
		case err != nil:
			log.Printf("[acceptance] %v", err)
			return nil
		case !secfile.IsEncrypted(data) && !adopt:
			log.Printf("[acceptance] %s is plain on a secure install, which is a foreign write: the lists stay unavailable", opts.Path)
			return nil
		case !secfile.IsEncrypted(data):
			if err := secfile.WriteFileDurable(opts.Path, data, 0o600, key); err != nil {
				log.Printf("[acceptance] sealing the lists failed: %v", err)
				return nil
			}
		}
	}
	return wireAcceptance(opts, d.http)
}

// startOff is the start with the delivery check switched off. Requests are not
// wired, as before they existed. A queue that already moved is still served.
func (w *deliveryWiring) startOff(d deliveryDeps) error {
	// A key that was never written would seal what nobody could open again.
	if w.key.IsNew() {
		if d.http != nil {
			log.Printf("[acceptance] the lists are unavailable: the delivery store key is only written when the delivery check runs")
		}
	} else {
		openLists(d, w.key.Key(), false, w.requestBook)
	}
	if d.queue == nil || !queueMoved(queueStorePath(d.cfg)) {
		return nil
	}
	reqs, repair, err := delivery.OpenRequestsRepaired(delivery.RequestOptions{
		Dir: filepath.Join(expandHome(d.cfg.DataDir), requestsDir), Key: w.key.Key(), AdoptPlain: w.key.AdoptPlain(),
	})
	if err != nil {
		w.tellRequestsOff(d, nil, err)
		return nil
	}
	if repair != nil {
		w.tellRepaired(d, nil, repair)
	}
	if err := d.queue.MoveTo(func() (session.QueueRecords, error) { return reqs, nil }); err != nil {
		log.Printf("[delivery] the work queue cannot be read: %v", err)
	}
	return nil
}

// ---- the older queue --------------------------------------------------------

// queueMoved reports that queue.json is the marker.
func queueMoved(path string) bool {
	data, err := os.ReadFile(path) // #nosec G304 -- the daemon's own data file
	return err == nil && string(data) == delivery.QueueMarker
}

// openStartupQueue is the queue early in the start. Once moved, it waits for
// the request store, which opens last.
func openStartupQueue(path string) (*session.QueueStore, error) {
	if queueMoved(path) {
		return session.NewDeferredQueueStore(), nil
	}
	return session.NewQueueStore(path)
}

// daemonQueue is the queue for the CLI and the standalone MCP server: the
// running daemon's. It opens no file.
func daemonQueue() *session.QueueStore {
	return session.NewRemoteQueueStore(func(method, path string, body any) ([]byte, error) {
		return daemonJSONBody(method, path, body)
	})
}

// daemonRunning reports a live daemon over this data folder.
func daemonRunning(dataDir string) (int, bool) {
	data, err := os.ReadFile(filepath.Join(dataDir, "daemon.pid")) // #nosec G304 -- the daemon's own file
	if err != nil {
		return 0, false
	}
	var pid int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &pid); err != nil || pid <= 0 || pid == os.Getpid() {
		return 0, false
	}
	p, err := os.FindProcess(pid)
	if err != nil || p.Signal(syscall.Signal(0)) != nil {
		return 0, false
	}
	return pid, true
}

// exportQueue writes the queue back to queue.json so that an older datawatch
// can be started. The daemon must be stopped.
func exportQueue(cfg *config.Config, key []byte, plain bool) (int, error) {
	dataDir := expandHome(cfg.DataDir)
	if pid, running := daemonRunning(dataDir); running {
		return 0, fmt.Errorf("datawatch is running (PID %d): stop it first, with 'datawatch stop'", pid)
	}
	path := queueStorePath(cfg)
	if _, err := os.Stat(path); err == nil && !queueMoved(path) {
		return 0, fmt.Errorf("%s already holds a queue; there is nothing to export, and it was not changed", path)
	}
	reqs, err := delivery.OpenRequests(delivery.RequestOptions{Dir: filepath.Join(dataDir, requestsDir), Key: key})
	if err != nil {
		return 0, err
	}
	return reqs.ExportQueue(path, plain)
}

func newQueueExportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Write the queue back to queue.json, to go back to an older datawatch (the daemon must be stopped)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := newDeliveryWiring()
			cfg, encKey, err := loadConfigDeriveKeys(w.openKey)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			if encKey == nil {
				if err := w.openKey(cfg, nil); err != nil {
					return err
				}
			}
			defer zeroBytes(encKey)
			plain, _ := cmd.Flags().GetBool("plain")
			n, err := exportQueue(cfg, w.key.Key(), plain)
			if err != nil {
				return err
			}
			fmt.Printf("%d queue items written to %s as plain JSON\n", n, queueStorePath(cfg))
			return nil
		},
	}
	cmd.Flags().Bool("plain", false, "Required on a secure install: the file written is plain content")
	return cmd
}

// ---- pi ---------------------------------------------------------------------

// piFamily reports the sessions the datawatch pi extension is for.
func piFamily(family string) bool { return family == "pi" || strings.HasPrefix(family, "pi-") }

// piPreLaunch gives a pi session its own credential, whether or not Claude's
// channel is on.
type piPreLaunch struct {
	cfg *config.Config
	// mint issues the token of one session.
	mint   func(sessionID string) (string, error)
	setEnv func(tmuxSession, key, value string) error
	wire   func(*session.Session, piDeliveryLaunch) error
	// revoke ends one session's token. Without Claude's channel, the session
	// end hook is the only place that can.
	revoke func(sessionID string) error
}

// run wires one session before it starts. token is the session's own token
// when one was already minted: a second mint would revoke it.
func (p piPreLaunch) run(sess *session.Session, token string) {
	sess.DeliveryExtension = ""
	if !piFamily(sess.BackendFamily) {
		return
	}
	if token == "" {
		if p.mint == nil {
			return
		}
		tok, err := p.mint(sess.FullID)
		if err != nil {
			fmt.Printf("[warn] mint session token for %s: %v; its requests cannot be confirmed\n", sess.FullID, err)
			return
		}
		token = tok
	}
	in := piDeliveryLaunch{DataDir: expandHome(p.cfg.DataDir), APIURL: loopbackBaseURL(p.cfg), Token: token,
		SetSessionEnv: p.setEnv}
	if p.cfg.Server.TLSEnabled {
		in.CACertPath = daemonCertPath(p.cfg)
	}
	if err := p.wire(sess, in); err != nil {
		fmt.Printf("[warn] wire pi delivery for %s: %v; its requests cannot be confirmed\n", sess.FullID, err)
	}
}

// sessionHooks is the part of the session manager the hooks without Claude's
// channel use.
type sessionHooks interface {
	SetOnPreLaunch(func(*session.Session))
	SetOnSessionEnd(func(*session.Session))
}

// end takes back what run gave an ended session: its token and its
// credential file.
func (p piPreLaunch) end(sess *session.Session) {
	if !piFamily(sess.BackendFamily) {
		return
	}
	if p.revoke != nil {
		if err := p.revoke(sess.FullID); err != nil {
			fmt.Printf("[warn] revoke session token for %s: %v\n", sess.FullID, err)
		}
	}
	endPiDelivery(sess, expandHome(p.cfg.DataDir))
}

// hooksWithoutChannel sets what requests need when Claude's channel is off: pi
// still gets its credential and loses it at its end, and session ends are
// still reported.
func (w *deliveryWiring) hooksWithoutChannel(m sessionHooks, pi piPreLaunch) {
	m.SetOnPreLaunch(func(sess *session.Session) { pi.run(sess, "") })
	m.SetOnSessionEnd(func(sess *session.Session) {
		w.sessionEnded(sess.FullID)
		pi.end(sess)
	})
}
