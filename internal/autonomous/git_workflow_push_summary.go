package autonomous

import "fmt"

// PushSummary renders a PRD's push outcome as one operator-facing line
// fragment for the read surfaces (CLI prd-get, web views, API
// consumers). It reports exactly what the stored push state says:
//
//   - PushStatePushed    → "pushed <ref>@<short sha>" (the sha is cut
//     to its first 7 characters when it is longer)
//   - PushStateNotPushed → "not pushed: <warning>"
//   - "" (no outcome yet, no push attempted) → ""
func (g GitWorkflow) PushSummary() string {
	switch g.PushState {
	case PushStatePushed:
		sha := g.PushedSHA
		if len(sha) > 7 {
			sha = sha[:7]
		}
		return fmt.Sprintf("pushed %s@%s", g.PushedRef, sha)
	case PushStateNotPushed:
		return "not pushed: " + g.PushWarning
	default:
		return ""
	}
}
