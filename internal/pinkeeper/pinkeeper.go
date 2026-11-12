package pinkeeper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/capacity"
)

const (
	// psTimeout bounds one GET /api/ps check.
	psTimeout = 5 * time.Second
	// pinTimeout bounds the load request. Loading a large model from disk
	// can take minutes.
	pinTimeout = 10 * time.Minute
	// unlimitedAfter is how far ahead of now an expires_at must sit to count
	// as unlimited. Ollama reports keep_alive -1 as a far-future or zero time.
	unlimitedAfter = 100 * 365 * 24 * time.Hour
	// maxErrBody caps how much of a failed response is quoted in an error.
	maxErrBody = 512
)

// Options configures a PinKeeper for one pinned compute node.
type Options struct {
	// Node is the compute node name, used in logs and alerts.
	Node string
	// Address is the node's ollama base URL, for example http://gpu1:11434.
	Address string
	// Model is the model the node is pinned to.
	Model string
	// Interval is the time between checks (pin_check_seconds). A zero or
	// negative interval disables the keeper: Start does nothing.
	Interval time.Duration
	// Busy reports whether a session is working on the named node, so a
	// re-pin would race a load. Nil means the node is never busy. See
	// LedgerBusy for the default.
	Busy func(node string) bool
	// Alerts receives a warning when an eviction forces a re-pin. Nil
	// disables alerting.
	Alerts *alerts.Store
	// Logf writes one line per decision. Nil uses the standard logger.
	Logf func(format string, args ...any)
	// Client sends the ollama requests. Nil uses http.DefaultClient; each
	// request carries its own timeout.
	Client *http.Client
}

// PinKeeper re-pins one node's model when ollama has dropped it.
// It is safe for concurrent use.
type PinKeeper struct {
	opt    Options
	client *http.Client

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

// New builds a PinKeeper. It starts nothing until Start is called.
func New(opt Options) *PinKeeper {
	client := opt.Client
	if client == nil {
		client = http.DefaultClient
	}
	return &PinKeeper{opt: opt, client: client}
}

// Start begins checking the node every Interval until ctx is cancelled or
// Stop is called. It returns at once. With a zero Interval, or with no
// model, Start is a no-op and nothing is requested from the node.
// Calling Start on a running keeper does nothing.
func (k *PinKeeper) Start(ctx context.Context) {
	if k.opt.Interval <= 0 || k.opt.Model == "" {
		return
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.cancel != nil {
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	k.cancel, k.done = cancel, done
	go func() {
		defer close(done)
		ticker := time.NewTicker(k.opt.Interval)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				k.tick(runCtx)
			}
		}
	}()
}

// Stop ends the check loop and waits for an in-flight check to finish.
// It is safe to call when Start was never called or did nothing.
func (k *PinKeeper) Stop() {
	k.mu.Lock()
	cancel, done := k.cancel, k.done
	k.cancel, k.done = nil, nil
	k.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
}

// tick runs one check: read the node's loaded models, decide, and act.
func (k *PinKeeper) tick(ctx context.Context) {
	loaded, err := k.loadedModels(ctx)
	if err != nil {
		k.logf("node %s: pin check failed: %v", k.opt.Node, err)
		return
	}
	st := classify(loaded, k.opt.Model, time.Now())
	if st.state == stateHealthy {
		return
	}
	if k.busy() {
		k.logf("node %s: busy-skipped re-pin of %s, a session is working on the node", k.opt.Node, k.opt.Model)
		return
	}
	if err := k.pin(ctx); err != nil {
		k.logf("node %s: re-pin of %s failed: %v", k.opt.Node, k.opt.Model, err)
		return
	}
	k.logf("node %s: re-pinned %s (%s)", k.opt.Node, k.opt.Model, st.describe())
	// An eviction is an alert. A finite expiry is only an upgrade to the
	// pin, so it stays quiet: a normal request loads the model with a
	// finite expiry, and that must not alert on every request.
	if st.state == stateAbsent {
		k.alert(st.replaced)
	}
}

// busy reports whether the node is in use, via the Busy hook.
func (k *PinKeeper) busy() bool {
	return k.opt.Busy != nil && k.opt.Busy(k.opt.Node)
}

// alert raises the eviction warning. replaced names the models that took
// the pinned model's place, if any.
func (k *PinKeeper) alert(replaced []string) {
	if k.opt.Alerts == nil {
		return
	}
	body := fmt.Sprintf("Ollama on node %s had no %s loaded, so datawatch loaded it again with keep_alive -1 to keep it pinned.", k.opt.Node, k.opt.Model)
	if len(replaced) > 0 {
		body += fmt.Sprintf(" It had been replaced by %s.", strings.Join(replaced, ", "))
	}
	k.opt.Alerts.AddSystem(alerts.LevelWarn, fmt.Sprintf("Repinned %s on %s — it had been evicted", k.opt.Model, k.opt.Node), body)
}

func (k *PinKeeper) logf(format string, args ...any) {
	if k.opt.Logf != nil {
		k.opt.Logf(format, args...)
		return
	}
	log.Printf(format, args...)
}

func (k *PinKeeper) url(path string) string {
	return strings.TrimRight(k.opt.Address, "/") + path
}

// psResponse is the part of ollama's GET /api/ps reply the keeper reads.
type psResponse struct {
	Models []psModel `json:"models"`
}

type psModel struct {
	Name      string    `json:"name"`
	Model     string    `json:"model"`
	ExpiresAt time.Time `json:"expires_at"`
}

// loadedModels returns the models ollama reports as resident on the node.
func (k *PinKeeper) loadedModels(ctx context.Context) ([]psModel, error) {
	ctx, cancel := context.WithTimeout(ctx, psTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, k.url("/api/ps"), nil)
	if err != nil {
		return nil, fmt.Errorf("ollama /api/ps: build request: %w", err)
	}
	resp, err := k.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama /api/ps: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama /api/ps: HTTP %d", resp.StatusCode)
	}
	var out psResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("ollama /api/ps decode: %w", err)
	}
	return out.Models, nil
}

