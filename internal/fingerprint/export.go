// The responder half of fingerprint reconciliation
// (docs/plans/2026-10-14-concurrency-and-budget-design.md §3c): the
// exporter this daemon exposes the fingerprint hashes of ITS OWN
// resources to an opted-in peer over
// GET /api/federation/fingerprints. Item.Name is the operator's own
// label for the local resource — the only plain-text field in an Item.
// The identity value it is derived from, the node address, the billing
// label and any credential stay local; only the keyed hash crosses the
// wire.
package fingerprint

import (
	"strings"

	"github.com/dmz006/datawatch/internal/inference"
)

// Item is one exported fingerprint: the kind, the operator's local
// label for the resource, and the keyed hash a peer matches its own
// against. Name is the only plain-text field; the source value is
// never carried.
type Item struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	Hash string `json:"hash"`
}

// Exporter builds the fingerprint list the endpoint answers with.
// Both collaborators are injectable (nil-safe) so tests and a daemon
// that has neither registry yet drive it without live registries:
//   - Nodes resolves each compute node's own identity value.
//   - LLMs lists the daemon's LLM entries for their BillingGroup.
type Exporter struct {
	Nodes *IdentityResolver
	LLMs  func() []inference.LLM
}

// Export returns one Item per resolvable compute node (KindNode,
// Name = the node's local name, Hash = Hash(key, KindNode, identity
// value)) and one Item per LLM with a non-empty BillingGroup
// (KindBilling, Name = the LLM name, Hash = Hash(key, KindBilling,
// trimmed BillingGroup)). Nodes with no identity source are skipped —
// not fingerprinted, not an error. A hash that cannot be derived
// (empty key or value) is skipped too; the handler rejects an empty
// key before calling here.
func (e *Exporter) Export(key []byte) []Item {
	var items []Item
	if e.Nodes != nil {
		for _, v := range e.Nodes.ResolveAll() {
			h, err := Hash(key, KindNode, v.Value)
			if err != nil {
				continue
			}
			items = append(items, Item{Kind: KindNode, Name: v.Name, Hash: h})
		}
	}
	if e.LLMs != nil {
		for _, l := range e.LLMs() {
			bg := strings.TrimSpace(l.BillingGroup)
			if bg == "" {
				continue // "no group" — nothing to reconcile
			}
			h, err := Hash(key, KindBilling, bg)
			if err != nil {
				continue
			}
			items = append(items, Item{Kind: KindBilling, Name: l.Name, Hash: h})
		}
	}
	return items
}
