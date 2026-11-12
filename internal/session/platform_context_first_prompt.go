package session

import "github.com/dmz006/datawatch/internal/platformcontext"

// FirstPromptContext returns the text to prepend to a session's first prompt,
// or "" when nothing should be prepended: the setting is off, the context was
// already delivered, or the backend family does not use first-prompt delivery
// (rules_file backends read the guardrails file instead).
//
// Limitation: this path delivers the text once, at the first prompt. It cannot
// re-deliver after the backend compacts its context, because the backends that
// use first_prompt delivery have no start or compact hook to trigger a resend.
func FirstPromptContext(family string, alreadyDelivered, enabled bool) string {
	if !enabled || alreadyDelivered {
		return ""
	}
	switch platformcontext.DeliveryModes()[family] {
	case platformcontext.DeliveryFirstPrompt, platformcontext.DeliveryRulesFileAndFirstPrompt:
		return platformcontext.PlatformContext
	}
	return ""
}

// FirstPromptContextFor is FirstPromptContext plus the claude-code fallback:
// a claude-code session whose local file was not written (localFileWritten is
// false) gets the platform context as a first-prompt prefix, once.
func FirstPromptContextFor(family string, alreadyDelivered, enabled, localFileWritten bool) string {
	if p := FirstPromptContext(family, alreadyDelivered, enabled); p != "" {
		return p
	}
	if enabled && !alreadyDelivered && !localFileWritten && family == "claude-code" {
		return platformcontext.PlatformContext
	}
	return ""
}

// prependPlatformContextOnce returns prompt with the platform context prefixed
// when this is the session's first prompt for a first_prompt backend, and marks
// the session record so later prompts are left alone. It is called from Start,
// the single place a task prompt is handed to a backend.
func (m *Manager) prependPlatformContextOnce(sess *Session, family, prompt string) string {
	if sess == nil {
		return prompt
	}
	enabled := !m.platformContextFirstPromptOff
	if m.cfg != nil {
		enabled = enabled && m.cfg.PlatformContext.EffectiveEnabled(m.projectPolicyFor(sess.ProjectDir))
	}
	prefix := FirstPromptContextFor(family, sess.PlatformContextDelivered, enabled, sess.PlatformContextLocalFile)
	if prefix == "" {
		return prompt
	}
	if m.cfg != nil {
		prefix = m.cfg.PlatformContext.AppendNote(prefix)
	}
	m.store.Mutate(func() { sess.PlatformContextDelivered = true })
	return prefix + "\n\n" + prompt
}