// pin loads the model with keep_alive -1 through one non-streaming
// /api/generate call. An empty prompt loads the model without generating.
func (k *PinKeeper) pin(ctx context.Context) error {
	body, err := json.Marshal(map[string]any{
		"model":      k.opt.Model,
		"prompt":     "",
		"stream":     false,
		"keep_alive": -1,
	})
	if err != nil {
		return fmt.Errorf("encode pin request: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, pinTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, k.url("/api/generate"), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("ollama /api/generate: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := k.client.Do(req)
	if err != nil {
		return fmt.Errorf("ollama /api/generate: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrBody))
		return fmt.Errorf("ollama /api/generate: HTTP %d: %s", resp.StatusCode, buf)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

type state int

const (
	// stateHealthy: the pinned model is resident with unlimited keep_alive.
	stateHealthy state = iota
	// stateExpiring: the pinned model is resident but will unload when idle.
	stateExpiring
	// stateAbsent: the pinned model is not resident.
	stateAbsent
)

// verdict is one tick's reading of the node.
type verdict struct {
	state state
	// replaced names the other models resident when the pin is absent.
	replaced []string
}

func (v verdict) describe() string {
	switch v.state {
	case stateExpiring:
		return "it was loaded without keep_alive -1"
	case stateAbsent:
		return "it had been evicted"
	default:
		return "healthy"
	}
}

// classify reads the resident models against the pinned model. A model
// with a zero expires_at, or one that expires more than unlimitedAfter
// from now, counts as unlimited.
func classify(models []psModel, pinned string, now time.Time) verdict {
	var others []string
	for _, m := range models {
		if sameModel(m.Name, pinned) || sameModel(m.Model, pinned) {
			if m.ExpiresAt.IsZero() || m.ExpiresAt.After(now.Add(unlimitedAfter)) {
				return verdict{state: stateHealthy}
			}
			return verdict{state: stateExpiring}
		}
		name := m.Name
		if name == "" {
			name = m.Model
		}
		others = append(others, name)
	}
	return verdict{state: stateAbsent, replaced: others}
}

// sameModel compares two ollama model names. A name without a tag means
// the latest tag, so "qwen3:latest" and "qwen3" are the same model.
func sameModel(a, b string) bool {
	return withTag(a) == withTag(b)
}

func withTag(name string) string {
	name = strings.TrimSpace(name)
	if !strings.Contains(name[strings.LastIndex(name, "/")+1:], ":") {
		return name + ":latest"
	}
	return name
}

// LedgerBusy returns a Busy hook that reports a node as busy while any
// capacity lease names it. A lease means a session was admitted to the
// node and may be loading a model. The ledger does not show a load in
// progress, so any lease counts.
func LedgerBusy(led *capacity.Ledger) func(node string) bool {
	return func(node string) bool {
		if led == nil {
			return false
		}
		for _, l := range led.Snapshot().Leases {
			if l.Node == node {
				return true
			}
		}
		return false
	}
}
