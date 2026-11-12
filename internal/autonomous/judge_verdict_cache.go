package autonomous

// Per-commit judge verdict cache.
//
// A judge that COMPLETES a pass or block verdict on a given commit, under a
// given judge policy and given rules, has produced a judgment that running
// the same judge again against the same input will simply repeat (modulo
// LLM noise on the summary — the outcome for the same verdict inputs is the
// thing the daemon acts on). The cache replays that completed verdict
// instead of paying for another judge run.
//
// The identity of a cached verdict is the triple
// (commit sha, judge policy, rules hash). Commit sha anchors the judgment
// to the exact state of the code it judged; the judge policy key distinguishes
// which backends/policy produced it (a different policy list is a different
// judgment); the rules hash is a SHA-256 over the guardrail's judged inputs
// (guardrail name, prompt/spec/model), so a rules or prompt change is a
// cache miss.
//
// Only completed verdicts are ever stored: a pass, or a block whose category
// is a real finding (content, or the implicit pre-category value). A judge
// that timed out, crashed, or could not parse its own output (infra /
// invalid-response / judge-timeout categories, or a hard error) is NOT a
// completed judgment and is never cached — a later infra retry must still
// get its genuine chance, and a timeout must never be replayed as a verdict.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// JudgeVerdictCacheFileName is the persisted cache file, written inside the
// PRD Store's directory (<dataDir>/autonomous/) next to the PRD/Story/Task
// json-lines files — the same PRD store directory pattern as the repo CI
// watch and supervisor watch stores. Nothing outside the project's data
// directory is touched.
const JudgeVerdictCacheFileName = "judge_verdict_cache.json"

// JudgeVerdictCacheKey joins the three identity parts of one cached verdict:
// the commit sha the judgment covered, the policy key that ran it, and the
// rules hash of what was judged. Any empty part produces an empty key, which
// callers must treat as "not cacheable" (an uncommitted change has no sha
// to anchor the judgment to).
func JudgeVerdictCacheKey(commitSHA, policyKey, rulesHash string) string {
	if commitSHA == "" || policyKey == "" || rulesHash == "" {
		return ""
	}
	return strings.Join([]string{commitSHA, policyKey, rulesHash}, "\x1f")
}

// JudgeVerdictRulesHash is a stable hash of the judged inputs: the name of
// the guardrail whose rules ran, the exact prompt or spec text the judge
// saw, and the resolved judge model. Any change to any of them changes the
// hash, which is what makes a rules or prompt change a cache miss.
func JudgeVerdictRulesHash(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(h[:])
}

// JudgeVerdictCacheable reports whether a verdict is a completed judgment
// worth replaying from the cache: a pass, or a block whose category is a
// real finding (content, or the empty pre-BL410 value that means content).
// A block whose category says the judge never actually finished — infra
// (timeout/crash/no output), invalid-response (unparseable output) or the
// judge-timeout hold — is never cacheable, nor is any non-pass/non-block
// outcome.
func JudgeVerdictCacheable(v GuardrailVerdict) bool {
	switch v.Outcome {
	case "pass":
		return true
	case "block":
		switch v.Category {
		case "", "content":
			return true
		}
	}
	return false
}

// JudgeVerdictCacheEntry is one cached verdict plus the identity parts it
// was stored under, so the persisted file is self-describing.
type JudgeVerdictCacheEntry struct {
	CommitSHA string           `json:"commit_sha"`
	PolicyKey string           `json:"policy_key"`
	RulesHash string           `json:"rules_hash"`
	Verdict   GuardrailVerdict `json:"verdict"`
	StoredAt  time.Time        `json:"stored_at"`
}

// JudgeVerdictCache is the in-memory cache of completed judge verdicts,
// persisted as a single JSON object so the entries survive a daemon restart
// (same durability discipline as the PRD store: in memory, file under the
// data directory, no other storage).
type JudgeVerdictCache struct {
	mu      sync.Mutex
	path    string
	now     func() time.Time // injectable clock; tests override it
	entries map[string]JudgeVerdictCacheEntry
}

// NewJudgeVerdictCache opens (or creates) the cache under dir, which is the
// PRD Store's directory, and loads any persisted entries. A missing file on
// first open is a normal, empty cache, not an error.
func NewJudgeVerdictCache(dir string) (*JudgeVerdictCache, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("judge verdict cache: mkdir %s: %w", dir, err)
	}
	c := &JudgeVerdictCache{
		path:    filepath.Join(dir, JudgeVerdictCacheFileName),
		now:     time.Now,
		entries: map[string]JudgeVerdictCacheEntry{},
	}
	if err := c.load(); err != nil {
		return nil, err
	}
	return c, nil
}

// Path returns the absolute path of the cache file.
func (c *JudgeVerdictCache) Path() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.path
}

