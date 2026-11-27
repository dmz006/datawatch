// Request sources (delivery job 5: wakes, handover, replies, answers, bugs,
// schedules to a role, exit hooks). Job 4 leaves these two as no-ops and part A
// calls them, so that job 5 needs no job 4 file. Owner: job 5.

package main

import (
	"context"

	"github.com/dmz006/datawatch/internal/delivery"
)

// requestHolderStarter starts a project's defined supervisor for a waiting
// request. Nil: nothing is started.
func requestHolderStarter(deliveryDeps) delivery.HolderStarter { return nil }

// startRequestSources starts what creates requests, once the store is open.
func startRequestSources(context.Context, deliveryDeps, delivery.RequestDaemon) {}

var (
	_ = requestHolderStarter
	_ = startRequestSources
)
