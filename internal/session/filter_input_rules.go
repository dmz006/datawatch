// Rules for output filters that type into a session (send_input, schedule):
// they are made by the operator on purpose, start disabled, can never match a
// permission or safety prompt, and are never seeded.

package session

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// TypesInput reports whether a filter with this action types into a session.
func (a FilterAction) TypesInput() bool {
	return a == FilterActionSendInput || a == FilterActionSchedule
}

// ErrInputFilterRefused wraps every refusal of an input-typing filter.
var ErrInputFilterRefused = errors.New("input-typing filter refused")

// promptSamples are lines of permission, safety and confirmation prompts. A
// pattern that matches any of them may not type input.
var promptSamples = []string{
	"Do you want to proceed?",
	"Would you like to proceed?",
	"Do you want to make this edit to main.go?",
	"Do you want to create notes.md?",
	"Do you want to overwrite config.yaml?",
	"Do you want to delete this file?",
	"Do you want to allow Claude to fetch this content?",
	"Do you want to run this command?",
	"Do you want to continue? [Y/n]",
	"❯ 1. Yes",
	"1. Yes",
	"2. Yes, and don't ask again for git commands in this project",
	"2. Yes, allow all edits during this session (shift+tab)",
	"3. No, and tell Claude what to do differently (esc)",
	"This command requires approval",
	"This command requires your approval before it runs",
	"Only a person may approve this removal.",
	"Safety check: this removal requires confirmation",
	"Permission required",
	"Claude needs your permission to use Bash",
	"Allow this action?",
	"Allow execution of: 'rm'?",
	"Proceed? [y/N]",
	"Continue? (y/n)",
	"Overwrite existing file? [Y/n]",
	"Run shell command? (Y)es/(N)o [Yes]:",
	"Approve? (y/n/always)",
	"Are you sure you want to continue connecting (yes/no/[fingerprint])?",
	"Are you sure you want to do this?",
	"Enter to confirm · Esc to cancel",
	"Esc to cancel · Tab to amend",
	"Quick safety check: Is this a project you created or one you trust?",
	"Do you trust the files in this folder?",
	"❯ 1. Yes, I trust this folder",
	"WARNING: Claude Code running in Bypass Permissions mode",
	"❯ 2. Yes, I accept",
	"WARNING: Loading development channels",
	"❯ 1. I am using this for local development",
	"New MCP server found in .mcp.json",
	"2. Use this MCP server",
}

var patternSpace = regexp.MustCompile(`\\s[+*?]?|\.[+*]\??| +`)

