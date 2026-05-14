// BL303 S2 — guardrail registry + scan adapter.
//
// The registry holds named guardrail entries. Built-in scan guardrails
// (sast-scan, secrets-scan, deps-scan) are pre-registered at Manager
// construction. Skills can register additional entries via RegisterGuardrail.
//
// resolveGuardrails returns the effective list for a PRD at a given level,
// applying the per-PRD override precedence:
//
//	explicit PerTask/PerStoryGuardrails > named GuardrailProfile > global Config.

package autonomous

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous/scan"
)

// initGuardrailLibrary pre-registers the three built-in scan guardrails.
// Called from NewManager.
func (m *Manager) initGuardrailLibrary() {
	m.guardrailLib = []GuardrailEntry{
		{
			Name:        "sast-scan",
			Description: "Static Application Security Testing — regex-based source code analysis",
			Type:        "scan",
			ScanType:    "sast",
		},
		{
			Name:        "secrets-scan",
			Description: "Hardcoded credential and secret detection",
			Type:        "scan",
			ScanType:    "secrets",
		},
		{
			Name:        "deps-scan",
			Description: "Dependency manifest and known-CVE vulnerability scanning",
			Type:        "scan",
			ScanType:    "deps",
		},
	}
}

// GuardrailLibrary returns all registered guardrails (built-in + skill-contributed).
func (m *Manager) GuardrailLibrary() []GuardrailEntry {
	m.guardrailLibMu.RLock()
	defer m.guardrailLibMu.RUnlock()
	out := make([]GuardrailEntry, len(m.guardrailLib))
	copy(out, m.guardrailLib)
	return out
}

// RegisterGuardrail adds or replaces a guardrail entry in the library.
// Replaces an existing entry with the same Name; appends otherwise.
func (m *Manager) RegisterGuardrail(entry GuardrailEntry) {
	m.guardrailLibMu.Lock()
	defer m.guardrailLibMu.Unlock()
	for i, e := range m.guardrailLib {
		if e.Name == entry.Name {
			m.guardrailLib[i] = entry
			return
		}
	}
	m.guardrailLib = append(m.guardrailLib, entry)
}

// lookupGuardrailEntry returns the entry for name, or (zero, false) on miss.
func (m *Manager) lookupGuardrailEntry(name string) (GuardrailEntry, bool) {
	m.guardrailLibMu.RLock()
	defer m.guardrailLibMu.RUnlock()
	for _, e := range m.guardrailLib {
		if e.Name == name {
			return e, true
		}
	}
	return GuardrailEntry{}, false
}

