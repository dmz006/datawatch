// Completed display, shipped-state counting and filtering primitives.
//
// Shipped state is orthogonal to a record's lifecycle status: a PRD or
// Automation that carries shipped state (a non-empty merge commit) keeps the
// status it had when the work landed — a cancelled PRD whose work landed
// stays cancelled *and* carries shipped state. The shipped-state surfaces
// (counts, filters, the view's effective status) all derive from those two
// facts plus the autonomous.shipped settings:
//
//   - effective display status: "completed" when the record carries shipped
//     state and the shipped surface is enabled (autonomous.shipped.enabled),
//     otherwise the stored status;
//   - counts: one bucket per displayed status. When the shipped surface is
//     enabled, records carrying shipped state group under "completed"
//     together with stored-completed records — a cancelled-but-shipped
//     record lands in "completed", never "cancelled" and never a second
//     "shipped" bucket; a record cancelled without shipped state lands in
//     "cancelled";
//   - shipped selection (list filters): "shipped" keeps only records that
//     carry shipped state, "unshipped" only those without, "all" applies no
//     shipped selection.

package autonomous

// CompletedDisplayStatus is the displayed/counted status of a record that
// carries a shipped record (merged code) while the shipped surface is
// enabled: the work finished, so it shows as Completed — also when its
// stored status is still cancelled and has not been migrated yet. The
// release, merge time and merge commit stay available as detail fields.
const CompletedDisplayStatus = "completed"

// PRDEffectiveDisplayStatus reports the status a PRD displays as: shipped
// when it carries shipped state and the shipped surface is enabled,
// otherwise its stored status. A nil PRD reports the empty status.
func PRDEffectiveDisplayStatus(p *PRD, shippedEnabled bool) string {
	if p == nil {
		return ""
	}
	if shippedEnabled && p.IsShipped() {
		return CompletedDisplayStatus
	}
	return string(p.Status)
}

// AutomationEffectiveDisplayStatus reports the status an Automation displays
// as: shipped when it carries shipped state and the shipped surface is
// enabled, otherwise its stored status. A nil Automation reports the empty
// status.
func AutomationEffectiveDisplayStatus(a *Automation, shippedEnabled bool) string {
	if a == nil {
		return ""
	}
	if shippedEnabled && a.IsShipped() {
		return CompletedDisplayStatus
	}
	return string(a.Status)
}

// prdCountStatus is the status bucket a PRD counts under: the completed
// bucket when the shipped surface is enabled and the PRD carries a shipped
// record (the count_separately setting no longer splits a second bucket),
// otherwise the stored status.
func prdCountStatus(p *PRD, shippedEnabled, _ bool) string {
	if p == nil {
		return ""
	}
	if shippedEnabled && p.IsShipped() {
		return CompletedDisplayStatus
	}
	return string(p.Status)
}

// automationCountStatus is the status bucket an Automation counts under
// (same rule as prdCountStatus for Automation records).
func automationCountStatus(a *Automation, shippedEnabled, _ bool) string {
	if a == nil {
		return ""
	}
	if shippedEnabled && a.IsShipped() {
		return CompletedDisplayStatus
	}
	return string(a.Status)
}

// CountPRDStates returns one count per displayed PRD status across prds.
// With shippedEnabled set, every PRD carrying a shipped record counts under
// "completed" together with stored-completed PRDs — including PRDs whose
// stored status is still cancelled, which never count as cancelled — while
// an unshipped PRD counts under its stored status. shippedEnabled off folds
// every PRD back into its stored-status bucket. countSeparately is kept for
// signature stability; it no longer creates a second bucket. nil entries are
// skipped (they never count).
func CountPRDStates(prds []*PRD, shippedEnabled, countSeparately bool) map[string]int {
	out := make(map[string]int)
	for _, p := range prds {
		out[prdCountStatus(p, shippedEnabled, countSeparately)]++
	}
	return out
}

// CountAutomationStates returns one count per displayed Automation status
// across autos, using the same shipped/cancelled separation as
// CountPRDStates for Automation records.
func CountAutomationStates(autos []*Automation, shippedEnabled, countSeparately bool) map[string]int {
	out := make(map[string]int)
	for _, a := range autos {
		out[automationCountStatus(a, shippedEnabled, countSeparately)]++
	}
	return out
}

// ShippedCounts is the per-status count pair the shipped-state read surfaces
// return: the PRD buckets and the Automation buckets, each one count per
// displayed status with shipped (when the shipped surface is enabled and
// the counts separate it) separate from cancelled.
type ShippedCounts struct {
	PRDs        map[string]int `json:"prds"`
	Automations map[string]int `json:"automations"`
}

// BuildShippedCounts computes the ShippedCounts for one PRD list and one
// Automation list under the given shipped-surface settings. Both lists may be
// nil — the respective bucket then stays empty.
func BuildShippedCounts(prds []*PRD, autos []*Automation, shippedEnabled, countSeparately bool) ShippedCounts {
	return ShippedCounts{
		PRDs:        CountPRDStates(prds, shippedEnabled, countSeparately),
		Automations: CountAutomationStates(autos, shippedEnabled, countSeparately),
	}
}

// PRDShippedMatch reports whether p survives a shipped selection: the
// "shipped" selection keeps only records carrying shipped state while the
// "unshipped" selection keeps only those without. With the shipped surface
// disabled the selection applies to nothing — no record has anything to
// display on shipped state — so every record survives either selection.
func PRDShippedMatch(p *PRD, shippedEnabled, wanted bool) bool {
	if !shippedEnabled {
		return true
	}
	if p == nil {
		return false
	}
	return p.IsShipped() == wanted
}

// AutomationShippedMatch reports whether a survives a shipped selection for
// an Automation (same rule as PRDShippedMatch).
func AutomationShippedMatch(a *Automation, shippedEnabled, wanted bool) bool {
	if !shippedEnabled {
		return true
	}
	if a == nil {
		return false
	}
	return a.IsShipped() == wanted
}

// ShippedCounts computes the shipped-state counts for this Manager's whole
// stored PRD list and Automation list in one call, under the given
// shipped-surface settings. A nil Manager returns the empty counts.
func (m *Manager) ShippedCounts(shippedEnabled, countSeparately bool) ShippedCounts {
	if m == nil {
		return ShippedCounts{PRDs: map[string]int{}, Automations: map[string]int{}}
	}
	return BuildShippedCounts(m.Store().ListPRDs(), m.ListAutomations(), shippedEnabled, countSeparately)
}
