// The shadow of a hand merge, run by the daemon. Recording an Automaton's
// shipped state with source merge_step queues one: the daemon builds the same
// merge with the same engine as its own merge runs, in shadow mode, in a
// disposable copy of the project under the data directory. It runs whether or
// not merge runs are switched on, since it changes and sends nothing.

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/mergepipeline"
	"github.com/dmz006/datawatch/internal/mergeshadow"
	"github.com/dmz006/datawatch/internal/proclimit"
)

// mergeShadowSource is the Manager surface the shadow of a hand merge and the
// turn-on report use.
type mergeShadowSource interface {
	mergeRunner
	MergeShadowSubject(ctx context.Context, id string) (autonomous.ShadowSubject, bool)
	MergeEligibilityForHandMerge(ctx context.Context, id, tip string, mergedAt time.Time) autonomous.MergeSelectionEntry
	MergeSelection(ctx context.Context, id string) autonomous.MergeSelection
}

var _ mergeShadowSource = (*autonomous.API)(nil)

// handShadows are the queued and running shadows of this daemon. One runs at a
// time: each runs a project's whole checks.
var handShadows struct {
	one    sync.Mutex
	wg     sync.WaitGroup
	mu     sync.Mutex
	active map[string]bool
}

// waitHandShadows waits for every queued shadow of a hand merge.
func waitHandShadows() { handShadows.wg.Wait() }

func shadowActive(dataDir, key string) bool {
	handShadows.mu.Lock()
	defer handShadows.mu.Unlock()
	return handShadows.active[dataDir+"\x00"+key]
}

// claimShadow marks the shadow as this daemon's, and reports false when it
// already is.
func claimShadow(dataDir, key string) bool {
	handShadows.mu.Lock()
	defer handShadows.mu.Unlock()
	if handShadows.active == nil {
		handShadows.active = map[string]bool{}
	}
	if handShadows.active[dataDir+"\x00"+key] {
		return false
	}
	handShadows.active[dataDir+"\x00"+key] = true
	return true
}

func releaseShadow(dataDir, key string) {
	handShadows.mu.Lock()
	defer handShadows.mu.Unlock()
	delete(handShadows.active, dataDir+"\x00"+key)
}

const shadowInterrupted = "the shadow was interrupted: the daemon stopped before it finished. Record the shipped state again to run it again"

// listHandShadows reads the stored shadows. One still marked queued or running
// that this daemon is not running was cut off.
func listHandShadows(dataDir string) ([]*mergeshadow.Replay, error) {
	all, err := mergeshadow.ListReplays(dataDir)
	if err != nil {
		return nil, err
	}
	for _, r := range all {
		if (r.State == mergeshadow.ReplayQueued || r.State == mergeshadow.ReplayRunning) && !shadowActive(dataDir, r.Key) {
			r.State, r.Error = mergeshadow.ReplayError, shadowInterrupted
		}
	}
	return all, nil
}

// handShadowRoot holds the disposable copies, under the data directory.
func handShadowRoot(dataDir string) string { return filepath.Join(dataDir, "merge-pipeline", "shadow") }

// handShadowLimit replaces the limit a hand-merge shadow's commands run under.
// It is nil, meaning this system's own, except where a test sets it.
var handShadowLimit proclimit.Starter

// shadowCommandLimit is the limit a shadow's commands run under.
func shadowCommandLimit() proclimit.Starter {
	if handShadowLimit != nil {
		return handShadowLimit
	}
	return proclimit.Host()
}

// repoCommit reads a commit of the repository at repo: its full id and commit
// time.
func repoCommit(ctx context.Context, repo, rev string) (string, time.Time, bool) {
	if rev == "" || strings.HasPrefix(rev, "-") {
		return "", time.Time{}, false
	}
	cmd := exec.CommandContext(ctx, "git", "log", "-1", "--format=%H %cI", "--end-of-options", rev+"^{commit}", "--") // #nosec G204 -- fixed binary, rev cannot be an option
	cmd.Dir = repo
	var out bytes.Buffer
	cmd.Stdout = &out
	// This reads one commit as the shadow is queued. Where there is no limit
	// to be had it is read all the same, so that the shadow is stored and
	// says why it could not run.
	if l := shadowCommandLimit(); l.Available(ctx) == nil {
		ctx = proclimit.With(ctx, l, nil)
	}
	if err := proclimit.Run(ctx, cmd); err != nil {
		return "", time.Time{}, false
	}
	id, when, ok := strings.Cut(strings.TrimSpace(out.String()), " ")
	at, terr := time.Parse(time.RFC3339, when)
	return id, at, ok && terr == nil
}

