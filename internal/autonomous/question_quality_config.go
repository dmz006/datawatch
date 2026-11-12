package autonomous

import "github.com/dmz006/datawatch/internal/config"

// QualityPolicyFromConfig builds the question quality policy from
// autonomous.questions. Absent keys read their defaults through the Effective
// helpers, so the result is complete. Do not pass it through
// NormalizeQualityPolicy: that would turn an explicit quality_min_text_chars
// of 0 (no length check) back into the default.
func QualityPolicyFromConfig(q config.AutonomousQuestionsConfig) QualityPolicy {
	return QualityPolicy{
		Mode:             q.EffectiveQualityMode(),
		MinTextChars:     q.EffectiveQualityMinTextChars(),
		TrackerIDPattern: q.EffectiveQualityTrackerIDPattern(),
	}
}
