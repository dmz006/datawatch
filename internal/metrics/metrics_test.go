package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestHandler(t *testing.T) {
	h := Handler()
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
	_ = h
}

func TestWorkerQuestionsTotalIsRegisteredWithOutcomeLabel(t *testing.T) {
	Register()
	for _, o := range []string{"raised", "duplicate", "refused", "parked", "resumed"} {
		WorkerQuestionsTotal.WithLabelValues(o).Inc()
	}
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	seen := map[string]bool{}
	for _, f := range families {
		if f.GetName() != "datawatch_worker_questions_total" {
			continue
		}
		for _, m := range f.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == "outcome" && m.GetCounter().GetValue() >= 1 {
					seen[l.GetValue()] = true
				}
			}
		}
	}
	if len(seen) != 5 {
		t.Fatalf("outcomes seen = %v, want all five of raised, duplicate, refused, parked, resumed", seen)
	}
}