// ValidateInputFilter checks a pattern and value for a filter that types
// input. The value must be one line of visible text: a bare Enter or a
// control key would accept whatever is preselected and could carry no mark.
func ValidateInputFilter(pattern, value string) error {
	refuse := func(why string) error { return fmt.Errorf("%w: %s", ErrInputFilterRefused, why) }
	if strings.TrimSpace(value) == "" {
		return refuse("the value must contain visible text; a bare Enter is not allowed")
	}
	if strings.ContainsFunc(value, isControlRune) {
		return refuse("the value must be one line without control characters")
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Errorf("invalid pattern regex: %w", err)
	}
	const matchesPrompt = "the pattern matches a permission, safety or confirmation prompt; a filter may never answer one"
	if re.MatchString("") {
		return refuse(matchesPrompt + " (it matches every line)")
	}
	for _, s := range promptSamples {
		for _, v := range []string{s, strings.ToLower(s), "│ " + s + " │"} {
			if re.MatchString(v) {
				return refuse(matchesPrompt)
			}
		}
	}
	// A pattern narrowed to one file or tool name still spells out the
	// prompt wording.
	plain := strings.ToLower(strings.ReplaceAll(patternSpace.ReplaceAllString(pattern, " "), `\`, ""))
	if containsAnyPhrase(plain, approvalPhrases...) || containsAnyPhrase(plain, activeChoiceDialogPhrases...) ||
		containsAnyPhrase(plain, "do you want to", "would you like to", "enter to confirm", "esc to cancel", "are you sure", "y/n", "yes/no") {
		return refuse(matchesPrompt)
	}
	return nil
}

// disabled-reason texts shown in every filter list.
const (
	filterReasonSeeded = "turned off on upgrade: a built-in filter may not type input"
	filterReasonUnsafe = "turned off: this filter could answer a permission or confirmation prompt"
)

// quarantineLocked turns off every stored filter that types input and must
// not: all seeded ones (once, flagged so the daemon also cancels what they
// had already queued) and any whose pattern or value breaks the rules.
// Reports whether anything changed.
func (fs *FilterStore) quarantineLocked() bool {
	changed := false
	for i := range fs.filters {
		f := &fs.filters[i]
		if !f.Action.TypesInput() {
			continue
		}
		switch {
		case f.Seeded:
			if f.Enabled || f.DisabledReason != filterReasonSeeded {
				if f.DisabledReason != filterReasonSeeded {
					f.PendingCleanup = true
				}
				f.Enabled, f.DisabledReason = false, filterReasonSeeded
				changed = true
			}
		case f.Enabled && ValidateInputFilter(f.Pattern, f.Value) != nil:
			f.Enabled, f.DisabledReason, f.PendingCleanup = false, filterReasonUnsafe, true
			changed = true
		}
	}
	return changed
}

// TakeUpgradeCleanup returns the filters turned off by an upgrade whose
// queued input has not been cancelled yet, and clears the flag.
func (fs *FilterStore) TakeUpgradeCleanup() ([]FilterPattern, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	var out []FilterPattern
	for i := range fs.filters {
		if fs.filters[i].PendingCleanup {
			out = append(out, fs.filters[i])
			fs.filters[i].PendingCleanup = false
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, fs.save()
}

// MayType reports why filter id may not type input, or nil when it may.
func (fs *FilterStore) MayType(id string) error {
	for _, f := range fs.List() {
		if f.ID != id {
			continue
		}
		switch {
		case !f.Action.TypesInput():
			return fmt.Errorf("%w: filter %s does not type input", ErrInputFilterRefused, id)
		case f.Seeded:
			return fmt.Errorf("%w: filter %s is built in", ErrInputFilterRefused, id)
		case !f.Enabled:
			return fmt.Errorf("%w: filter %s is turned off", ErrInputFilterRefused, id)
		}
		return ValidateInputFilter(f.Pattern, f.Value)
	}
	return fmt.Errorf("%w: filter %q not found", ErrInputFilterRefused, id)
}

// ── what filters had already queued ─────────────────────────────────────────

// CancelQueuedFilterInput cancels the pending on-input commands that filter f
// queued. Entries stored before queued commands carried a filter id are
// recognised by their shape (on-input, unnamed, same text as the filter).
// It returns the cancelled ids.
func (s *ScheduleStore) CancelQueuedFilterInput(f FilterPattern) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ids []string
	for _, sc := range s.entries {
		if sc.State != SchedPending || (sc.Type != SchedTypeCommand && sc.Type != "") {
			continue
		}
		legacy := sc.FilterID == "" && sc.RunAt.IsZero() && sc.RunAfterID == "" &&
			sc.ScheduleName == "" && sc.SessionName == "" && sc.CronExpr == "" && sc.Command == f.Value
		if sc.FilterID == f.ID || legacy {
			sc.State = SchedCancelled
			sc.DoneAt = time.Now()
			ids = append(ids, sc.ID)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	return ids, s.save()
}

// AddFilterInput queues text an output filter wants typed when the session
// next asks for input. The filter id stays with the entry so it is typed as
// the filter's, never as the operator's.
func (s *ScheduleStore) AddFilterInput(sessionID, filterID, command string) (*ScheduledCommand, error) {
	return s.AddFull(AddOptions{SessionID: sessionID, FilterID: filterID, Command: command})
}

// InputSource is the SendInput source for a queued command.
func (sc *ScheduledCommand) InputSource() string {
	if sc.FilterID != "" {
		return FilterSource(sc.FilterID)
	}
	return "schedule"
}

// FireScheduled types a due queued command into fullID and records the
// outcome. It is the one way a queued command is typed, and it claims the fire
// first, so one fire is typed at most once whichever passes see it: a pass that
// comes too late gets ErrFireTaken, and one that finds another schedule being
// typed into fullID gets ErrFireDeferred. A command held back by a prompt on
// screen stays pending; one whose filter may no longer type is cancelled; a
// name schedule whose session can no longer take input stays pending
// (ErrScheduleTargetGone); any other failure marks it failed. It reports
// whether the command was typed.
func (m *Manager) FireScheduled(store *ScheduleStore, fullID string, sc *ScheduledCommand) (bool, error) {
	claimed, release, err := store.claimFire(sc.ID, fullID, time.Now())
	if err != nil {
		return false, err
	}
	// Not deferred: after a panic it is unknown whether the text arrived, so
	// the claim stays and the next start settles it.
	sent, err := m.typeClaimed(store, fullID, &claimed)
	release()
	return sent, err
}

// typeClaimed is FireScheduled once the fire is claimed.
func (m *Manager) typeClaimed(store *ScheduleStore, fullID string, sc *ScheduledCommand) (bool, error) {
	err := m.SendInput(fullID, sc.Command, sc.InputSource())
	switch {
	case err == nil:
		return true, store.MarkDone(sc.ID, false)
	case errors.Is(err, ErrPromptOnScreen):
		return false, err
	case errors.Is(err, ErrFilterInputNotAllowed):
		_ = store.Cancel(sc.ID)
		return false, err
	case sc.NameTargeted() && m.targetGone(fullID):
		return false, fmt.Errorf("%w: %w", ErrScheduleTargetGone, err)
	default:
		_ = store.MarkDone(sc.ID, true)
		return false, err
	}
}
