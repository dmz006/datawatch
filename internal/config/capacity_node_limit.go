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
