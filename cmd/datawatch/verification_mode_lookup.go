package main

import (
	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/inference"
)

// verificationModeLookup adapts the LLM registry to the lookup the verifier
// resolution uses to read an entry's verification_mode. A nil registry, an
// unknown entry or an unset mode yields "" (resolves to generate).
func verificationModeLookup(reg *inference.Registry) autonomous.VerificationModeLookup {
	if reg == nil {
		return nil
	}
	return func(backend, _ string) inference.VerificationMode {
		if backend == "" {
			return ""
		}
		l, err := reg.Get(backend)
		if err != nil || l == nil {
			return ""
		}
		return l.VerificationMode
	}
}
