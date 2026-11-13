package plugins

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/alerts"
)

// recordedAlert / recordedAudit are the sinks the recorder tests feed
// to capture what the recorder emits.
type recordedAlert struct {
	level alerts.Level
	title string
	body  string
}

type recordedAudit struct {
	action  string
	subject string
	state   string
	details map[string]any
}

type eventSinks struct {
	mu     sync.Mutex
	alerts []recordedAlert
	audits []recordedAudit
}

func (s *eventSinks) addAlert(a recordedAlert) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.alerts = append(s.alerts, a)
}

func (s *eventSinks) addAudit(a recordedAudit) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.audits = append(s.audits, a)
}

func (s *eventSinks) countAlerts() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.alerts)
}

func (s *eventSinks) countAudits() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.audits)
}

func (s *eventSinks) lastAlert() recordedAlert {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.alerts) == 0 {
		return recordedAlert{}
	}
	return s.alerts[len(s.alerts)-1]
}

func (s *eventSinks) lastAudit() recordedAudit {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.audits) == 0 {
		return recordedAudit{}
	}
	return s.audits[len(s.audits)-1]
}

func newTestRecorder(sinks *eventSinks, opts ServiceEventOptions) *ServiceEventRecorder {
	opts.Alert = func(level alerts.Level, title, body string) {
		sinks.addAlert(recordedAlert{level: level, title: title, body: body})
	}
	opts.Audit = func(action, subject string, details map[string]any) {
		st, _ := details["state"].(string)
		sinks.addAudit(recordedAudit{action: action, subject: subject, state: st, details: details})
	}
	return NewServiceEventRecorder(opts)
}

// containsIgnoreCase reports whether s contains sub case-insensitively.
func containsIgnoreCase(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}

