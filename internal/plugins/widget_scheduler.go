// BL403 — pull-mode widget scheduler.
//
// A WidgetScheduler keeps one goroutine per (enabled plugin, pull-mode
// widget). Each goroutine owns its own ticker at the widget's interval and
// fetches the value — via the status_poll hook through Registry.Invoke for
// kind: automation plugins, or GET <connection.base_url><pull_path> for
// kind: service plugins — and writes it to the WidgetStore. Widgets are
// fully isolated: a slow or hung pull only ever delays its own widget, and
// a tick that arrives while that widget's previous pull is still running
// is skipped rather than queued.

package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dmz006/datawatch/internal/secrets"
)

// Widget scheduler defaults.
const (
	defaultWidgetHTTPTimeout       = 10 * time.Second
	defaultWidgetReconcileInterval = 30 * time.Second
)

// WidgetScheduler pulls pull-mode widget values on their intervals.
//
// The exported tunables must be set before Run.
type WidgetScheduler struct {
	// HTTPClient fetches service-plugin widgets. Defaults to a client with
	// a 10s timeout.
	HTTPClient *http.Client
	// ReconcileInterval is how often Run re-reads the registry so plugin
	// hot-reloads (Discover/Watch) and enable/disable toggles are picked up
	// without an explicit Reconcile call. Defaults to 30s.
	ReconcileInterval time.Duration

	// intervalFor, when set, overrides Widget.PullInterval. Test hook: it
	// allows sub-second intervals that manifests cannot express.
	intervalFor func(plugin string, w Widget) time.Duration

	reg     *Registry
	store   *WidgetStore
	secrets secrets.Store // nil = use the registry's secrets store

	mu      sync.Mutex
	ctx     context.Context // Run's context; nil until Run starts
	workers map[string]*widgetWorker
	wg      sync.WaitGroup
}

// widgetSpec is everything that defines one widget's pull. A worker is
// restarted whenever its spec changes.
type widgetSpec struct {
	plugin   Plugin // snapshot taken under Registry.mu
	widget   Widget
	interval time.Duration
}

// sameAs reports whether two specs describe the same pull, so the running
// worker can be kept.
func (a widgetSpec) sameAs(b widgetSpec) bool {
	if a.widget != b.widget || a.interval != b.interval || a.plugin.Kind != b.plugin.Kind ||
		a.plugin.Entry != b.plugin.Entry {
		return false
	}
	ca, cb := a.plugin.Connection, b.plugin.Connection
	if (ca == nil) != (cb == nil) {
		return false
	}
	if ca == nil {
		return true
	}
	ta, tb := "", ""
	if ca.Auth != nil {
		ta = ca.Auth.Token
	}
	if cb.Auth != nil {
		tb = cb.Auth.Token
	}
	return ca.BaseURL == cb.BaseURL && ta == tb
}

type widgetWorker struct {
	spec   widgetSpec
	cancel context.CancelFunc
}

// NewWidgetScheduler builds a scheduler that pulls widgets declared by reg's
// plugins into store. secretStore resolves service connection auth tokens;
// when nil the registry's secrets store (SetSecretsStore) is used.
func NewWidgetScheduler(reg *Registry, store *WidgetStore, secretStore secrets.Store) *WidgetScheduler {
	return &WidgetScheduler{
		HTTPClient:        &http.Client{Timeout: defaultWidgetHTTPTimeout},
		ReconcileInterval: defaultWidgetReconcileInterval,
		reg:               reg,
		store:             store,
		secrets:           secretStore,
		workers:           map[string]*widgetWorker{},
	}
}

// Run starts the widget goroutines and keeps them in sync with the
// registry until ctx is done, then waits for every widget goroutine (and
// any in-flight pull, which is cancelled) to exit.
func (s *WidgetScheduler) Run(ctx context.Context) {
	s.mu.Lock()
	s.ctx = ctx
	s.mu.Unlock()

	interval := s.ReconcileInterval
	if interval <= 0 {
		interval = defaultWidgetReconcileInterval
	}
	t := time.NewTicker(interval)
	s.Reconcile()
	func() {
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.Reconcile()
			}
		}
	}()

	s.mu.Lock()
	for k, w := range s.workers {
		w.cancel()
		delete(s.workers, k)
	}
	s.ctx = nil
	s.mu.Unlock()
	s.wg.Wait()
}

// Reconcile makes the running widget goroutines match the registry: one
// per enabled plugin's pull-mode widget. Goroutines for removed or
// disabled plugins, removed widgets and changed widget definitions are
// stopped; new ones are started with an immediate first pull. It is a
// no-op until Run has started.
func (s *WidgetScheduler) Reconcile() {
	desired := s.desiredSpecs()

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx == nil || s.ctx.Err() != nil {
		return
	}
	for k, w := range s.workers {
		if spec, ok := desired[k]; !ok || !spec.sameAs(w.spec) {
			w.cancel()
			delete(s.workers, k)
		}
	}
	for k, spec := range desired {
		if _, ok := s.workers[k]; ok {
			continue
		}
		wctx, cancel := context.WithCancel(s.ctx)
		s.workers[k] = &widgetWorker{spec: spec, cancel: cancel}
		s.wg.Add(1)
		go s.runWorker(wctx, spec)
	}
}

