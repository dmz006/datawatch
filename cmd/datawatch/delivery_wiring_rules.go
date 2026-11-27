// Rule changes as requests (delivery job 6). Job 4 leaves these two as no-ops
// and part A calls them, so that job 6 needs no job 4 file; its code in
// internal/delivery goes in rules_bundle*.go (rules.go is the delivery check's
// status rules). Owner: job 6.

package main

import (
	"context"

	"github.com/dmz006/datawatch/internal/delivery"
)

// requestRulesVersion names the rules version of a scope. Nil: none is named.
func requestRulesVersion(deliveryDeps) func(delivery.Scope) string { return nil }

// startRequestRules watches the rules, once the store is open.
func startRequestRules(context.Context, deliveryDeps, delivery.RequestDaemon) {}

var (
	_ = requestRulesVersion
	_ = startRequestRules
)
