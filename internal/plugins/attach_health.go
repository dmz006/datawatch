// Attach-only service health probe.
//
// The managed-service supervisor (supervisor.go) only knows the health of
// services it launched itself, so an attach-only service's state is never
// known to datawatch. This probe closes that gap: it GETs the plugin's
// connection.base_url + connection.health_path on every interval and
// records the outcome — last check time, HTTP status, latency, last error
// — and reports the service as up, down or (before the first check)
// unknown. When the health response is JSON carrying a "version" field
// the version is recorded; when it does not, nothing is recorded for it.
//
// Attach-only means datawatch never starts a process: this probe only
// connects to the service the operator is running themselves.
package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/secrets"
)

// AttachHealthStatus is the probe's view of an attach-only service.
type AttachHealthStatus string

const (
	// AttachHealthUnknown means no probe has completed yet.
	AttachHealthUnknown AttachHealthStatus = "unknown"
	// AttachHealthUp means the last probe got a 2xx response.
	AttachHealthUp AttachHealthStatus = "up"
	// AttachHealthDown means the last probe failed (transport error,
	// timeout or a non-2xx response).
	AttachHealthDown AttachHealthStatus = "down"
)

// DefaultAttachHealthInterval is the probe cadence used when the caller
// leaves AttachHealthProbe.Interval unset. It is a default, not a fixed
// loop constant: the caller (the settings wiring) may set any interval.
const DefaultAttachHealthInterval = 5 * time.Second

// AttachHealthProbe periodically GETs an attach-only service's health
// endpoint and records the outcome. Construct with
// NewAttachHealthProbe, set Interval (zero = DefaultAttachHealthInterval)
// and HTTPClient (nil = 3s timeout, same as the supervisor's probe), then
// Run from a goroutine.
type AttachHealthProbe struct {
	// Interval between probes. Zero means DefaultAttachHealthInterval.
	// Read once by Run; not safe to change while running.
	Interval time.Duration
	// HTTPClient used for each probe; nil uses the default.
	HTTPClient *http.Client

	url        string
	authHeader string
	client     *http.Client

	// OnResult — invoked once per completed check, after the probe's
	// lock is released, with the check's outcome (healthy) and HTTP
	// status code (0 when the request never completed). Nil-safe; set
	// once before Run. Used to feed the service event recorder.
	OnResult func(healthy bool, statusCode int)

	mu       sync.Mutex
	status   AttachHealthStatus
	lastAt   time.Time
	lastCode int
	lastDur  time.Duration
	lastErr  string
	version  string
	upSince  time.Time // when the probe first reported up in the current up-streak
}

// NewAttachHealthProbe builds a probe for a kind: service plugin's
// connection block. The connection must declare a non-empty health_path;
// without one there is nothing to probe. store resolves
// ${secret:...} auth tokens and may be nil. No process is started.
func NewAttachHealthProbe(p *Plugin, store secrets.Store) (*AttachHealthProbe, error) {
	if p == nil {
		return nil, errors.New("plugin is nil")
	}
	if p.Kind != KindService {
		return nil, fmt.Errorf("plugin %q is kind %q, not %q", p.Name, p.Kind, KindService)
	}
	c := p.Connection
	if c == nil || c.BaseURL == "" {
		return nil, fmt.Errorf("plugin %q has no connection.base_url", p.Name)
	}
	if c.HealthPath == "" {
		return nil, fmt.Errorf("plugin %q has no connection.health_path to probe", p.Name)
	}
	h, err := resolveConnectionAuthHeader(p, store)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: defaultServiceProbeTimeout}
	return &AttachHealthProbe{
		url:        connectionHealthURL(c),
		authHeader: h,
		client:     client,
		status:     AttachHealthUnknown,
	}, nil
}

