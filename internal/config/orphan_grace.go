package config

// OrphanGraceDefaultSeconds is the default grace period of the autonomous
// orphan sweep, used when autonomous.orphan_grace_seconds is unset or not
// positive.
const OrphanGraceDefaultSeconds = 300

// NormalizeOrphanGraceSeconds returns the effective orphan grace period in
// seconds: the configured value when positive, otherwise
// OrphanGraceDefaultSeconds.
func NormalizeOrphanGraceSeconds(v int) int {
	if v <= 0 {
		return OrphanGraceDefaultSeconds
	}
	return v
}