// TestServiceEvents_ServiceDown covers the service_down condition:
// raise-once at the threshold, no re-raise on further failures, clear
// on a passing probe, and re-raise after a new failure run.
func TestServiceEvents_ServiceDown(t *testing.T) {
	sinks := &eventSinks{}
	r := newTestRecorder(sinks, ServiceEventOptions{}) // default threshold 3

	// Two failures below the default threshold: nothing at all.
	r.RecordHealthProbe("imap-mcp", false)
	r.RecordHealthProbe("imap-mcp", false)
	if n := sinks.countAlerts(); n != 0 {
		t.Fatalf("below threshold: got %d alerts, want 0", n)
	}
	if n := sinks.countAudits(); n != 0 {
		t.Fatalf("below threshold: got %d audits, want 0", n)
	}

	// Third failure raises service_down once.
	r.RecordHealthProbe("imap-mcp", false)
	if n := sinks.countAlerts(); n != 1 {
		t.Fatalf("at threshold: got %d alerts, want 1", n)
	}
	al := sinks.lastAlert()
	if al.level != alerts.LevelWarn {
		t.Errorf("raise alert level = %q, want warn", al.level)
	}
	if al.title != "Plugin imap-mcp is down" {
		t.Errorf("raise alert title = %q", al.title)
	}
	au := sinks.lastAudit()
	if au.action != "plugin_service_down" {
		t.Errorf("raise audit action = %q, want plugin_service_down", au.action)
	}
	if au.subject != "imap-mcp" {
		t.Errorf("raise audit subject = %q, want imap-mcp", au.subject)
	}
	if au.state != "raised" {
		t.Errorf("raise audit state = %q, want raised", au.state)
	}
	if got := au.details["consecutive_failures"]; got != 3 {
		t.Errorf("consecutive_failures = %v, want 3", got)
	}
	if got := au.details["threshold"]; got != DefaultDownFailureThreshold {
		t.Errorf("threshold detail = %v, want %d", got, DefaultDownFailureThreshold)
	}
	if !containsIgnoreCase(al.body, "imap-mcp") {
		t.Errorf("raise body does not name the plugin: %q", al.body)
	}

	// Raise once, never on every poll.
	r.RecordHealthProbe("imap-mcp", false)
	r.RecordHealthProbe("imap-mcp", false)
	if n := sinks.countAlerts(); n != 1 {
		t.Fatalf("after further failures: got %d alerts, want 1 (raise-once)", n)
	}
	if n := sinks.countAudits(); n != 1 {
		t.Fatalf("after further failures: got %d audits, want 1", n)
	}
	if d, _, _, _ := r.Active("imap-mcp", ""); !d {
		t.Errorf("Active down = false, want true while failed")
	}

	// A passing probe clears: recovery alert + cleared audit entry.
	r.RecordHealthProbe("imap-mcp", true)
	if n := sinks.countAlerts(); n != 2 {
		t.Fatalf("after recovery: got %d alerts, want 2", n)
	}
	re := sinks.lastAlert()
	if re.level != alerts.LevelInfo {
		t.Errorf("clear alert level = %q, want info", re.level)
	}
	if re.title != "Plugin imap-mcp is back up" {
		t.Errorf("clear alert title = %q", re.title)
	}
	ca := sinks.lastAudit()
	if ca.action != "plugin_service_down" || ca.state != "cleared" {
		t.Errorf("clear audit = action %q state %q, want plugin_service_down / cleared", ca.action, ca.state)
	}
	if d, _, _, _ := r.Active("imap-mcp", ""); d {
		t.Errorf("Active down = true after clear, want false")
	}

	// Healthy probes while never down raise nothing.
	r.RecordHealthProbe("imap-mcp", true)
	r.RecordHealthProbe("imap-mcp", true)
	if n := sinks.countAlerts(); n != 2 {
		t.Fatalf("healthy probes: got %d alerts, want 2 (no new events)", n)
	}

	// A new failure run after the clear re-raises on the next
	// transition (raise once per transition, both directions).
	r.RecordHealthProbe("imap-mcp", false)
	r.RecordHealthProbe("imap-mcp", false)
	r.RecordHealthProbe("imap-mcp", false)
	if n := sinks.countAlerts(); n != 3 {
		t.Fatalf("re-raise: got %d alerts, want 3", n)
	}
	if a := sinks.lastAlert(); a.level != alerts.LevelWarn || a.title != "Plugin imap-mcp is down" {
		t.Errorf("re-raise alert = %+v", a)
	}
}

// TestServiceEvents_ServiceDownCustomThreshold covers the threshold
// being a parameter, not buried in the logic.
func TestServiceEvents_ServiceDownCustomThreshold(t *testing.T) {
	sinks := &eventSinks{}
	r := newTestRecorder(sinks, ServiceEventOptions{DownFailureThreshold: 7})

	for i := 0; i < 6; i++ {
		r.RecordHealthProbe("slack-mcp", false)
	}
	if n := sinks.countAlerts(); n != 0 {
		t.Fatalf("6 failures with threshold 7: got %d alerts, want 0", n)
	}
	r.RecordHealthProbe("slack-mcp", false)
	if n := sinks.countAlerts(); n != 1 {
		t.Fatalf("7th failure: got %d alerts, want 1", n)
	}
	if got := sinks.lastAudit().details["threshold"]; got != 7 {
		t.Errorf("threshold detail = %v, want 7", got)
	}
}

// TestServiceEvents_ServiceDownIsPerPlugin checks that one plugin's
// failures never move another plugin's condition.
func TestServiceEvents_ServiceDownIsPerPlugin(t *testing.T) {
	sinks := &eventSinks{}
	r := newTestRecorder(sinks, ServiceEventOptions{})

	for i := 0; i < 3; i++ {
		r.RecordHealthProbe("imap-mcp", false)
	}
	r.RecordHealthProbe("slack-mcp", false)
	r.RecordHealthProbe("slack-mcp", false)
	if n := sinks.countAlerts(); n != 1 {
		t.Fatalf("got %d alerts, want 1 (imap-mcp only)", n)
	}
	down, _, _, _ := r.Active("slack-mcp", "")
	if down {
		t.Errorf("slack-mcp reported down although only imap-mcp failed")
	}
}

