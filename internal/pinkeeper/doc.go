// Package pinkeeper keeps a compute node's pinned ollama model resident.
//
// A compute node may pin one model (node_pinned_model). Ollama unloads
// idle models, and a request for another model can evict the pinned one.
// A PinKeeper watches one pinned node on a fixed interval
// (pin_check_seconds). When the pinned model is absent, or loaded with a
// finite expiry, it loads the model again with keep_alive -1. It skips
// the re-pin while a session is working on the node, and it raises a
// system alert when an eviction forced the re-pin.
//
// The keeper does not route requests and does not decide which model a
// request runs. internal/inference enforces the pin on requests. This
// package only restores the pin after Ollama has dropped it.
package pinkeeper
