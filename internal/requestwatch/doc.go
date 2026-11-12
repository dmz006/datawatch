// Package requestwatch observes the in-flight model request of a worker or
// verifier session whose model runs on a registered compute node, so a hung
// request can be told from a slow one.
//
// The package is observation only. It makes no model call and performs no
// remediation: it never ends a worker, never retries on another backend and
// never unloads a model. A Source reads one signal from the node (for an
// Ollama node, its running-models listing and optionally the GPU utilisation
// of an attached observer) and a Tracker turns the stream of samples into an
// Observation: generating, idle, unreachable or unknown, with the time since
// the last sign of progress.
//
// Every threshold is passed in as Params on each call, so a configuration
// change applies at the next poll. Nothing in this package names a model, a
// node or a backend family.
package requestwatch