// TestServiceEvents_TokenRejected covers the token_rejected condition:
// a 401/403 on a probe or a widget pull raises once; a successful
// answer clears; other statuses change nothing; re-rejection
// re-raises.
func TestServiceEvents_TokenRejected(t *testing.T) {
	sinks := &eventSinks{}
	r := newTestRecorder(sinks, ServiceEventOptions{})

	// 500s neither raise nor clear.
	r.RecordHTTPStatus("imap-mcp", "probe", 500)
	if n := sinks.countAlerts(); n != 0 {
		t.Fatalf("500: got %d alerts, want 0", n)
	}

	// 401 on a probe raises token_rejected once.
	r.RecordHTTPStatus("imap-mcp", "probe", 401)
	if n := sinks.countAlerts(); n != 1 {
		t.Fatalf("401: got %d alerts, want 1", n)
	}
	al := sinks.lastAlert()
	if al.title != "Plugin imap-mcp token rejected" {
		t.Errorf("raise title = %q", al.title)
	}
	if !containsIgnoreCase(al.body, "Settings") {
		t.Errorf("raise body does not say to check Settings: %q", al.body)
	}
	au := sinks.lastAudit()
	if au.action != "plugin_service_token_rejected" || au.state != "raised" {
		t.Errorf("raise audit = action %q state %q", au.action, au.state)
	}
	if got := au.details["status_code"]; got != 401 {
		t.Errorf("status_code = %v, want 401", got)
	}
	if got := au.details["source"]; got != "probe" {
		t.Errorf("source = %q, want probe", got)
	}

	// A second 401 (say, a widget pull) must not re-raise.
	r.RecordHTTPStatus("imap-mcp", "widget_pull", 403)
	if n := sinks.countAlerts(); n != 1 {
		t.Fatalf("second 4xx: got %d alerts, want 1 (raise-once)", n)
	}

	// A successful answer (widget pull 200) clears.
	r.RecordHTTPStatus("imap-mcp", "widget_pull", 200)
	if n := sinks.countAlerts(); n != 2 {
		t.Fatalf("200: got %d alerts, want 2 (recovery)", n)
	}
	re := sinks.lastAlert()
	if re.level != alerts.LevelInfo {
		t.Errorf("clear level = %q, want info", re.level)
	}
	ca := sinks.lastAudit()
	if ca.action != "plugin_service_token_rejected" || ca.state != "cleared" {
		t.Errorf("clear audit = action %q state %q", ca.action, ca.state)
	}
	if _, tok, _, _ := r.Active("imap-mcp", ""); tok {
		t.Errorf("token condition still active after clear")
	}

	// A 403 from a widget pull while clean re-raises.
	r.RecordHTTPStatus("imap-mcp", "widget_pull", 403)
	if n := sinks.countAlerts(); n != 3 {
		t.Fatalf("403 again: got %d alerts, want 3", n)
	}
	if got := sinks.lastAudit().details["status_code"]; got != 403 {
		t.Errorf("re-raise status_code = %v, want 403", got)
	}
}