// Run probes every interval until ctx is cancelled. It performs the first
// probe immediately, then returns promptly once ctx is done.
func (a *AttachHealthProbe) Run(ctx context.Context) {
	if err := ctx.Err(); err != nil {
		return
	}
	interval := a.Interval
	if interval <= 0 {
		interval = DefaultAttachHealthInterval
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	a.check(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.check(ctx)
		}
	}
}

// Status returns the last observed status (up, down or unknown).
func (a *AttachHealthProbe) Status() AttachHealthStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.status
}

// Version returns the last recorded service version, or "" if no health
// response so far has carried a version field.
func (a *AttachHealthProbe) Version() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.version
}

// LastCheck returns the last probe's outcome: time, HTTP status (0 for a
// transport failure), latency and error ("" on success), plus the
// recorded version. LastCheck is zero when no probe has completed.
// UpSince is set while the service is reported up: the moment the probe
// first saw it up in the current up-streak (zero otherwise), so the
// elapsed time since the service last came up is computable.
func (a *AttachHealthProbe) LastCheck() AttachHealthReport {
	a.mu.Lock()
	defer a.mu.Unlock()
	return AttachHealthReport{
		Status:     a.status,
		LastCheck:  a.lastAt,
		StatusCode: a.lastCode,
		Latency:    a.lastDur,
		LastError:  a.lastErr,
		Version:    a.version,
		UpSince:    a.upSince,
	}
}

// AttachHealthReport is a point-in-time snapshot of the probe's findings.
type AttachHealthReport struct {
	Status     AttachHealthStatus `json:"status"`
	LastCheck  time.Time          `json:"last_check,omitempty"`
	StatusCode int                `json:"status_code,omitempty"`
	Latency    time.Duration      `json:"latency,omitempty"`
	LastError  string             `json:"last_error,omitempty"`
	Version    string             `json:"version,omitempty"`
	UpSince    time.Time          `json:"up_since,omitempty"`
}

// check performs one probe and records its outcome.
func (a *AttachHealthProbe) check(ctx context.Context) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.url, nil)
	if err != nil {
		return
	}
	if a.authHeader != "" {
		req.Header.Set("Authorization", a.authHeader)
	}
	start := time.Now()
	resp, err := a.client.Do(req)
	latency := time.Since(start)
	if err != nil {
		if ctx.Err() != nil {
			return // shutting down; a cancelled request is not a finding
		}
		a.record(AttachHealthDown, start, 0, latency, err.Error(), "")
		return
	}
	defer resp.Body.Close() //nolint:errcheck

	// The response may or may not be JSON carrying a "version" field; read
	// a bounded amount and record the version only when it is present.
	var version string
	var body map[string]json.RawMessage
	if dec := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)); dec.Decode(&body) == nil {
		if raw, ok := body["version"]; ok {
			var v string
			if json.Unmarshal(raw, &v) == nil {
				version = v
			}
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		a.record(AttachHealthDown, start, resp.StatusCode, latency,
			"health endpoint returned "+resp.Status, version)
		return
	}
	a.record(AttachHealthUp, start, resp.StatusCode, latency, "", version)
}

// record stores one probe outcome under the mutex. version is recorded
// only when non-empty (i.e. the response carried a version field);
// otherwise the previously recorded version, if any, is kept.
func (a *AttachHealthProbe) record(status AttachHealthStatus, at time.Time, code int, dur time.Duration, errStr, version string) {
	a.mu.Lock()
	wasUp := a.status == AttachHealthUp
	a.status = status
	a.lastAt = at
	a.lastCode = code
	a.lastDur = dur
	a.lastErr = errStr
	if status == AttachHealthUp && !wasUp {
		a.upSince = at // the service just came up
	}
	if status != AttachHealthUp {
		a.upSince = time.Time{} // down again: no uptime to report
	}
	if version != "" {
		a.version = version
	}
	a.mu.Unlock()
	if fn := a.OnResult; fn != nil {
		fn(status == AttachHealthUp, code)
	}
}
