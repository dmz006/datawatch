//go:build !linux

package resources

// probeScope reports unavailable: user scopes exist only on Linux.
func probeScope() bool { return false }