// startHandShadow queues the shadow of the hand merge that was just recorded
// for Automaton id, and keeps the hand merge's check results when given. A
// finished shadow is never run again; one that could not run, or was cut off,
// is.
func (s *Server) startHandShadow(id, release, recorded string, checks map[string]string) {
	if s.cfg == nil || !s.cfg.Autonomous.MergeShadow.EffectiveEnabled() {
		return
	}
	src, ok := s.autonomousMgr.(mergeShadowSource)
	if !ok {
		return
	}
	repo := src.MergeBaseRepo()
	if repo == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	subject, found := src.MergeShadowSubject(ctx, id)
	if !found {
		return
	}
	commit, committed, here := repoCommit(ctx, repo, recorded)
	if !here {
		if subject.BranchHead == "" {
			return // work of another repository: not a merge of this one
		}
		commit, committed = recorded, time.Now().UTC()
	}
	dataDir := s.statePath()
	key := mergeshadow.ReplayKey(id, commit)
	if len(checks) > 0 {
		if err := mergeshadow.SaveHandChecks(dataDir, key, checks); err != nil {
			log.Printf("[merge-shadow] Automaton %s: the hand check results were not stored: %v", id, err)
		}
	}
	rec := mergeshadow.Replay{
		Key: key, AutomatonID: id, Name: subject.Name, Kind: subject.Kind, Project: filepath.Base(repo),
		Branch: subject.Branch, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(), State: mergeshadow.ReplayQueued,
		Hand:   mergeshadow.HandMerge{Commit: commit, Release: release, At: committed.UTC(), Conflicts: []string{}},
		Daemon: mergeshadow.DaemonMerge{Checks: []mergeshadow.ReplayCheck{}, Conflicts: []string{}},
	}
	var selected *mergeshadow.ReplaySelection
	old, err := mergeshadow.LoadReplay(dataDir, key)
	switch {
	case err == nil:
		if old.State == mergeshadow.ReplayDone || shadowActive(dataDir, key) {
			return
		}
		// A rerun keeps the first run's place and selection answer.
		rec.CreatedAt, selected = old.CreatedAt, old.Selection
	case !errors.Is(err, os.ErrNotExist):
		log.Printf("[merge-shadow] Automaton %s: read the stored shadow: %v", id, err)
	}
	if !claimShadow(dataDir, key) {
		return
	}
	if err := mergeshadow.SaveReplay(dataDir, &rec); err != nil {
		releaseShadow(dataDir, key)
		log.Printf("[merge-shadow] Automaton %s: the shadow was not queued: %v", id, err)
		return
	}
	cfg := s.mergePipelineConfig()
	handShadows.wg.Add(1)
	go func() {
		defer handShadows.wg.Done()
		defer releaseShadow(dataDir, key)
		handShadows.one.Lock()
		defer handShadows.one.Unlock()
		runHandShadow(src, cfg, repo, dataDir, rec, subject, selected)
	}()
}

// runHandShadow runs one queued shadow and stores what it saw. The caller
// holds the one-at-a-time lock.
func runHandShadow(src mergeShadowSource, cfg config.MergePipelineConfig, repo, dataDir string, rec mergeshadow.Replay, subject autonomous.ShadowSubject, selected *mergeshadow.ReplaySelection) {
	// One shadow at a time, of either kind: this one stays queued until it
	// is its turn, and its time starts then.
	turn, release, err := mergeshadow.AcquireSlot(context.Background())
	if err != nil {
		log.Printf("[merge-shadow] Automaton %s: %v", rec.AutomatonID, err)
		return
	}
	defer release()
	ctx, cancel := context.WithTimeout(turn, mergeRunTimeout)
	defer cancel()
	save := func(r *mergeshadow.Replay) {
		r.UpdatedAt = time.Now().UTC()
		if err := mergeshadow.SaveReplay(dataDir, r); err != nil {
			log.Printf("[merge-shadow] Automaton %s: store the shadow: %v", r.AutomatonID, err)
		}
	}
	rec.State = mergeshadow.ReplayRunning
	save(&rec)
	// What an earlier shadow left behind when the daemon stopped.
	root := handShadowRoot(dataDir)
	if err := os.RemoveAll(root); err != nil {
		log.Printf("[merge-shadow] clear %s: %v", root, err)
	}
	if subject.BranchHead == "" {
		rec.State, rec.Error = mergeshadow.ReplayError, "branch "+subject.Branch+" is not in the merge repository, so the merge cannot be rebuilt"
		rec.Selection = selected
		save(&rec)
		return
	}
	title := rec.AutomatonID
	if facts, ok := src.(mergeReleaseSource); ok {
		if f, found := facts.MergeReleaseFacts(rec.AutomatonID, nil, ""); found && f.Title != "" {
			title = f.Title
		}
	}
	settings, err := json.Marshal(cfg)
	if err != nil {
		rec.State, rec.Error = mergeshadow.ReplayError, "read the merge settings: "+err.Error()
		save(&rec)
		return
	}
	mergedAt := rec.Hand.At
	out := mergepipeline.ShadowHandMerge(ctx, mergepipeline.ReplayInput{
		Replay: rec, Repo: repo, WorkDir: filepath.Join(root, rec.Key), BranchHead: subject.BranchHead,
		Title: title, Settings: settings, Limit: shadowCommandLimit(),
		Select: func(c context.Context, tip string) *mergeshadow.ReplaySelection {
			if selected != nil && selected.Tip == tip {
				return selected
			}
			e := src.MergeEligibilityForHandMerge(c, rec.AutomatonID, tip, mergedAt)
			return &mergeshadow.ReplaySelection{Eligible: e.Eligible, Basis: e.Basis, Tip: tip, Reasons: e.Reasons}
		},
		// The runner a real run gets, over the copy.
		Runner: func(copyDir, scratch string) *mergepipeline.Runner {
			r := newMergeRunner(src, cfg, copyDir, rec.Project, dataDir, nil)
			r.ScratchRoot = scratch
			return r
		},
	})
	save(out)
	log.Printf("[merge-shadow] Automaton %s, hand merge %.12s: %s", out.AutomatonID, out.Hand.Commit, out.Verdict())
}
