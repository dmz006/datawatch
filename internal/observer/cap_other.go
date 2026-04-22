//go:build !linux

// BL171 / v4.1.1 — capability probe is Linux-only. Other platforms
// always report "no capability" so the PWA renders the right
// "eBPF unsupported on this OS" badge.

package observer

func probeBPFCapabilityPlatform() bool { return false }
