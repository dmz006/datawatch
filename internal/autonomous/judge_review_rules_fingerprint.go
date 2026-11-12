package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"sort"

	"github.com/dmz006/datawatch/internal/config"
)

// reviewRulesUnreadableMarker stands in for the bytes of a rules or context
// file that is present but cannot be read, so the fingerprint still tells a
// readable file from an unreadable one.
const reviewRulesUnreadableMarker = "\x01unreadable\x01"

// reviewRulesFingerprint returns a hex SHA-256 over the exact rules text the
// completion judge reads for this Automaton, or "" when there is none (no
// project directory, or no rules or context file).
//
// It follows rulesReviewSuffix. With the base-commit source the review text
// already embeds each rules and context file as it was at the base commit, so
// that text is hashed. With the working-tree source the review text is only a
// pointer naming the files, so the pointer text is hashed together with the
// name and bytes of each present file, in sorted name order, read from the
// project directory. The generated-artefact review text is part of both.
//
// It never calls rulesReviewSuffix: that records a fallback Decision when the
// base commit cannot be read. This function appends no Decision and saves
// nothing; on a base-commit read error it falls back to the working-tree text
// silently.
func (m *Manager) reviewRulesFingerprint(ctx context.Context, prd *PRD) string {
	if prd == nil || prd.ProjectDir == "" {
		return ""
	}
	if m.Config().Automation.EffectiveRulesReviewSource() == config.RulesReviewSourceBaseCommit {
		files, sha, _, err := m.readRulesFilesAtBase(ctx, prd)
		if err == nil {
			text := baseCommitRulesReviewText(files, sha)
			if text == "" {
				return ""
			}
			return JudgeVerdictRulesHash(text + m.generatedArtefactReviewText(prd))
		}
	}
	return m.worktreeRulesFingerprint(prd)
}

// worktreeRulesFingerprint hashes the working-tree pointer text and, for each
// present rules or context file in sorted name order, its name and bytes.
func (m *Manager) worktreeRulesFingerprint(prd *PRD) string {
	pointer := m.worktreeRulesReviewSuffix(prd)
	if pointer == "" {
		return ""
	}
	names := presentRulesContextNames(prd, m.resolvedRulesContext(prd))
	sort.Strings(names)
	parts := make([]string, 0, 1+2*len(names))
	parts = append(parts, pointer)
	for _, name := range names {
		content := reviewRulesUnreadableMarker
		// #nosec G304 -- the name comes from the operator-configured rules and context files and the path stays inside the Automaton's own project directory
		if b, err := os.ReadFile(filepath.Join(prd.ProjectDir, name)); err == nil {
			content = string(b)
		}
		parts = append(parts, name, content)
	}
	return JudgeVerdictRulesHash(parts...)
}
