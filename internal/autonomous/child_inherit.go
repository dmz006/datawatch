// Child inheritance: when an Automaton is created with a parent, it starts
// from the parent's setup instead of from nothing. Setting:
// autonomous.automation.child_inherit (on by default; off copies nothing).
//
// What a child inherits, each only where the caller gave no explicit value:
//   - the project link (ProjectProfile) and the project's repository
//     directory as ProjectDir, never a git worktree path;
//   - the parent's Automation: the child joins it as a member right after the
//     parent, through InsertMember, so held inserts and the agent insert policy
//     apply as they do for any insert;
//   - Backend and Model, VerificationBackend and VerificationModel, where a
//     model only travels together with its backend;
//   - Tags;
//   - Priority and PrioritySource.
//
// Model tier rule: a child of a Sonnet or Haiku parent never ends up on an
// Opus or Fable model through inheritance; the pair is clamped to the parent's
// entry. A value the caller gave explicitly is never touched.

package autonomous

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"
)

// childInheritDecisionKind is the Decision kind of the one entry
// inheritFromParent records on the child. It also marks the child as done, so
// a second call (for example defect intake after creation) changes nothing.
const childInheritDecisionKind = "child_inherit"

// childInheritActor is the actor the Automation insert of an inheriting child
// is made under. It is an agent actor, so agent_insert_policy applies to it.
const childInheritActor = "agent:child_inherit"

// childExplicit names the fields the caller of a creation path set itself. An
// explicit value always wins over inheritance. The zero value means the
// caller set nothing.
type childExplicit struct {
	Project             bool // ProjectProfile
	ProjectDir          bool
	Automation          bool // the caller placed the child in an Automation itself
	Backend             bool
	Model               bool
	VerificationBackend bool
	VerificationModel   bool
	Tags                bool
	Priority            bool
}

// Model tiers for the clamp. Unknown names rank 0 and are never clamped.
const (
	modelTierUnknown = 0
	modelTierSmall   = 1 // Haiku
	modelTierMid     = 2 // Sonnet
	modelTierTop     = 3 // Opus, Fable
)

// modelTier ranks a model name by family. The match is on the family word so
// it covers every version and provider-qualified spelling.
func modelTier(model string) int {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "opus"), strings.Contains(m, "fable"):
		return modelTierTop
	case strings.Contains(m, "sonnet"):
		return modelTierMid
	case strings.Contains(m, "haiku"):
		return modelTierSmall
	}
	return modelTierUnknown
}

// capsAtMid reports whether a parent entry with this model keeps its children
// below the top tier.
func capsAtMid(parentModel string) bool {
	t := modelTier(parentModel)
	return t == modelTierSmall || t == modelTierMid
}