// TestServiceEvents_BacklogGrowing covers the
// enrichment_backlog_growing condition: N consecutive rises at >= the
// minimum rise raise once; a flat or falling pull clears; the first
// observation only establishes the baseline.
func TestServiceEvents_BacklogGrowing(t *testing.T) {
	sinks := &eventSinks{}
	r := newTestRecorder(sinks, ServiceEventOptions{}) // N=3, minRise=1

	// First observation is the baseline: no event.
	r.RecordBacklogCount("imap-mcp", 10)
	// Second rises by 1: streak 1, below N=3.
	r.RecordBacklogCount("imap-mcp", 11)
	// Third rise: streak 2.
	r.RecordBacklogCount("imap-mcp", 13)
	if n := sinks.countAlerts(); n != 0 {
		t.Fatalf("streak 2 of 3: got %d alerts, want 0", n)
	}

	// Fourth rise reaches streak 3: raise once, warn, named audit.
	r.RecordBacklogCount("imap-mcp", 16)
	if n := sinks.countAlerts(); n != 1 {
		t.Fatalf("streak 3 of 3: got %d alerts, want 1", n)
	}
	al := sinks.lastAlert()
	if al.level != alerts.LevelWarn || al.title != "Plugin imap-mcp backlog is growing" {
		t.Errorf("raise alert = %+v", al)
	}
	au := sinks.lastAudit()
	if au.action != "plugin_service_enrichment_backlog" || au.state != "raised" {
		t.Errorf("raise audit = action %q state %q", au.action, au.state)
	}
	if got := au.details["streak"]; got != 3 {
		t.Errorf("streak = %v, want 3", got)
	}
	if got := au.details["last_pending"]; got != 16 {
		t.Errorf("last_pending = %v, want 16", got)
	}

	// Still rising while active: no further events.
	r.RecordBacklogCount("imap-mcp", 20)
	if n := sinks.countAlerts(); n != 1 {
		t.Fatalf("rising while active: got %d alerts, want 1 (raise-once)", n)
	}

	// A falling pull ends the streak: recovery alert + cleared audit.
	r.RecordBacklogCount("imap-mcp", 9)
	if n := sinks.countAlerts(); n != 2 {
		t.Fatalf("falling: got %d alerts, want 2 (recovery)", n)
	}
	if got := sinks.lastAudit().state; got != "cleared" {
		t.Errorf("clear audit state = %q, want cleared", got)
	}
	if _, _, bk, _ := r.Active("imap-mcp", ""); bk {
		t.Errorf("backlog condition still active after a falling pull")
	}

	// A flat pull also ends the streak (a flat pull is not a rise).
	r.RecordBacklogCount("imap-mcp", 100) // baseline
	r.RecordBacklogCount("imap-mcp", 101)
	r.RecordBacklogCount("imap-mcp", 103)
	r.RecordBacklogCount("imap-mcp", 106) // active
	r.RecordBacklogCount("imap-mcp", 106) // flat: rise 0 -> clears
	if got := sinks.lastAudit(); got.action != "plugin_service_enrichment_backlog" || got.state != "cleared" {
		t.Errorf("flat pull audit = action %q state %q, want cleared backlog entry", got.action, got.state)
	}
}

// TestServiceEvents_BacklogCustomParameters covers N and the minimum
// rise being parameters.
func TestServiceEvents_BacklogCustomParameters(t *testing.T) {
	// N = 2 rising pulls of at least 5 each.
	sinks := &eventSinks{}
	r := newTestRecorder(sinks, ServiceEventOptions{BacklogRisePulls: 2, BacklogMinRise: 5})

	r.RecordBacklogCount("mcp", 0) // baseline
	r.RecordBacklogCount("mcp", 3) // rise 3 < 5: does not count
	r.RecordBacklogCount("mcp", 9) // rise 6 >= 5: streak 1
	if n := sinks.countAlerts(); n != 0 {
		t.Fatalf("streak 1 of 2: got %d alerts, want 0", n)
	}
	r.RecordBacklogCount("mcp", 12) // rise 3 < 5: streak resets, no raise
	if n := sinks.countAlerts(); n != 0 {
		t.Fatalf("small rise: got %d alerts, want 0", n)
	}
	r.RecordBacklogCount("mcp", 20) // rise 8: streak 1
	r.RecordBacklogCount("mcp", 30) // rise 10: streak 2 -> raise
	if n := sinks.countAlerts(); n != 1 {
		t.Fatalf("streak 2 of 2: got %d alerts, want 1", n)
	}
	if got := sinks.lastAudit().details["pulls"]; got != 2 {
		t.Errorf("pulls detail = %v, want 2", got)
	}
	if got := sinks.lastAudit().details["min_rise"]; got != 5 {
		t.Errorf("min_rise detail = %v, want 5", got)
	}
}

