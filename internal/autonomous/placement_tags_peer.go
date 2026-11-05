package autonomous

import "strings"

// Peer placement build (placeholder). The peer placement build is not built
// yet, and this file is where its tags must come from when it is: the
// operator addendum (docs/plans/2026-10-20-tag-browsing-tile-design.md,
// Decisions) records that the peer placement build requires its tags to come
// from the placement record. Today every placement the daemon makes is local
// work, so the peer axis is always empty and no peer:<name> tag is written —
// which is correct, not a gap.
//
// Invariant for the build when it lands: the peer:<name> value is taken only
// from the explicit peer name recorded for the placement — the Placement.Peer
// field when the placing code names it, or the PeerSource below when it does.
// It is never inferred from DNS resolution, an IP address, or the machine's
// hostname. Those name the network plumbing, not the named federation peer
// the work runs on; a placement with no explicit peer name is local work and
// gets no peer tag, whatever the network says.

// PeerSource names the federated peer a task is placed on, for the peer axis
// of the placement tags. The peer placement build wires it through
// SetPlacementPeerSource; it returns the explicit registered peer name
// recorded for the work ("fake-federated-peer" in a test, a registry name in
// production) and "" for work that runs locally.
//
// A correct PeerSource only reads the explicit record that names the
// placement's peer. It must never resolve the name by DNS lookup, by an IP
// address, or via os.Hostname — explicit field over inference.
type PeerSource func(taskID string) string

// PlacementPeer resolves the peer name one placement's tags carry for taskID.
//
//  1. the explicit peer already on the placement record wins verbatim —
//     the placement record is the source the addendum names, so an
//     explicit record never loses to the source;
//  2. otherwise the PeerSource, when it is wired and it names a peer —
//     and only an explicit name per its contract, never an inference;
//  3. otherwise "" — local work: no peer tag.
//
// Step 3 is the guard against inference by fallback: nothing below the
// record — no DNS, no IP, no hostname — may supply the value.
func PlacementPeer(p Placement, source PeerSource, taskID string) string {
	if name := strings.TrimSpace(p.Peer); name != "" {
		return name
	}
	if source == nil {
		return ""
	}
	return strings.TrimSpace(source(taskID))
}
