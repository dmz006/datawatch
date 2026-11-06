// Package compute — verifier slot helpers.
//
// This file is for computing how many verifier slots a node takes and how
// much free GPU memory a verifier needs before it starts, plus the advice
// figure operators look at. The functions are pure and read: none of them
// applies a value or changes anything. The slot suggestion in particular is
// advice only — nothing applies it automatically and it never raises the
// executor cap.
package compute

import "fmt"

// EffectiveVerifierSlots is the verifier slot count a node takes: the
// operator's explicit verifier_slots value when it is set (an explicit 0
// returns 0, a node with no verifier workers), otherwise 1 when the node
// hosts the local engine the verifier judges against and 0 when it does
// not.
func EffectiveVerifierSlots(explicit *int, hostsLocalEngine bool) int {
	if explicit != nil {
		return *explicit
	}
	if hostsLocalEngine {
		return 1
	}
	return 0
}

// EffectiveVerifierMinFreeMB is the minimum free GPU memory, in MB, a
// verifier slot wants free before it starts: the operator's explicit
// verifier_min_free_mem_mb value when it is set (0 means the check is off),
// otherwise the default of 1024.
func EffectiveVerifierMinFreeMB(explicit *int) int {
	if explicit == nil {
		return 1024
	}
	return *explicit
}

// FreeGPUMB is the free GPU memory of a node in MB: the sum over its GPUs
// of each GPU's total minus its used memory. A GPU's used memory already
// includes the model that is resident on it, so the figure stays free
// memory after the resident model. ok is false when the node has no GPU
// data at all or any GPU reports zero total memory. A GPU whose used
// memory is greater than its total counts as zero free.
func FreeGPUMB(obs NodeObservation) (freeMB uint64, ok bool) {
	if len(obs.GPUs) == 0 {
		return 0, false
	}
	for _, g := range obs.GPUs {
		if g.TotalBytes == 0 {
			return 0, false
		}
		if g.UsedBytes < g.TotalBytes {
			freeMB += bytesToMB(g.TotalBytes - g.UsedBytes)
		}
	}
	return freeMB, true
}

// SuggestVerifierSlots is the advice figure for how many verifier slots the
// node's free GPU memory would hold: the free memory divided by the
// context size in MB each verifier slot takes, rounded down. ok is false
// when the context size is 0 or the node has no usable GPU data, so there
// is nothing to divide by or nothing to measure.
//
// The suggestion is advice only: nothing applies it automatically and it
// never raises the executor cap.
func SuggestVerifierSlots(obs NodeObservation, contextMB int) (suggested int, ok bool) {
	if contextMB == 0 {
		return 0, false
	}
	freeMB, ok := FreeGPUMB(obs)
	if !ok {
		return 0, false
	}
	return int(freeMB) / contextMB, true
}

// HeadroomCheck is whether the node's free GPU memory is at least the
// minimum a verifier slot wants free, in MB. When the minimum is 0 the
// check is off; when the node has no usable GPU data there is nothing to
// check. The ok flag is true in both of those cases and otherwise says
// whether the free memory meets the minimum. The reason is plain English
// for operators and names no tracker IDs.
func HeadroomCheck(obs NodeObservation, minFreeMB int) (ok bool, reason string) {
	if minFreeMB == 0 {
		return true, "headroom check is off"
	}
	freeMB, ok := FreeGPUMB(obs)
	if !ok {
		return true, "no GPU data, so headroom is not checked"
	}
	free := int(freeMB)
	if free >= minFreeMB {
		return true, fmt.Sprintf("%d MB free meets the minimum of %d MB", free, minFreeMB)
	}
	return false, fmt.Sprintf("%d MB free is below the minimum of %d MB", free, minFreeMB)
}