// Len returns the number of cached entries (debug/REST surface).
func (c *JudgeVerdictCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// Lookup returns the cached verdict for exactly (commitSHA, policyKey,
// rulesHash). Any empty part is a miss: the key identity is the triple.
func (c *JudgeVerdictCache) Lookup(commitSHA, policyKey, rulesHash string) (GuardrailVerdict, bool) {
	key := JudgeVerdictCacheKey(commitSHA, policyKey, rulesHash)
	if key == "" {
		return GuardrailVerdict{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return GuardrailVerdict{}, false
	}
	return e.Verdict, true
}

// Store records a verdict under the key. It is a silent no-op for an
// empty key or for a verdict JudgeVerdictCacheable rejects — which is how
// the "never cache a timed-out or otherwise unfinished judge" rule is
// enforced at the storage boundary, independent of the callers.
// A Store for an existing key updates the verdict and its stored-at time.
func (c *JudgeVerdictCache) Store(commitSHA, policyKey, rulesHash string, v GuardrailVerdict) error {
	key := JudgeVerdictCacheKey(commitSHA, policyKey, rulesHash)
	if key == "" || !JudgeVerdictCacheable(v) {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = JudgeVerdictCacheEntry{
		CommitSHA: commitSHA,
		PolicyKey: policyKey,
		RulesHash: rulesHash,
		Verdict:   v,
		StoredAt:  c.now(),
	}
	return c.persistLocked()
}

// load reads the persisted file (if any) into the in-memory map. Called
// from NewJudgeVerdictCache before the cache is shared.
func (c *JudgeVerdictCache) load() error {
	b, err := os.ReadFile(c.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("judge verdict cache: read %s: %w", c.path, err)
	}
	var entries map[string]JudgeVerdictCacheEntry
	if err := json.Unmarshal(b, &entries); err != nil {
		return fmt.Errorf("judge verdict cache: parse %s: %w", c.path, err)
	}
	if entries == nil {
		entries = map[string]JudgeVerdictCacheEntry{}
	}
	c.entries = entries
	return nil
}

// persistLocked writes the whole map to a temp file and renames it over
// the cache file (atomic on the daemon's filesystems), so a crash mid-write
// never leaves a half-written cache. The caller holds c.mu.
func (c *JudgeVerdictCache) persistLocked() error {
	b, err := json.MarshalIndent(c.entries, "", "  ")
	if err != nil {
		return fmt.Errorf("judge verdict cache: encode: %w", err)
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return fmt.Errorf("judge verdict cache: write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, c.path); err != nil {
		return fmt.Errorf("judge verdict cache: rename to %s: %w", c.path, err)
	}
	return nil
}

// --- manager wiring -------------------------------------------------------

// judgeVerdictCacheRef returns the manager's judge verdict cache, or nil
// when the manager was built without one (hand-constructed test managers).
func (m *Manager) judgeVerdictCacheRef() *JudgeVerdictCache {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.judgeVerdictCache
}

// judgeVerdictCacheEnabled resolves autonomous.judge_verdict_cache_enabled
// under the canonical normalizer: unset (nil) reads true, so the cache is
// on by default and only an explicit false turns it off. When the daemon
// wired a live accessor (SetJudgeVerdictCacheEnabled), that wins so a
// runtime config set is honored without a restart.
func (m *Manager) judgeVerdictCacheEnabled() bool {
	m.mu.Lock()
	fn := m.judgeVerdictCacheEnabledFn
	cfg := m.cfg
	m.mu.Unlock()
	if fn != nil {
		return fn()
	}
	return config.NormalizeJudgeVerdictCacheEnabled(cfg.JudgeVerdictCacheEnabled)
}

// judgeVerdictCommitSHA picks the commit sha the judgment is anchored to:
// the invocation's ReviewHead when it carries one, else the latest attempt commit recorded on the unit's own task (task-level
// judgments), else the latest across the unit story's tasks (story-level),
// else the latest recorded anywhere in the PRD (PRD-level). A PRD with no
// recorded attempt commit at all has no anchor, so the cache is bypassed
// both for lookup and store.
func judgeVerdictCommitSHA(prd *PRD, inv GuardrailInvocation) string {
	if inv.ReviewHead != "" {
		return inv.ReviewHead
	}
	if prd == nil {
		return ""
	}
	latestOf := func(ts []Task) string {
		var sha string
		for i := range ts {
			t := &ts[i]
			if len(t.AttemptCommitSHAs) == 0 {
				continue
			}
			sha = t.AttemptCommitSHAs[len(t.AttemptCommitSHAs)-1]
		}
		return sha
	}
	overall := ""
	for i := range prd.Story {
		s := &prd.Story[i]
		if sha := latestOf(s.Tasks); sha != "" {
			overall = sha
			if s.ID == inv.UnitID {
				return sha
			}
		}
	}
	// Task-level: a later story may own a unit with an older-looking order,
	// so a unit-id match anywhere wins over the walk order.
	for i := range prd.Story {
		s := &prd.Story[i]
		for j := range s.Tasks {
			t := &s.Tasks[j]
			if t.ID == inv.UnitID && len(t.AttemptCommitSHAs) > 0 {
				return t.AttemptCommitSHAs[len(t.AttemptCommitSHAs)-1]
			}
		}
	}
	return overall
}

// judgeVerdictPolicyKey is the "judge policy" part of the cache key: the
// pinned backend when the invocation pinned one (an explicit override is a
// different policy than the list it overrode), else the manager's
// configured judge backend list (empty list = the judge follows the
// verifier policy, which is a third, distinct key).
func judgeVerdictPolicyKey(m *Manager, inv GuardrailInvocation) string {
	if inv.GuardrailPinned && inv.GuardrailBackend != "" {
		return "pinned:" + inv.GuardrailBackend
	}
	m.mu.Lock()
	policy := append([]string(nil), m.cfg.JudgePolicy...)
	m.mu.Unlock()
	return "judge:" + strings.Join(policy, "|")
}

// judgeVerdictCacheHit returns the cached verdict for the invocation's
// (commit, judge policy, rules) triple when the cache is enabled and holds
// a completed verdict for exactly that triple. ok being false — regardless
// of the reason — means the caller must run the judge live.
func (m *Manager) judgeVerdictCacheHit(prd *PRD, inv GuardrailInvocation) (v GuardrailVerdict, ok bool) {
	c := m.judgeVerdictCacheRef()
	if c == nil || !m.judgeVerdictCacheEnabled() {
		return GuardrailVerdict{}, false
	}
	commit := judgeVerdictCommitSHA(prd, inv)
	if commit == "" {
		return GuardrailVerdict{}, false
	}
	return c.Lookup(commit, judgeVerdictPolicyKey(m, inv), judgeVerdictRulesHash(inv))
}

// judgeVerdictCacheRecord stores a completed pass/block verdict for the
// invocation's key. It is a no-op when the cache is disabled, the run
// returned an error, the verdict is not cacheable (timeout/infra/
// invalid-response/unparseable), or there is no commit sha to anchor — so
// the only verdicts that ever land in the cache are successful
// pass/block judgments on a real commit.
func (m *Manager) judgeVerdictCacheRecord(prd *PRD, inv GuardrailInvocation, v GuardrailVerdict, err error) {
	c := m.judgeVerdictCacheRef()
	if c == nil || !m.judgeVerdictCacheEnabled() || err != nil || !JudgeVerdictCacheable(v) {
		return
	}
	commit := judgeVerdictCommitSHA(prd, inv)
	if commit == "" {
		return
	}
	if serr := c.Store(commit, judgeVerdictPolicyKey(m, inv), judgeVerdictRulesHash(inv), v); serr != nil {
		log.Printf("judge verdict cache: store for %s/%s: %v", inv.PRDID, inv.UnitID, serr)
	}
}

// judgeVerdictCacheBypassKey is the context key marking a judge run whose
// cache lookup must be skipped (the stored verdict was judged on a
// different head than the PRD's current one). The store after the judge
// still happens, so the fresh verdict replaces the stale entry's role.
type judgeVerdictCacheBypassKey struct{}

// withJudgeVerdictCacheLookupBypassed marks ctx so runGuardrailWithInfraRetry
// runs the judge live instead of replaying a cached verdict.
func withJudgeVerdictCacheLookupBypassed(ctx context.Context) context.Context {
	return context.WithValue(ctx, judgeVerdictCacheBypassKey{}, true)
}

// judgeVerdictCacheLookupBypassed reports whether ctx asks for the cache
// lookup to be skipped.
func judgeVerdictCacheLookupBypassed(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	b, _ := ctx.Value(judgeVerdictCacheBypassKey{}).(bool)
	return b
}

// judgeVerdictRulesHash is the "rules" part of the key: a hash over the
// guardrail name and the exact inputs the judge saw for this unit, plus the
// rules fingerprint when the invocation carries one. An empty fingerprint adds
// no part, so per-task and per-story keys are the same as they were before the
// fingerprint existed.
func judgeVerdictRulesHash(inv GuardrailInvocation) string {
	parts := []string{inv.Guardrail, inv.Prompt, inv.UnitTitle, inv.UnitSpec, inv.GuardrailModel}
	if inv.RulesFingerprint != "" {
		parts = append(parts, inv.RulesFingerprint)
	}
	return JudgeVerdictRulesHash(parts...)
}
