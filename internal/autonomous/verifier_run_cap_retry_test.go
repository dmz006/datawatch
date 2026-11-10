package autonomous

import (
	"errors"
	"fmt"
	"testing"
)

func TestVerifierRunCapRetryNextBackendInOrder(t *testing.T) {
	order := []string{"ollama-a", "ollama-b", "ollama-c"}
	got, ok := NextVerifierRunCapBackend(order, "ollama-a", false)
	if !ok || got != "ollama-b" {
		t.Fatalf("NextVerifierRunCapBackend(a) = %q, %v; want ollama-b, true", got, ok)
	}
}

func TestVerifierRunCapRetrySpentRetryDoesNotRetryAgain(t *testing.T) {
	order := []string{"ollama-a", "ollama-b", "ollama-c"}
	if got, ok := NextVerifierRunCapBackend(order, "ollama-b", true); ok {
		t.Fatalf("NextVerifierRunCapBackend with retried=true = %q, true; want no retry", got)
	}
}

func TestVerifierRunCapRetrySingleBackendHasNoNext(t *testing.T) {
	if got, ok := NextVerifierRunCapBackend([]string{"ollama-a"}, "ollama-a", false); ok {
		t.Fatalf("single-backend list retried on %q; want no retry", got)
	}
	if got, ok := NextVerifierRunCapBackend(nil, "ollama-a", false); ok {
		t.Fatalf("empty list retried on %q; want no retry", got)
	}
}

func TestVerifierRunCapRetryLastBackendHasNoNext(t *testing.T) {
	order := []string{"ollama-a", "ollama-b"}
	if got, ok := NextVerifierRunCapBackend(order, "ollama-b", false); ok {
		t.Fatalf("last backend retried on %q; want no retry", got)
	}
}

func TestVerifierRunCapRetrySkipsCutDuplicatesAndBlanks(t *testing.T) {
	order := []string{"ollama-a", " ", "ollama-a", "ollama-b"}
	got, ok := NextVerifierRunCapBackend(order, "ollama-a", false)
	if !ok || got != "ollama-b" {
		t.Fatalf("NextVerifierRunCapBackend = %q, %v; want ollama-b, true", got, ok)
	}
}

func TestVerifierRunCapRetryCutNotInListStartsAtTop(t *testing.T) {
	order := []string{"ollama-a", "ollama-b"}
	got, ok := NextVerifierRunCapBackend(order, "elsewhere", false)
	if !ok || got != "ollama-a" {
		t.Fatalf("NextVerifierRunCapBackend = %q, %v; want ollama-a, true", got, ok)
	}
}

func TestVerifierRunCapRetryIsRunCapEnd(t *testing.T) {
	cut := &OneShotNoOutputError{Msg: "run cap", RunCap: true}
	if !IsVerifierRunCapEnd(cut) {
		t.Fatal("IsVerifierRunCapEnd(run-cap error) = false, want true")
	}
	if !IsVerifierRunCapEnd(fmt.Errorf("wrapped: %w", cut)) {
		t.Fatal("IsVerifierRunCapEnd(wrapped run-cap error) = false, want true")
	}
	for name, err := range map[string]error{
		"nil":      nil,
		"timeout":  &OneShotNoOutputError{Msg: "timeout", TimedOut: true},
		"watchdog": &OneShotNoOutputError{Msg: "stall"},
		"plain":    errors.New("boom"),
	} {
		if IsVerifierRunCapEnd(err) {
			t.Errorf("IsVerifierRunCapEnd(%s) = true, want false", name)
		}
	}
}
