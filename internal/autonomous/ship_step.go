// Ship step — runs when a release is finished. Per the project's ShipPolicy:
// push the branch to the configured remote, create an annotated tag named from
// tag_format and the release version in cmd/datawatch/main.go, then push the
// tag. off skips, shadow records the exact git commands without running them,
// on runs them. Never force-pushes; refuses a dirty working tree. Outcomes and
// failures are recorded on the Automaton's triage record.
package autonomous

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/git"
)

const shipTriageSource = "ship_step"

// shipTimeout bounds the whole ship step.
const shipTimeout = 2 * time.Minute

// ShipResult is what one ship step did.
type ShipResult struct {
	Branch   string
	Tag      string
	Version  string
	Commands []string // exact commands: planned (shadow) or run (on)
	Notes    []string
	Err      error
}

// RunShipStep executes the ship step for dir under policy p. branch may be ""
// (the checked-out branch is used). It never touches the triage record; see
// Manager.recordShipStep.
func RunShipStep(ctx context.Context, dir, branch string, p ShipPolicy) ShipResult {
	var r ShipResult
	pushMode, tagMode := p.Push, p.Tag
	if pushMode == config.ChecksModeOff && tagMode == config.ChecksModeOff {
		r.Notes = append(r.Notes, "push and tag are off: skipped")
		return r
	}
	if strings.TrimSpace(dir) == "" {
		r.Err = fmt.Errorf("ship: no project directory")
		return r
	}
	remote := p.Remote
	if remote == "" {
		remote = config.ShipDefaultRemote
	}
	record := func(args []string) { r.Commands = append(r.Commands, "git "+strings.Join(args, " ")) }

	if branch == "" {
		b, err := git.CurrentBranch(ctx, dir)
		if err != nil || b == "" {
			r.Err = fmt.Errorf("ship: cannot determine the branch to push (detached HEAD?)")
			return r
		}
		branch = b
	}
	r.Branch = branch

	var tag, message string
	if tagMode != config.ChecksModeOff {
		v, err := readVersionVar(filepath.Join(dir, "cmd/datawatch/main.go"))
		if err != nil {
			r.Err = fmt.Errorf("ship: read release version: %w", err)
			return r
		}
		r.Version = v
		if tag, err = git.FormatTag(p.TagFormat, v); err != nil {
			r.Err = fmt.Errorf("ship: %w", err)
			return r
		}
		r.Tag = tag
		message = "Release " + v
	}

	// Shadow only plans; it does not need a clean tree or touch git.
	if pushMode != config.ChecksModeOn && tagMode != config.ChecksModeOn {
		if pushMode == config.ChecksModeShadow {
			record(git.ShipPushBranchArgs(remote, branch))
		}
		if tagMode == config.ChecksModeShadow {
			record(git.TagCreateArgs(tag, message))
			record(git.PushTagArgs(remote, tag))
		}
		r.Notes = append(r.Notes, "shadow: commands recorded, not run")
		return r
	}

	clean, err := git.IsClean(ctx, dir)
	if err != nil {
		r.Err = fmt.Errorf("ship: check working tree: %w", err)
		return r
	}
	if !clean {
		r.Err = fmt.Errorf("ship: refusing to run, the working tree is dirty")
		return r
	}

	switch pushMode {
	case config.ChecksModeShadow:
		record(git.ShipPushBranchArgs(remote, branch))
	case config.ChecksModeOn:
		record(git.ShipPushBranchArgs(remote, branch))
		if err := git.PushBranchRef(ctx, dir, remote, branch); err != nil {
			r.Err = fmt.Errorf("ship: push branch: %w", err)
			return r
		}
	}

	switch tagMode {
	case config.ChecksModeShadow:
		record(git.TagCreateArgs(tag, message))
		record(git.PushTagArgs(remote, tag))
	case config.ChecksModeOn:
		head, err := git.HeadCommit(ctx, dir)
		if err != nil {
			r.Err = fmt.Errorf("ship: resolve HEAD: %w", err)
			return r
		}
		state, at, err := git.TagStatus(ctx, dir, tag, head)
		if err != nil {
			r.Err = fmt.Errorf("ship: inspect tag: %w", err)
			return r
		}
		switch state {
		case git.TagOtherCommit:
			r.Err = fmt.Errorf("ship: tag %s already exists on a different commit (%s, HEAD is %s)", tag, at, head)
			return r
		case git.TagSameCommit:
			r.Notes = append(r.Notes, fmt.Sprintf("tag %s already exists on %s: not recreated", tag, head))
		default:
			record(git.TagCreateArgs(tag, message))
			if err := git.CreateAnnotatedTag(ctx, dir, tag, message); err != nil {
				r.Err = fmt.Errorf("ship: create tag: %w", err)
				return r
			}
		}
		record(git.PushTagArgs(remote, tag))
		if err := git.PushTag(ctx, dir, remote, tag); err != nil {
			r.Err = fmt.Errorf("ship: push tag: %w", err)
			return r
		}
	}
	return r
}

// runShipStep is the completion-path hook: read the policy for the PRD's
// project directory, run the step, record the outcome on the triage record.
func (m *Manager) runShipStep(prd *PRD) *ShipResult {
	if prd == nil || strings.TrimSpace(prd.ProjectDir) == "" {
		return nil
	}
	p := ShipPolicyFor(prd.ProjectDir)
	if p.Push == config.ChecksModeOff && p.Tag == config.ChecksModeOff {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), shipTimeout)
	defer cancel()
	r := RunShipStep(ctx, prd.ProjectDir, prd.Git.Branch, p)
	m.recordShipStep(prd, p, r)
	return &r
}

func (m *Manager) recordShipStep(prd *PRD, p ShipPolicy, r ShipResult) {
	lines := []string{}
	if r.Tag != "" {
		lines = append(lines, "tag "+r.Tag)
	}
	lines = append(lines, r.Commands...)
	lines = append(lines, r.Notes...)
	mode := p.Push
	if mode == config.ChecksModeOff {
		mode = p.Tag
	}
	if p.Push == config.ChecksModeOn || p.Tag == config.ChecksModeOn {
		mode = config.ChecksModeOn
	}
	label := "ship step"
	if r.Err != nil {
		log.Printf("[autonomous] prd=%s ship step failed: %v", prd.ID, r.Err)
		lines = append(lines, "FAILED: "+r.Err.Error())
		label = "ship step failed"
	}
	m.recordCheckFindings(prd, mode, shipTriageSource, label, lines, r.Err != nil)
}
