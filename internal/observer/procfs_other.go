//go:build !linux

// BL171 (S9) — non-Linux stub. The full /proc walk is Linux-only;
// macOS and Windows get empty process records so the rest of the
// StatsResponse v2 still renders (host / cpu / mem / disk / gpu via
// host-agnostic paths in collector.go).

package observer

func walkProc() ([]ProcRecord, error) {
	return nil, nil
}

func readHostFields() (os_, kernel, arch string) { return "", "", "" }
func readLoadavg() (l1, l5, l15 float64)          { return 0, 0, 0 }
func readMemInfo() Mem                            { return Mem{} }
func readUptimeSeconds() int64                    { return 0 }

// runtimeNumCPU returns the core count; non-Linux falls back to
// the generic runtime.NumCPU() path.
func runtimeNumCPU() int { return runtimeNumCPUGeneric() }