// TestServiceEvents_BacklogIsPerPlugin checks that one plugin's streak
// never affects another plugin's.
func TestServiceEvents_BacklogIsPerPlugin(t *testing.T) {
	sinks := &eventSinks{}
	r := newTestRecorder(sinks, ServiceEventOptions{})

	// imap-mcp fully raises.
	for i, c := range []int{10, 11, 13, 16} {
		if i == 0 {
			r.RecordBacklogCount("imap-mcp", c)
		} else {
			r.RecordBacklogCount("imap-mcp", c)
		}
	}
	// slack-mcp has seen one rising pull only.
	r.RecordBacklogCount("slack-mcp", 5)
	r.RecordBacklogCount("slack-mcp", 9)
	if n := sinks.countAlerts(); n != 1 {
		t.Fatalf("got %d alerts, want 1 (imap-mcp only)", n)
	}
	if _, _, bk, _ := r.Active("slack-mcp", ""); bk {
		t.Errorf("slack-mcp backlog reported active with only one rise")
	}
}

// TestServiceEvents_ModelNodeUnreachable covers the
// model_node_unreachable condition: a non-answer raises once, the
// first answer clears, re-unreachability re-raises, and the condition
// is per (plugin, model) pair.
func TestServiceEvents_ModelNodeUnreachable(t *testing.T) {
	sinks := &eventSinks{}
	r := newTestRecorder(sinks, ServiceEventOptions{})

	// A reachable node never raises.
	r.RecordModelNode("voice", "whisper", true)
	if n := sinks.countAlerts(); n != 0 {
		t.Fatalf("reachable: got %d alerts, want 0", n)
	}

	// A non-answer raises once, with the named audit action.
	r.RecordModelNode("voice", "whisper", false)
	if n := sinks.countAlerts(); n != 1 {
		t.Fatalf("unreachable: got %d alerts, want 1", n)
	}
	al := sinks.lastAlert()
	if al.level != alerts.LevelWarn || al.title != "Plugin voice model node unreachable" {
		t.Errorf("raise alert = %+v", al)
	}
	if !containsIgnoreCase(al.body, "whisper") {
		t.Errorf("raise body does not name the model: %q", al.body)
	}
	au := sinks.lastAudit()
	if au.action != "plugin_service_model_node_unreachable" || au.state != "raised" {
		t.Errorf("raise audit = action %q state %q", au.action, au.state)
	}
	if au.subject != "voice/whisper" {
		t.Errorf("subject = %q, want voice/whisper", au.subject)
	}
	if got := au.details["model"]; got != "whisper" {
		t.Errorf("model detail = %v, want whisper", got)
	}

	// Repeated non-answers do not re-raise.
	r.RecordModelNode("voice", "whisper", false)
	if n := sinks.countAlerts(); n != 1 {
		t.Fatalf("second non-answer: got %d alerts, want 1 (raise-once)", n)
	}
	if _, _, _, node := r.Active("voice", "whisper"); !node {
		t.Errorf("Active node = false while unreachable")
	}

	// The first answer clears with a recovery alert.
	r.RecordModelNode("voice", "whisper", true)
	if n := sinks.countAlerts(); n != 2 {
		t.Fatalf("answer: got %d alerts, want 2 (recovery)", n)
	}
	if got := sinks.lastAudit().state; got != "cleared" {
		t.Errorf("clear audit state = %q, want cleared", got)
	}
	if _, _, _, node := r.Active("voice", "whisper"); node {
		t.Errorf("node still active after an answer")
	}

	// Unreachability again re-raises.
	r.RecordModelNode("voice", "whisper", false)
	if n := sinks.countAlerts(); n != 3 {
		t.Fatalf("unreachable again: got %d alerts, want 3", n)
	}
}

