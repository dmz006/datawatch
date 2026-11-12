package main

import "testing"

func TestVerificationModeLookupNilRegistry(t *testing.T) {
	if verificationModeLookup(nil) != nil {
		t.Fatal("nil registry must give nil lookup")
	}
}