// inheritFromParent copies the parent's setup onto child once, at creation.
// It returns at once, copying nothing and recording nothing, when
// autonomous.automation.child_inherit is off, when either record is missing,
// or when the child was already handled. It never fails the creation: what
// could not be inherited is recorded in the child's Decisions.
func (m *Manager) inheritFromParent(child, parent *PRD, explicit childExplicit) {
	if child == nil || parent == nil || child.ID == parent.ID {
		return
	}
	if !m.Config().Automation.EffectiveChildInherit() {
		return
	}
	done := false
	m.store.WithLock(func() {
		for _, d := range child.Decisions {
			if d.Kind == childInheritDecisionKind {
				done = true
				return
			}
		}
	})
	if done {
		return
	}
	var inherited, clamped, skipped []string

	// Copy the scalar fields under the store lock; the Automation insert below
	// takes its own locks and must run outside it.
	dirNote := ""
	repoDir := ""
	if !explicit.ProjectDir && parent.ProjectDir != "" {
		repoDir, dirNote = m.parentRepoDir(parent)
	}
	m.store.WithLock(func() {
		if !explicit.Project && parent.ProjectProfile != "" {
			child.ProjectProfile = parent.ProjectProfile
			inherited = append(inherited, "project link")
		}
		if !explicit.ProjectDir && parent.ProjectDir != "" {
			if repoDir != "" {
				child.ProjectDir = repoDir
				inherited = append(inherited, "project directory")
			} else {
				skipped = append(skipped, "project directory ("+dirNote+")")
			}
		}
		inheritLLMPair(&child.Backend, &child.Model, parent.Backend, parent.Model, explicit.Backend, explicit.Model,
			"backend and model", &inherited, &clamped)
		inheritLLMPair(&child.VerificationBackend, &child.VerificationModel, parent.VerificationBackend, parent.VerificationModel,
			explicit.VerificationBackend, explicit.VerificationModel, "verification backend and model", &inherited, &clamped)
		if !explicit.Tags && len(parent.TagIDs) > 0 {
			child.TagIDs = unionStrings(child.TagIDs, parent.TagIDs)
			if child.PrimaryTagID == "" {
				child.PrimaryTagID = parent.PrimaryTagID
			}
			inherited = append(inherited, "tags")
		}
		if !explicit.Priority && (parent.PrioritySource != "" || parent.Priority != 0) {
			child.Priority = parent.Priority
			child.PrioritySource = parent.PrioritySource
			inherited = append(inherited, "priority")
		}
		child.UpdatedAt = time.Now()
	})
	_ = m.store.SavePRD(child)

	if !explicit.Automation {
		if note, ok := m.inheritAutomation(child, parent); ok {
			inherited = append(inherited, note)
		} else if note != "" {
			skipped = append(skipped, note)
		}
	}

	m.recordChildInherit(child, parent, inherited, clamped, skipped)
}

// inheritLLMPair copies a backend/model pair from the parent onto the child.
// A model only inherits together with its backend: when the child's backend
// is its own (explicit, or a different one from the parent's), the parent's
// model is not copied. Afterwards the tier clamp applies to an inherited or
// pre-set (non-explicit) model.
func inheritLLMPair(childBackend, childModel *string, parentBackend, parentModel string,
	explicitBackend, explicitModel bool, label string, inherited, clamped *[]string) {
	changed := false
	switch {
	case parentBackend == "":
		// Nothing to inherit: a model alone does not travel.
	case !explicitBackend && !explicitModel:
		if *childBackend != parentBackend || *childModel != parentModel {
			changed = true
		}
		*childBackend, *childModel = parentBackend, parentModel
	case explicitBackend && !explicitModel && *childBackend == parentBackend && *childModel != parentModel:
		*childModel = parentModel
		changed = true
	}
	if changed {
		*inherited = append(*inherited, label)
	}
	// The clamp covers values the caller did not give explicitly; it swaps the
	// whole pair, so it never runs over an explicit backend either.
	if explicitModel || explicitBackend || !capsAtMid(parentModel) || modelTier(*childModel) != modelTierTop {
		return
	}
	*clamped = append(*clamped, fmt.Sprintf("%s: %s/%s held to the parent's %s/%s", label, *childBackend, *childModel, parentBackend, parentModel))
	*childBackend, *childModel = parentBackend, parentModel
}

