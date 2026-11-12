package session

import "github.com/dmz006/datawatch/internal/platformcontext"

// PlatformContextStatus is what is in force for one session.
type PlatformContextStatus struct {
	// Enabled is the effective value: project override when set, else global.
	Enabled bool
	// ProjectOverride is the project's own value, nil when none is set.
	ProjectOverride *bool
	// Delivery is the delivery mode for the session's backend family.
	Delivery platformcontext.DeliveryMode
}

// PlatformContextStatusFor reports the platform context state for sess.
func (m *Manager) PlatformContextStatusFor(sess *Session) PlatformContextStatus {
	st := PlatformContextStatus{Enabled: true}
	if sess == nil {
		return st
	}
	st.Delivery = platformcontext.DeliveryModes()[sess.BackendFamily]
	if m.cfg != nil {
		policy := m.projectPolicyFor(sess.ProjectDir)
		if policy != nil && policy.PlatformContextEnabled != nil {
			v := *policy.PlatformContextEnabled
			st.ProjectOverride = &v
		}
		st.Enabled = m.cfg.PlatformContext.EffectiveEnabled(policy)
	}
	return st
}
