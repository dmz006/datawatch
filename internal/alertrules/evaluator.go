package alertrules

import (
	"context"
	"fmt"
	"time"

	"github.com/dmz006/datawatch/internal/observer"
)

// Evaluator periodically checks each enabled rule against the live
// observer envelopes and fires the configured action when the
// condition is met and the cooldown has elapsed.
type Evaluator struct {
	// Store is the rule + firing persistence layer.
	Store *Store

	// GetEnvelopes returns the current observer envelope snapshot.
	// It may return nil when the observer is not yet started.
	GetEnvelopes func() []observer.Envelope

	// FireAlert emits a system alert through the daemon's alert store.
	// name is the rule name; msg is the formatted message.
	FireAlert func(name, msg string)

	// ScaleNode adjusts the number of instances on a compute node by
	// delta (positive = scale up, negative = scale down). This is a
	// stub in v8.1 — the K8s implementation ships later.
	ScaleNode func(nodeName string, delta int) error

	// evalInterval controls how often the evaluation loop runs.
	// Zero defaults to 30 s.
	evalInterval time.Duration

	// lastFired tracks the last firing time per rule name.
	lastFired map[string]time.Time
}

// NewEvaluator constructs an Evaluator with sensible defaults.
func NewEvaluator(store *Store, getEnvelopes func() []observer.Envelope, fireAlert func(name, msg string), scaleNode func(nodeName string, delta int) error) *Evaluator {
	return &Evaluator{
		Store:        store,
		GetEnvelopes: getEnvelopes,
		FireAlert:    fireAlert,
		ScaleNode:    scaleNode,
		lastFired:    map[string]time.Time{},
	}
}

// Start begins the evaluation loop. It blocks until ctx is cancelled.
func (e *Evaluator) Start(ctx context.Context) {
	interval := e.evalInterval
	if interval == 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.evaluate()
		}
	}
}

// evaluate runs one pass across all enabled rules.
func (e *Evaluator) evaluate() {
	if e.GetEnvelopes == nil {
		return
	}
	envelopes := e.GetEnvelopes()
	rules := e.Store.List()
	now := time.Now()

	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		cooldown := time.Duration(rule.CooldownSeconds) * time.Second
		if cooldown == 0 {
			cooldown = 300 * time.Second
		}
		if last, ok := e.lastFired[rule.Name]; ok {
			if now.Sub(last) < cooldown {
				continue
			}
		}

		// Gather envelopes matching the source filter.
		var candidates []observer.Envelope
		for _, env := range envelopes {
			if rule.SourceFilter == "" || env.Source == rule.SourceFilter {
				candidates = append(candidates, env)
			}
		}

		// Find the max value for the configured metric across candidates.
		for _, env := range candidates {
			val, ok := metricValue(env, rule.Condition.Metric)
			if !ok {
				continue
			}
			if !conditionMet(rule.Condition.Operator, val, rule.Condition.Threshold) {
				continue
			}
			// Condition met — fire.
			e.lastFired[rule.Name] = now
			result := e.fire(rule, env, val, now)
			e.Store.RecordFiring(Firing{
				RuleName:     rule.Name,
				EnvID:        env.ID,
				Source:       env.Source,
				Value:        val,
				FiredAt:      now,
				Action:       rule.Action.Kind,
				ActionResult: result,
			})
			break // fire once per rule per cycle
		}
	}
}

// fire executes the rule's Action and returns a short result string.
func (e *Evaluator) fire(rule AlertRule, env observer.Envelope, val float64, now time.Time) string {
	msg := fmt.Sprintf("alert-rule %q: %s=%.2f %s %.2f (envelope=%s)",
		rule.Name, rule.Condition.Metric, val, rule.Condition.Operator, rule.Condition.Threshold, env.ID)

	switch rule.Action.Kind {
	case "alert":
		if e.FireAlert != nil {
			e.FireAlert(rule.Name, msg)
		}
		return "alert emitted"

	case "scale_up":
		amount := rule.Action.ScaleAmount
		if amount == 0 {
			amount = 1
		}
		if e.ScaleNode != nil {
			if err := e.ScaleNode(rule.Action.ScaleTarget, amount); err != nil {
				return fmt.Sprintf("scale_up error: %v", err)
			}
		}
		if e.FireAlert != nil {
			e.FireAlert(rule.Name, fmt.Sprintf("[scale_up] %s — added %d instance(s) on %q",
				msg, amount, rule.Action.ScaleTarget))
		}
		return fmt.Sprintf("scaled up %d on %s", amount, rule.Action.ScaleTarget)

	case "scale_down":
		amount := rule.Action.ScaleAmount
		if amount == 0 {
			amount = 1
		}
		if e.ScaleNode != nil {
			if err := e.ScaleNode(rule.Action.ScaleTarget, -amount); err != nil {
				return fmt.Sprintf("scale_down error: %v", err)
			}
		}
		if e.FireAlert != nil {
			e.FireAlert(rule.Name, fmt.Sprintf("[scale_down] %s — removed %d instance(s) on %q",
				msg, amount, rule.Action.ScaleTarget))
		}
		return fmt.Sprintf("scaled down %d on %s", amount, rule.Action.ScaleTarget)

	default:
		return fmt.Sprintf("unknown action kind: %s", rule.Action.Kind)
	}
}

// metricValue extracts the requested metric from an Envelope.
// ok is false when the metric name is not recognized.
func metricValue(env observer.Envelope, metric string) (float64, bool) {
	switch metric {
	case "cpu_pct":
		return env.CPUPct, true
	case "mem_pct":
		// No direct mem_pct on Envelope; express as fraction of RSS.
		// Return CPUPct as a best-effort — operators should use rss_bytes
		// for absolute memory checks. Return 0 with ok=false to be safe.
		return 0, false
	case "gpu_pct":
		return env.GPUPct, true
	case "rss_bytes":
		return float64(env.RSSBytes), true
	case "net_rx_bps":
		return float64(env.NetRxBps), true
	case "net_tx_bps":
		return float64(env.NetTxBps), true
	default:
		return 0, false
	}
}

// conditionMet evaluates (actual op threshold).
func conditionMet(op string, actual, threshold float64) bool {
	switch op {
	case ">":
		return actual > threshold
	case "<":
		return actual < threshold
	case ">=":
		return actual >= threshold
	case "<=":
		return actual <= threshold
	default:
		return false
	}
}