// unionStrings returns a followed by the entries of b that a lacks.
func unionStrings(a, b []string) []string {
	seen := make(map[string]bool, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, s := range append(append([]string(nil), a...), b...) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// parentRepoDir resolves the repository directory for the parent's project
// directory. A linked git worktree resolves to the repository that owns it
// (the parent of the shared git directory), never to the worktree path. A
// directory that is not a linked worktree is returned as it is. It returns an
// empty directory and a reason when a worktree path cannot be resolved.
func (m *Manager) parentRepoDir(parent *PRD) (dir, reason string) {
	pd := filepath.Clean(parent.ProjectDir)
	m.mu.Lock()
	baseRepo := m.cfg.WorktreeBaseRepo
	m.mu.Unlock()
	worktreeParent := parent.Git.Branch != ""
	out, err := runGitIn(pd, "rev-parse", "--git-dir", "--git-common-dir")
	if err != nil {
		// Not a git checkout the daemon can read. A worktree-mode parent's
		// directory is a worktree path: use the configured base repository.
		if worktreeParent && baseRepo != "" {
			return filepath.Clean(baseRepo), ""
		}
		if worktreeParent {
			return "", "the parent runs in a worktree and its repository could not be resolved"
		}
		return pd, ""
	}
	lines := strings.Fields(strings.TrimSpace(out))
	if len(lines) < 2 {
		return pd, ""
	}
	gitDir, commonDir := absAgainst(pd, lines[0]), absAgainst(pd, lines[1])
	if evalOrClean(gitDir) == evalOrClean(commonDir) {
		return pd, "" // an ordinary checkout, or a subdirectory of one
	}
	// A linked worktree: its repository is the parent of the common git dir.
	common := evalOrClean(commonDir)
	if filepath.Base(common) == ".git" {
		return filepath.Dir(common), ""
	}
	if baseRepo != "" {
		return filepath.Clean(baseRepo), ""
	}
	return "", "the parent runs in a worktree and its repository could not be resolved"
}

// absAgainst makes p absolute against base, the way git prints relative paths.
func absAgainst(base, p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(base, p)
}

// inheritAutomation adds the child to the parent's Automation, right after the
// parent's member. It reports (note, true) when the child joined or a pending
// insert was recorded, and (reason, false) when it was skipped. It skips
// quietly when the parent has no Automation, the Automation is completed or
// cancelled, or the parent is not one of its members.
func (m *Manager) inheritAutomation(child, parent *PRD) (string, bool) {
	a := m.AutomationForPRD(parent)
	if a == nil {
		return "", false
	}
	if a.Status == AutomationCompleted || a.Status == AutomationCancelled {
		return fmt.Sprintf("Automation %s is %s", a.ID, a.Status), false
	}
	if m.automations.OwnerOf(child.ID) != "" {
		return "", false
	}
	var after string
	for _, mem := range a.Members {
		if mem.PRDID == parent.ID {
			after = mem.ID
			break
		}
	}
	if after == "" {
		return fmt.Sprintf("the parent is not a member of Automation %s", a.ID), false
	}
	updated, err := m.InsertMember(a.ID, InsertMemberInput{
		PRDID:           child.ID,
		AfterMemberID:   after,
		Actor:           childInheritActor,
		Reason:          "child of Automaton " + parent.ID + " joins its parent's Automation",
		ExpectedVersion: a.Version,
	})
	if err != nil {
		log.Printf("[autonomous] child inherit: insert %s into automation %s: %v", child.ID, a.ID, err)
		return fmt.Sprintf("Automation %s insert refused: %v", a.ID, err), false
	}
	for _, mem := range updated.Members {
		if mem.PRDID == child.ID {
			return "Automation " + a.ID + " (joined after the parent)", true
		}
	}
	return "Automation " + a.ID + " (insert held for approval)", true
}

// inheritTitle names a parent for the record: its title, else the first line
// of its spec, shortened.
func inheritTitle(p *PRD) string {
	t := strings.TrimSpace(p.Title)
	if t == "" {
		t, _, _ = strings.Cut(strings.TrimSpace(p.Spec), "\n")
	}
	if r := []rune(t); len(r) > 60 {
		t = string(r[:60]) + "…"
	}
	return t
}

// recordChildInherit appends the one Decision saying what the child inherited
// and from which parent, and what was clamped or skipped.
func (m *Manager) recordChildInherit(child, parent *PRD, inherited, clamped, skipped []string) {
	note := fmt.Sprintf("inherited from parent Automaton %s (%s): ", parent.ID, inheritTitle(parent))
	if len(inherited) == 0 {
		note += "nothing to inherit"
	} else {
		note += strings.Join(inherited, ", ")
	}
	if len(clamped) > 0 {
		note += "; clamped: " + strings.Join(clamped, "; ")
	}
	if len(skipped) > 0 {
		note += "; skipped: " + strings.Join(skipped, "; ")
	}
	dec := Decision{At: time.Now(), Kind: childInheritDecisionKind, Actor: "autonomous", Note: note,
		Backend: child.Backend, Model: child.Model}
	m.store.WithLock(func() {
		child.Decisions = append(child.Decisions, dec)
		child.UpdatedAt = dec.At
	})
	_ = m.store.SavePRD(child)
}
