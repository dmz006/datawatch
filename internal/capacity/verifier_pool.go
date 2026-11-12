package capacity

// VerifierPool is the per-node pool verifier work is admitted against: a
// Request with Role RoleVerifier on node N takes node-verify:N in place of
// the executor pool node:N, so verifiers can run while every executor slot
// on the node is taken.
//
// LIMIT MEANING: a node-verify pool with a limit of 0 (unset) means the node
// has NO verifier pool. That inverts the usual pool rule of "0 means
// unlimited": here 0 means verifiers on that node fall back to the ordinary
// node:N behaviour, where the node pool's own 0 limit still means unlimited.
// Only a limit above 0 switches a verifier's lease onto the verifier pool; a
// SetLimit of 0 or below removes the verifier pool (and re-dispatches).
func VerifierPool(node string) string {
	return "node-verify:" + node
}

// VerifierSlotsOn reports the limit of the node's verifier pool: the number
// of verifier sessions the ledger admits on it. 0 means the node has no
// verifier pool — the inverter of the usual rule, where a pool's limit of 0
// means unlimited (see VerifierPool). A verifier on a node that reports 0 is
// admitted against node:<node> exactly as before.
//
// It is a read-only view under the ledger lock: it takes no lease, re-runs
// no dispatch, and calls no other ledger method. Because it is just a fast
// in-memory read, it is itself safe to call from an Options hook (see the
// LOCK RULE on Options).
func (l *Ledger) VerifierSlotsOn(node string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.limits[VerifierPool(node)]
}
