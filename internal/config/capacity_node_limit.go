package config

import "time"

// DefaultCapacityAdoptionWaitSeconds is how long, after a daemon restart, new
// capacity requests wait for the surviving work to be adopted when
// autonomous.capacity_adoption_wait_seconds is unset.
const DefaultCapacityAdoptionWaitSeconds = 30

// The three node over-admission settings are *bool so "unset" (nil, no key in
// the config file) is distinguishable from an explicit false: unset is on,
// the same shape as CapacityEnabled.

// CapacityStrictNodeLimitOn reports whether a node's limit is strict (default on).
func (a *AutonomousConfig) CapacityStrictNodeLimitOn() bool {
	return a == nil || a.CapacityStrictNodeLimit == nil || *a.CapacityStrictNodeLimit
}

// CapacityCountPoolByNodeOn reports whether an llm: pool counts by node (default on).
func (a *AutonomousConfig) CapacityCountPoolByNodeOn() bool {
	return a == nil || a.CapacityCountPoolByNode == nil || *a.CapacityCountPoolByNode
}

// CapacityAdoptBeforeAdmitOn reports whether resumed workers are admitted
// before new work after a restart (default on).
func (a *AutonomousConfig) CapacityAdoptBeforeAdmitOn() bool {
	return a == nil || a.CapacityAdoptBeforeAdmit == nil || *a.CapacityAdoptBeforeAdmit
}

// CapacityAdoptionWait is the effective restart adoption wait: unset or 0
// reads as DefaultCapacityAdoptionWaitSeconds.
func (a *AutonomousConfig) CapacityAdoptionWait() time.Duration {
	if a == nil || a.CapacityAdoptionWaitSeconds <= 0 {
		return DefaultCapacityAdoptionWaitSeconds * time.Second
	}
	return time.Duration(a.CapacityAdoptionWaitSeconds) * time.Second
}

// DefaultCapacitySlotReconcileGraceSeconds is how long a fresh lease counts as
// having a node lease in the orphan-slot reconcile when
// autonomous.capacity_slot_reconcile_grace_seconds is unset or 0.
const DefaultCapacitySlotReconcileGraceSeconds = 60

// MinCapacitySlotReconcileGraceSeconds is the smallest accepted non-zero grace.
const MinCapacitySlotReconcileGraceSeconds = 10

// CapacitySlotReconcileGrace is the effective slot reconcile grace: unset or 0
// reads as DefaultCapacitySlotReconcileGraceSeconds.
func (a *AutonomousConfig) CapacitySlotReconcileGrace() time.Duration {
	if a == nil || a.CapacitySlotReconcileGraceSeconds <= 0 {
		return DefaultCapacitySlotReconcileGraceSeconds * time.Second
	}
	return time.Duration(a.CapacitySlotReconcileGraceSeconds) * time.Second
}
