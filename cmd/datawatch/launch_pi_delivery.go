// The launch wiring of the datawatch pi extension (delivery job 4). Owner:
// part C. STUB: nothing calls it yet and it does nothing.
//
// Part A calls it from the pre-launch hook for a pi-family session, with a
// token minted for that session whether or not Claude's channel is on (today
// the mint and the credential file both depend on it, so with the channel off
// pi gets neither and nothing can confirm).

// When a busy pi is handed a request (after its run, or mid-task), and what
// the extension tells the model about such messages, are not decided: see the
// design questions in build/d4-split-final.md.

package main

import "github.com/dmz006/datawatch/internal/session"

// piDeliveryLaunch is what wiring one pi session needs.
type piDeliveryLaunch struct {
	DataDir    string // the daemon's expanded data directory
	APIURL     string // loopback base URL
	CACertPath string // empty without TLS
	Token      string // this session's own token
	// SetSessionEnv sets a variable in the session's tmux environment. The
	// variable carries a file path, never a token.
	SetSessionEnv func(tmuxSession, key, value string) error
}

// wirePiDelivery writes the session's delivery credential file and names it in
// the session's environment.
func wirePiDelivery(*session.Session, piDeliveryLaunch) error { return nil }

var _ = wirePiDelivery
