package main

import (
	"context"
	"errors"
	"testing"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

func TestRunClassifyVerificationFailsClosedWithoutRuntime(t *testing.T) {
	old := classifyRuntimeProvider
	defer func() { classifyRuntimeProvider = old }()

	classifyRuntimeProvider = nil
	v := runClassifyVerification(context.Background(), "n", "b", "m", "p", true)
	if v.OK || v.Category != "setup" || v.VerifierMode != autonomouspkg.VerifierModeClassify {
		t.Fatalf("nil provider: %+v", v)
	}
	classifyRuntimeProvider = func(string, string, string) (autonomouspkg.ClassifyRuntime, error) {
		return autonomouspkg.ClassifyRuntime{}, errors.New("no node")
	}
	v = runClassifyVerification(context.Background(), "n", "b", "m", "p", true)
	if v.OK || v.Category != "infra" {
		t.Fatalf("provider error: %+v", v)
	}
}