// desiredSpecs snapshots the enabled plugins' pull-mode widgets. Plugin
// fields are copied under Registry.mu so later pulls never race with
// SetEnabled, recordInvoke or a rediscover.
func (s *WidgetScheduler) desiredSpecs() map[string]widgetSpec {
	r := s.reg
	r.mu.Lock()
	plugins := make([]Plugin, 0, len(r.byName))
	for _, p := range r.byName {
		if p.Enabled {
			plugins = append(plugins, *p)
		}
	}
	r.mu.Unlock()

	out := map[string]widgetSpec{}
	for _, p := range plugins {
		for _, w := range p.Widgets {
			if w.DataMode != WidgetDataPull {
				continue
			}
			interval := w.PullInterval()
			if s.intervalFor != nil {
				if d := s.intervalFor(p.Name, w); d > 0 {
					interval = d
				}
			}
			out[widgetKey(p.Name, w.ID)] = widgetSpec{plugin: p, widget: w, interval: interval}
		}
	}
	return out
}

// runWorker drives one widget: an immediate pull, then one pull per tick.
// Each pull runs in its own goroutine so the ticker loop never blocks; a
// tick while the previous pull is still running is dropped.
func (s *WidgetScheduler) runWorker(ctx context.Context, spec widgetSpec) {
	defer s.wg.Done()
	var busy atomic.Bool
	var pulls sync.WaitGroup
	defer pulls.Wait()

	fire := func() {
		if !busy.CompareAndSwap(false, true) {
			return // previous pull of this widget still running: skip
		}
		pulls.Add(1)
		go func() {
			defer pulls.Done()
			defer busy.Store(false)
			s.pullOnce(ctx, spec)
		}()
	}

	fire()
	t := time.NewTicker(spec.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			fire()
		}
	}
}

// pullOnce fetches and stores one widget value. A failure records the
// error and keeps the previous value. Results of a pull whose worker was
// stopped meanwhile are discarded.
func (s *WidgetScheduler) pullOnce(ctx context.Context, spec widgetSpec) {
	pctx, cancel := context.WithTimeout(ctx, spec.interval)
	defer cancel()

	var data json.RawMessage
	var err error
	if spec.plugin.Kind == KindService {
		data, err = s.pullService(pctx, spec)
	} else {
		data, err = s.pullAutomation(pctx, spec)
	}
	if ctx.Err() != nil {
		return
	}
	name, id := spec.plugin.Name, spec.widget.ID
	if err == nil {
		err = s.store.Set(WidgetValue{Plugin: name, WidgetID: id, Data: data, Source: WidgetSourcePull})
	}
	if err != nil {
		s.store.SetError(name, id, err.Error())
	}
}

// pullAutomation runs the status_poll hook (see HookStatusPoll).
func (s *WidgetScheduler) pullAutomation(ctx context.Context, spec widgetSpec) (json.RawMessage, error) {
	w := spec.widget
	resp, err := s.reg.Invoke(ctx, spec.plugin.Name, HookStatusPoll, Request{
		Hook: HookStatusPoll,
		Payload: map[string]any{
			"widget_id": w.ID,
			"pull_path": w.PullPath,
			"surface":   w.Surface,
			"kind":      w.Kind,
		},
	})
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("status_poll: %s", resp.Error)
	}
	if resp.Fields == nil {
		return nil, errors.New("status_poll: response has no fields")
	}
	var v any = resp.Fields
	if d, ok := resp.Fields["data"]; ok {
		v = d
	}
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("status_poll: marshal data: %w", err)
	}
	return data, nil
}

// pullService GETs <connection.base_url><pull_path>.
func (s *WidgetScheduler) pullService(ctx context.Context, spec widgetSpec) (json.RawMessage, error) {
	p := &spec.plugin
	if p.Connection == nil || p.Connection.BaseURL == "" {
		return nil, errors.New("service plugin has no connection.base_url")
	}
	store := s.secrets
	if store == nil {
		s.reg.mu.Lock()
		store = s.reg.secretsStore
		s.reg.mu.Unlock()
	}
	auth, err := resolveConnectionAuthHeader(p, store)
	if err != nil {
		return nil, err
	}
	url := strings.TrimRight(p.Connection.BaseURL, "/") + spec.widget.PullPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxWidgetPayloadBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read widget response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("widget endpoint returned %s", resp.Status)
	}
	if len(body) > MaxWidgetPayloadBytes {
		return nil, fmt.Errorf("widget response is over the %d byte limit", MaxWidgetPayloadBytes)
	}
	if !json.Valid(body) {
		return nil, errors.New("widget response is not valid JSON")
	}
	return body, nil
}
