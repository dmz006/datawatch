package config

import "fmt"

// Defaults for the admission classes (autonomous.capacity_* keys). They are
// *int in the struct so an unset key (nil) reads as the default while an
// explicit 0 stays 0.
const (
	// DefaultCapacityAccountClaudeLimit is the cap on the whole Claude
	// account: claude-code plus claude-lean, one lane each.
	DefaultCapacityAccountClaudeLimit = 2
	// DefaultCapacitySupervisorReserveClaude is the Claude slots only the
	// supervisor class may use above the cap.
	DefaultCapacitySupervisorReserveClaude = 1
	// DefaultCapacitySupervisorReservePi is the same for the pi pool.
	DefaultCapacitySupervisorReservePi = 1
	// DefaultCapacitySupervisorReserveHost is the host session slots only the
	// supervisor class may use above the session cap.
	DefaultCapacitySupervisorReserveHost = 4
	// DefaultCapacityOperatorReserveHost is the host session slots above
	// session.max_sessions that only operator starts may use.
	DefaultCapacityOperatorReserveHost = 5
)

func intOr(p *int, def int) int {
	if p == nil {
		return def
	}
	return *p
}

// CapacityAccountClaudeLimit is the effective account:claude limit (0 =
// unlimited). Read on every pool sync.
func (a *AutonomousConfig) CapacityAccountClaudeLimit() int {
	if a == nil {
		return DefaultCapacityAccountClaudeLimit
	}
	return intOr(a.CapacityAccountClaudeLimitSetting, DefaultCapacityAccountClaudeLimit)
}

// CapacitySupervisorReserveClaude is the effective Claude supervisor buffer.
func (a *AutonomousConfig) CapacitySupervisorReserveClaude() int {
	if a == nil {
		return DefaultCapacitySupervisorReserveClaude
	}
	return intOr(a.CapacitySupervisorReserveClaudeSetting, DefaultCapacitySupervisorReserveClaude)
}

// CapacitySupervisorReservePi is the effective pi supervisor buffer.
func (a *AutonomousConfig) CapacitySupervisorReservePi() int {
	if a == nil {
		return DefaultCapacitySupervisorReservePi
	}
	return intOr(a.CapacitySupervisorReservePiSetting, DefaultCapacitySupervisorReservePi)
}

// CapacityOperatorReserveHost is the effective operator allowance above
// session.max_sessions (0 = operators stop at the cap too).
func (a *AutonomousConfig) CapacityOperatorReserveHost() int {
	if a == nil {
		return DefaultCapacityOperatorReserveHost
	}
	return intOr(a.CapacityOperatorReserveHostSetting, DefaultCapacityOperatorReserveHost)
}

// CapacitySupervisorReserveHost is the effective host session-slot buffer.
func (a *AutonomousConfig) CapacitySupervisorReserveHost() int {
	if a == nil {
		return DefaultCapacitySupervisorReserveHost
	}
	return intOr(a.CapacitySupervisorReserveHostSetting, DefaultCapacitySupervisorReserveHost)
}

// CapacitySupervisorCredentialOn reports whether a supervisor credential makes a
// REST start the supervisor class (default on). Off, a credentialed start is an
// operator start and the credential does nothing.
func (a *AutonomousConfig) CapacitySupervisorCredentialOn() bool {
	return a == nil || a.CapacitySupervisorCredentialEnabled == nil || *a.CapacitySupervisorCredentialEnabled
}

// validateCapacityAdmission rejects a negative admission value.
func validateCapacityAdmission(a *AutonomousConfig) error {
	for k, p := range map[string]*int{
		"capacity_account_claude_limit":      a.CapacityAccountClaudeLimitSetting,
		"capacity_supervisor_reserve_claude": a.CapacitySupervisorReserveClaudeSetting,
		"capacity_supervisor_reserve_pi":     a.CapacitySupervisorReservePiSetting,
		"capacity_supervisor_reserve_host":   a.CapacitySupervisorReserveHostSetting,
		"capacity_operator_reserve_host":     a.CapacityOperatorReserveHostSetting,
	} {
		if p != nil && *p < 0 {
			return fmt.Errorf("autonomous.%s: %d must be >= 0", k, *p)
		}
	}
	return nil
}
