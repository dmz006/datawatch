// plugins.Config.HealthInterval (plugins.services.health_interval_seconds)
// — the daemon's configured probe cadence reaches the attach-only
// health probes; a zero value keeps the package default.
package plugins

import (
	"context"
	"testing"
	"time"
)

func attachProbeInterval(t *testing.T, r *Registry, name string) time.Duration {
	t.Helper()
	ap, ok := r.attachProbes[name]
	if !ok {
		t.Fatalf("no attach probe registered for %q", name)
	}
	return ap.probe.Interval
}

func TestRegistry_AttachProbeIntervalFromConfig(t *testing.T) {
	p := statusPlugin("svc", LifecycleAttachOnly)
	p.Connection.BaseURL = "http://127.0.0.1:1" // a down service is fine: the cadence is what is under test

	r := &Registry{
		byName:  map[string]*Plugin{"svc": p},
		managed: map[string]bool{},
		cfg:     Config{HealthInterval: 45 * time.Second},
	}
	r.StartAttachProbes(context.Background())
	t.Cleanup(r.StopAttachProbes)

	if got := attachProbeInterval(t, r, "svc"); got != 45*time.Second {
		t.Fatalf("configured cadence not applied: probe interval = %v, want %v", got, 45*time.Second)
	}
}

func TestRegistry_AttachProbeIntervalZeroKeepsDefault(t *testing.T) {
	p := statusPlugin("svc", LifecycleAttachOnly)
	p.Connection.BaseURL = "http://127.0.0.1:1"

	r := &Registry{
		byName:  map[string]*Plugin{"svc": p},
		managed: map[string]bool{},
		// cfg zero: the registry keeps the attach_health package default.
	}
	r.StartAttachProbes(context.Background())
	t.Cleanup(r.StopAttachProbes)

	// Zero config: the probe keeps Interval unset, and attach_health's
	// package default applies at run time (Interval == 0 is the sentinel
	// documented on AttachHealthProbe.Interval).
	if got := attachProbeInterval(t, r, "svc"); got != 0 {
		t.Fatalf("zero config must leave the probe at the package default (unset), got %v", got)
	}
}
