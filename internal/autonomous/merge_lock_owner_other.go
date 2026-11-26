//go:build !unix

package autonomous

// processExists cannot be answered here, so no lock is ever taken over.
func processExists(int) bool { return true }
