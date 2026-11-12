// Package supervisorloop is a daemon-side supervisor loop engine. Mechanical
// steps are plain code; narrow model jobs run behind validators, are re-asked
// when a validator rejects their output, and escalate to the operator with an
// attempt summary instead of acting once they keep failing. The package
// defines small interfaces (ModelClient, Escalator, Clock, Store) and never
// imports internal/autonomous or internal/server.
package supervisorloop
