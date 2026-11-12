// Startup tokens — estimating the token size of a worker's starting context
// for the per-attempt record (TaskAttempt in failure_escalation.go).
//
// A launch estimates the size from the generated starting context
// (EstimateTokens) and marks the source as estimated; a value the worker or
// its runner reports at runtime is recorded with the reported source
// instead.
package autonomous

// TaskAttempt.StartupTokenSource values: how the attempt's startup token
// size was obtained.
const (
	// StartupTokenSourceEstimated marks a size estimated from the generated
	// starting context at launch (EstimateTokens).
	StartupTokenSourceEstimated = "estimated"
	// StartupTokenSourceReported marks a size the worker or its runner
	// reported at runtime.
	StartupTokenSourceReported = "reported"
)

// EstimateTokens estimates the token size of the starting context text s:
// one token per four bytes, rounded up. The empty context estimates to zero
// tokens.
func EstimateTokens(s string) int {
	return (len(s) + 3) / 4
}
