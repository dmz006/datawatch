package config

import (
	"encoding/json"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestSessionTaskDeliveryConfigRoundTrip(t *testing.T) {
	t.Run("zero values give defaults", func(t *testing.T) {
		s := SessionConfig{}
		if got := s.EffectiveTaskDeliveryWindow(); got != 60*time.Second {
			t.Fatalf("default window = %v, want 60s", got)
		}
		if got := s.EffectiveTaskDeliveryAttempts(); got != 3 {
			t.Fatalf("default attempts = %d, want 3", got)
		}
		if got := s.TaskDeliveryBudgetTotal(); got < 180*time.Second {
			t.Fatalf("default budget total = %v, want >= 180s", got)
		}
		// Defaults: 3*60s + (5s + 10s) backoff = 195s.
		if got := s.TaskDeliveryBudgetTotal(); got != 195*time.Second {
			t.Fatalf("default budget total = %v, want 195s", got)
		}
		if TaskDeliveryBackoff != 5*time.Second {
			t.Fatalf("TaskDeliveryBackoff = %v, want 5s", TaskDeliveryBackoff)
		}
	})

	t.Run("yaml round trip", func(t *testing.T) {
		doc := "task_delivery_window_seconds: 90\ntask_delivery_attempts: 4\n"
		var in SessionConfig
		if err := yaml.Unmarshal([]byte(doc), &in); err != nil {
			t.Fatalf("yaml unmarshal: %v", err)
		}
		if in.TaskDeliveryWindowSeconds != 90 || in.TaskDeliveryAttempts != 4 {
			t.Fatalf("got window=%d attempts=%d, want 90/4", in.TaskDeliveryWindowSeconds, in.TaskDeliveryAttempts)
		}
		var m map[string]any
		out, err := yaml.Marshal(in)
		if err != nil {
			t.Fatalf("yaml marshal: %v", err)
		}
		if err := yaml.Unmarshal(out, &m); err != nil {
			t.Fatalf("yaml re-unmarshal: %v", err)
		}
		if w, ok := m["task_delivery_window_seconds"].(int); !ok || w != 90 {
			t.Fatalf("yaml re-marshal task_delivery_window_seconds = %v (%T), want 90", m["task_delivery_window_seconds"], m["task_delivery_window_seconds"])
		}
		if a, ok := m["task_delivery_attempts"].(int); !ok || a != 4 {
			t.Fatalf("yaml re-marshal task_delivery_attempts = %v (%T), want 4", m["task_delivery_attempts"], m["task_delivery_attempts"])
		}
	})

	t.Run("json round trip", func(t *testing.T) {
		var in SessionConfig
		var m map[string]any
		js := `{"task_delivery_window_seconds": 75, "task_delivery_attempts": 2}`
		if err := json.Unmarshal([]byte(js), &in); err != nil {
			t.Fatalf("json unmarshal: %v", err)
		}
		if in.TaskDeliveryWindowSeconds != 75 || in.TaskDeliveryAttempts != 2 {
			t.Fatalf("got window=%d attempts=%d, want 75/2", in.TaskDeliveryWindowSeconds, in.TaskDeliveryAttempts)
		}
		out, err := json.Marshal(in)
		if err != nil {
			t.Fatalf("json marshal: %v", err)
		}
		if err := json.Unmarshal(out, &m); err != nil {
			t.Fatalf("json re-unmarshal: %v", err)
		}
		if w, ok := m["task_delivery_window_seconds"].(float64); !ok || int(w) != 75 {
			t.Fatalf("json re-marshal task_delivery_window_seconds = %v, want 75", m["task_delivery_window_seconds"])
		}
		if a, ok := m["task_delivery_attempts"].(float64); !ok || int(a) != 2 {
			t.Fatalf("json re-marshal task_delivery_attempts = %v, want 2", m["task_delivery_attempts"])
		}
	})

	t.Run("negative values fall back to defaults", func(t *testing.T) {
		s := SessionConfig{TaskDeliveryWindowSeconds: -60, TaskDeliveryAttempts: -1}
		if got := s.EffectiveTaskDeliveryWindow(); got != 60*time.Second {
			t.Fatalf("negative window = %v, want 60s default", got)
		}
		if got := s.EffectiveTaskDeliveryAttempts(); got != 3 {
			t.Fatalf("negative attempts = %d, want 3 default", got)
		}
		if got := s.TaskDeliveryBudgetTotal(); got != 195*time.Second {
			t.Fatalf("negative-value budget total = %v, want 195s default total", got)
		}
	})

	t.Run("total grows with the keys", func(t *testing.T) {
		base := (&SessionConfig{}).TaskDeliveryBudgetTotal()
		s := SessionConfig{TaskDeliveryWindowSeconds: 90, TaskDeliveryAttempts: 4}
		got := s.TaskDeliveryBudgetTotal()
		if got <= base {
			t.Fatalf("window=90/attempts=4 budget total = %v, want > default %v", got, base)
		}
		// 4*90s + (5s + 10s + 15s) = 390s.
		if got != 390*time.Second {
			t.Fatalf("budget total = %v, want 390s", got)
		}
	})
}