// TestServiceEvents_ModelNodeIsPerModel checks that models on the same
// plugin hold independent conditions.
func TestServiceEvents_ModelNodeIsPerModel(t *testing.T) {
	sinks := &eventSinks{}
	r := newTestRecorder(sinks, ServiceEventOptions{})

	r.RecordModelNode("voice", "whisper", false)
	r.RecordModelNode("voice", "llama", true)
	if n := sinks.countAlerts(); n != 1 {
		t.Fatalf("got %d alerts, want 1 (whisper only)", n)
	}
	if _, _, _, anyNode := r.Active("voice", ""); !anyNode {
		t.Errorf("Active voice (any model) = false although whisper is down")
	}
	if _, _, _, node := r.Active("voice", "llama"); node {
		t.Errorf("llama reported unreachable although only whisper is down")
	}
	// whisper recovers: Active for any model must go false too.
	r.RecordModelNode("voice", "whisper", true)
	if _, _, _, anyNode := r.Active("voice", ""); anyNode {
		t.Errorf("Active voice (any model) still true after whisper recovered")
	}
}

// TestServiceEvents_DefaultsApplied checks that zero-value options
// normalise to the documented defaults and that Thresholds reports
// them.
func TestServiceEvents_DefaultsApplied(t *testing.T) {
	r := NewServiceEventRecorder(ServiceEventOptions{
		Alert: func(alerts.Level, string, string) {},
	})
	down, pulls, minRise := r.Thresholds()
	if down != DefaultDownFailureThreshold {
		t.Errorf("default down threshold = %d, want %d", down, DefaultDownFailureThreshold)
	}
	if pulls != DefaultBacklogRisePulls {
		t.Errorf("default backlog pulls = %d, want %d", pulls, DefaultBacklogRisePulls)
	}
	if minRise != DefaultBacklogMinRise {
		t.Errorf("default backlog min rise = %d, want %d", minRise, DefaultBacklogMinRise)
	}
}

// TestServiceEvents_NilAuditSinkDropsAuditOnly checks that with a nil
// audit sink the alerts still go out and nothing panics.
func TestServiceEvents_NilAuditSinkDropsAuditOnly(t *testing.T) {
	sinks := &eventSinks{}
	r := newTestRecorder(sinks, ServiceEventOptions{})
	r.auditSink = nil // drop audit entries explicitly

	for i := 0; i < DefaultDownFailureThreshold; i++ {
		r.RecordHealthProbe("mcp", false)
	}
	if n := sinks.countAlerts(); n != 1 {
		t.Fatalf("got %d alerts, want 1 even with no audit sink", n)
	}
	if n := sinks.countAudits(); n != 0 {
		t.Fatalf("got %d audits, want 0 with nil audit sink", n)
	}
}

// TestServiceEvents_ConcurrentFeeds is a race smoke test: many
// goroutines feed the same recorder for different plugins.
func TestServiceEvents_ConcurrentFeeds(t *testing.T) {
	sinks := &eventSinks{}
	r := newTestRecorder(sinks, ServiceEventOptions{})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		plugin := fmt.Sprintf("p%d", i%4)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 256; j++ {
				r.RecordHealthProbe(plugin, j%7 != 0)
				r.RecordHTTPStatus(plugin, "probe", 200+j%3)
				if j%3 == 200 {
					r.RecordHTTPStatus(plugin, "widget_pull", 401)
				}
				r.RecordBacklogCount(plugin, j%50)
				r.RecordModelNode(plugin, "m", j%11 != 0)
			}
		}()
	}
	wg.Wait()
	// No assertion on counts; the race detector plus not panicking is
	// the check. But the recorder must end in a consistent state.
	for i := 0; i < 4; i++ {
		d, tk, bk, nd := r.Active(fmt.Sprintf("p%d", i), "")
		_ = d
		_ = tk
		_ = bk
		_ = nd
	}
}
