package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

// TestUIPromptDismiss_Prometheus: the dismissal counters expose one
// counter (datawatch_ui_prompt_dismissals_total) with kind and outcome
// labels, and each of the three outcomes (dismissed, not_cleared,
// refused) is observable per kind.
func TestUIPromptDismiss_Prometheus(t *testing.T) {
	// Register is idempotent (sync.Once in prometheus.go) — safe whether
	// this test runs before or after TestHandler.
	// Register the vec once; other tests (TestWorkerQuestions*) may have
	// already registered it — a duplicate registration is expected and fine
	// (prometheus.Register returns an error instead of panicking).
	if err := prometheus.Register(UIPromptDismissalsTotal); err != nil {
		// already registered — nothing to do.
		_ = err
	}
	UIPromptDismissalsTotal.WithLabelValues("autocompact-threshold", "dismissed").Inc()
	UIPromptDismissalsTotal.WithLabelValues("autocompact-threshold", "not_cleared").Inc()
	UIPromptDismissalsTotal.WithLabelValues("autocompact-threshold", "not_cleared").Inc()
	UIPromptDismissalsTotal.WithLabelValues("autocompact-threshold", "refused").Inc()
	UIPromptDismissalsTotal.WithLabelValues("theme-picker", "dismissed").Inc()

	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	seen := map[string]bool{}
	for _, f := range families {
		if f.GetName() != "datawatch_ui_prompt_dismissals_total" {
			continue
		}
		for _, m := range f.GetMetric() {
			labels := map[string]string{}
			for _, l := range m.GetLabel() {
				labels[l.GetName()] = l.GetValue()
			}
			if labels["kind"] == "autocompact-threshold" && labels["outcome"] != "" &&
				m.GetCounter().GetValue() >= 1 {
				seen[labels["outcome"]] = true
			}
		}
	}
	for _, o := range []string{"dismissed", "not_cleared", "refused"} {
		if !seen[o] {
			t.Fatalf("outcome %q not observed on autocompact-threshold; seen=%v", o, seen)
		}
	}
}