// resolveGuardrails returns the effective guardrail names for prd at level
// ("task" or "story"). Priority: per-PRD explicit > named profile > global Config.
func (m *Manager) resolveGuardrails(prd *PRD, level string) []string {
	if level == "task" && len(prd.PerTaskGuardrails) > 0 {
		return append([]string{}, prd.PerTaskGuardrails...)
	}
	if level == "story" && len(prd.PerStoryGuardrails) > 0 {
		return append([]string{}, prd.PerStoryGuardrails...)
	}
	if prd.GuardrailProfile != "" {
		if p, ok := m.store.GetGuardrailProfile(prd.GuardrailProfile); ok {
			return append([]string{}, p.Guardrails...)
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if level == "task" {
		return append([]string{}, m.cfg.PerTaskGuardrails...)
	}
	return append([]string{}, m.cfg.PerStoryGuardrails...)
}

// invokeScanGuardrail runs the targeted scanner(s) for a scan-type guardrail.
// It respects the global scan config thresholds (FailOnSeverity, MaxFindings)
// but enables only the scanner matching entry.ScanType.
func (m *Manager) invokeScanGuardrail(entry GuardrailEntry, inv GuardrailInvocation) (GuardrailVerdict, error) {
	m.mu.Lock()
	sc := m.cfg.Scan
	m.mu.Unlock()

	// Enable only the targeted scanner.
	sc.SASTEnabled = entry.ScanType == "sast"
	sc.SecretsEnabled = entry.ScanType == "secrets"
	sc.DepsEnabled = entry.ScanType == "deps"

	var scanners []scan.Scanner
	if sc.SASTEnabled {
		scanners = append(scanners, scan.NewSASTScanner())
	}
	if sc.SecretsEnabled {
		scanners = append(scanners, scan.NewSecretsScanner())
	}
	if sc.DepsEnabled {
		scanners = append(scanners, scan.NewDepsScanner())
	}

	result := scan.Run(inv.ProjectDir, sc, scanners, nil)

	outcome := "pass"
	if !result.Pass {
		// Downgrade "fail" (scan verdict) to "block" (guardrail outcome) for
		// severity ≥ FailOnSeverity. Warn for lower-severity findings.
		if len(result.Findings) > 0 {
			outcome = "block"
			// If all findings are below FailOnSeverity, warn rather than block.
			allLow := true
			thresh := string(sc.FailOnSeverity)
			for _, f := range result.Findings {
				if severityGE(string(f.Severity), thresh) {
					allLow = false
					break
				}
			}
			if allLow {
				outcome = "warn"
			}
		}
	}

	summary := fmt.Sprintf("%d finding(s)", len(result.Findings))
	if result.Notes != "" {
		summary = result.Notes
	}
	if result.Error != "" {
		summary = "scan error: " + result.Error
	}

	var issues []string
	for _, f := range result.Findings {
		loc := f.File
		if f.Line > 0 {
			loc = fmt.Sprintf("%s:%d", f.File, f.Line)
		}
		issues = append(issues, fmt.Sprintf("[%s] %s — %s", f.RuleID, loc, f.Message))
	}

	return GuardrailVerdict{
		Guardrail: inv.Guardrail,
		Outcome:   outcome,
		Summary:   summary,
		Issues:    issues,
		VerdictAt: time.Now(),
	}, nil
}

// severityGE returns true if level a >= level b in the severity ordering.
func severityGE(a, b string) bool {
	order := map[string]int{"info": 0, "warning": 1, "error": 2, "critical": 3}
	return order[strings.ToLower(a)] >= order[strings.ToLower(b)]
}

// loadSkillGuardrails reads skill manifests for skills assigned to prd and
// registers any guardrails they declare (Type=="skill"). Silently skips
// skills whose manifests cannot be loaded. Called from Manager.Run before
// the executor loop (T08).
func (m *Manager) loadSkillGuardrails(prd *PRD) {
	if m.skillsDir == "" || len(prd.Skills) == 0 {
		return
	}
	for _, skillName := range prd.Skills {
		manifestPath := fmt.Sprintf("%s/%s/SKILL.md", m.skillsDir, skillName)
		mf, err := m.parseSkillManifest(manifestPath)
		if err != nil || mf == nil {
			continue
		}
		for _, g := range mf.Guardrails {
			m.RegisterGuardrail(GuardrailEntry{
				Name:        g,
				Description: fmt.Sprintf("from skill %s", skillName),
				Type:        "skill",
			})
		}
	}
}

// parseSkillManifest is a minimal YAML-frontmatter reader for skill manifests.
// Returns nil without error when the file does not exist.
func (m *Manager) parseSkillManifest(path string) (*skillGuardrailManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil // missing = silent skip
	}
	// Extract frontmatter between --- delimiters.
	s := string(data)
	parts := strings.SplitN(s, "---", 3)
	if len(parts) < 3 {
		return nil, nil
	}
	// Simple key extraction — only read the guardrails field.
	var mf skillGuardrailManifest
	for _, line := range strings.Split(parts[1], "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "guardrails:") {
			// guardrails: [a, b] inline or subsequent lines; skip complex YAML.
			rest := strings.TrimSpace(strings.TrimPrefix(line, "guardrails:"))
			rest = strings.Trim(rest, "[]")
			for _, g := range strings.Split(rest, ",") {
				g = strings.Trim(strings.TrimSpace(g), `"'`)
				if g != "" {
					mf.Guardrails = append(mf.Guardrails, g)
				}
			}
		}
	}
	return &mf, nil
}

type skillGuardrailManifest struct {
	Guardrails []string
}

// newProfileID generates a short hex ID for a guardrail profile.
func newProfileID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
