package autonomous

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// TagSweepSessionSource is the minimal session-manager surface the
// production tag-sweep store needs: a flat listing of the sessions held
// (live or finished states are both stored sessions; "live or archived")
// and the system-tag writer. *session.Manager implements both; tests use a
// fake so the sweep is exercised without a running session manager.
type TagSweepSessionSource interface {
	// ListSessions returns every session the manager holds.
	ListSessions() []*session.Session
	// SetSystemTags replaces one session's derived system tags.
	SetSystemTags(sessionID string, tags []string) error
}

// TagSweepArchivedLister is the optional source surface that also lists the
// manager's archived sessions: the sweep's session pass covers live and
// archived alike, and a session manager hands its archived set out through
// this signature (*session.Manager implements it). A source without it is
// listed live only.
type TagSweepArchivedLister interface {
	ListArchivedSessions(f session.SessionFilter, extra func(*session.Session) bool, text string, before time.Time, limit int) []*session.Session
}

// TagSweepArchivedWriter is the optional write surface that reaches a
// session whether it is still active or already in the archive — the sweep
// re-derives the tags of every Automata-owned session of either and must be
// able to write either (*session.Manager implements it: SetSystemTagsAny).
// A source without it is written through SetSystemTags, its live-only path.
type TagSweepArchivedWriter interface {
	SetSystemTagsAny(sessionID string, tags []string) error
}

// SessionTagSweepStore is the TagSweepSessionStore the daemon wires for the
// tag sweep (wireCapacity after the placement-tag wiring): it lists every
// Automata-owned session — one that belongs to an Automaton (PRD) or that
// the manager already gave system tags — and rebuilds each one's placement
// from its own stored facts (its node, backend, model, project; the
// Automaton's ID, Automation and stage) plus the role and kind read from
// its name. The session side of Placement always carries session facts only
// — a session holds no lease, so no task or capacity facts — which is
// exactly the "rebuild Placement from the stored facts" the sweep needs.
// The operator tags (Session.Tags) never enter this struct; they are a
// separate list the sweep can neither see nor write.
type SessionTagSweepStore struct {
	// m is the autonomous Manager: the source of the Automaton and its
	// Automation and stage for a session's PRD. Nil (a partially wired
	// Manager) leaves the Automaton axis facts empty.
	m *Manager
	// src is the session manager (or a test fake) the store lists through
	// and writes each session's re-derived system tags into.
	src TagSweepSessionSource
}

// NewSessionTagSweepStore returns the production tag-sweep session store
// over the session source. am may be nil (the Automaton's automation and
// stage facts are then simply absent); src must not be nil.
func NewSessionTagSweepStore(am *Manager, src TagSweepSessionSource) *SessionTagSweepStore {
	return &SessionTagSweepStore{m: am, src: src}
}

// ListTagSweepSessions returns every Automata-owned session — live or
// archived — as the sweep reads it, each with its current system tags and
// the placement rebuilt from the stored facts. Sessions outside the
// Automata — no PRD and no system tags of their own — are not the sweep's
// to touch and are left out.
func (st *SessionTagSweepStore) ListTagSweepSessions() []TagSweepSession {
	if st.src == nil {
		return []TagSweepSession{}
	}
	out := []TagSweepSession{}
	seen := map[string]bool{}
	add := func(s *session.Session) {
		if s == nil || s.ID == "" || seen[s.ID] || !sweepOwnsSession(s.PRDID, s.SystemTags) {
			return
		}
		seen[s.ID] = true
		out = append(out, TagSweepSession{
			ID:         s.ID,
			SystemTags: s.SystemTags,
			Placement:  st.rebuildPlacement(s),
		})
	}
	for _, s := range st.src.ListSessions() {
		add(s)
	}
	// Archived sessions are the sweep's too: re-deriving is exactly what
	// "the current settings, applied without a fresh launch" means for a
	// session that finished before the settings settled. A source without
	// the archived lister contributes live sessions only.
	if arc, ok := interface{}(st.src).(TagSweepArchivedLister); ok {
		for _, s := range arc.ListArchivedSessions(session.SessionFilter{}, nil, "", time.Time{}, 0) {
			add(s)
		}
	}
	return out
}

// SetSystemTags writes one listed session's re-derived system tags back
// through the session manager. The write reaches whichever of the active
// store and the archive holds the session (a source that implements the
// any-writer, which *session.Manager does); the operator tags are a separate
// list and are not touched on either path.
func (st *SessionTagSweepStore) SetSystemTags(sessionID string, tags []string) error {
	if st.src == nil {
		return nil
	}
	if w, ok := interface{}(st.src).(TagSweepArchivedWriter); ok {
		return w.SetSystemTagsAny(sessionID, tags)
	}
	return st.src.SetSystemTags(sessionID, tags)
}

// sweepOwnsSession reports whether a session is Automata-owned for the tag
// sweep: it belongs to an Automaton (its session records a PRD ID) or the
// manager already gave it system tags (a launched placement session). A
// session with neither — an ordinary operator session — is not the sweep's
// to re-derive and must not be touched by it.
func sweepOwnsSession(prdID string, systemTags []string) bool {
	return prdID != "" || len(systemTags) > 0
}

// sweepLocalNode is the node name a launch-fact session with no stored compute
// node is placed on: the daemon's own node, the name local work carries
// everywhere else in the placement vocabulary.
const sweepLocalNode = "local"

// rebuildPlacement rebuilds one session's Placement from its stored facts. A
// session that recorded its launch facts (LaunchRole, LaunchKind or
// LaunchStage) gets the full placement through SessionPlacement: the role,
// kind and stage it was launched with, and the local node when it stored
// none. A session with no recorded launch facts is an existing session, and
// it keeps exactly the derivation the sweep always gave it (legacyPlacement),
// so no existing session's tags change.
func (st *SessionTagSweepStore) rebuildPlacement(s *session.Session) Placement {
	if s.LaunchRole == "" && s.LaunchKind == "" && s.LaunchStage == "" {
		return st.legacyPlacement(s)
	}
	return SessionPlacement(s, sweepLocalNode, st.automationOf)
}

// automationOf is the Automation lookup SessionPlacement takes: the
// Automation a PRD is a member of, through the same AutomationForPRD lookup the
// rest of the manager uses. A nil manager, an unknown PRD or a PRD in no
// Automation gives "".
func (st *SessionTagSweepStore) automationOf(prdID string) string {
	if st.m == nil {
		return ""
	}
	prd, ok := st.m.Store().GetPRD(prdID)
	if !ok || prd == nil {
		return ""
	}
	if a := st.m.AutomationForPRD(prd); a != nil {
		return a.ID
	}
	return ""
}

// legacyPlacement is the derivation a session with no recorded launch facts
// has always been given. It reads its role and kind from its name, takes no
// local-node fallback, and reads its stage from its Automaton's membership.
// Its Automaton, Automation and stage come from the stored PRD, as before.
// No lease, task or capacity fact is mixed in: a session holds none.
func (st *SessionTagSweepStore) legacyPlacement(s *session.Session) Placement {
	role, kind := sessionNameRoleKind(s.Name)
	p := Placement{
		Node:    strings.TrimSpace(s.ComputeNodeRef),
		Backend: s.BackendFamily,
		Model:   s.Model,
		Lane:    laneForBackend(s.BackendFamily),
		Role:    role,
		Kind:    kind,
	}
	if d := strings.TrimSpace(s.ProjectDir); d != "" {
		p.Project = filepath.Base(d)
	}
	if st.m == nil {
		return p
	}
	prdID := strings.TrimSpace(s.PRDID)
	if prdID == "" {
		return p
	}
	p.Automaton = prdID
	prd, ok := st.m.Store().GetPRD(prdID)
	if ok && prd != nil {
		// The PRD's own AutomationID when set, else the owner map the
		// Automation store keeps per member PRD — same lookup the rest of the
		// manager uses, so a PRD the operator added to an Automation after
		// the session started still resolves to its automation and stage.
		if a := st.m.AutomationForPRD(prd); a != nil {
			p.Automation = a.ID
			p.Stage = st.m.StageOfPRD(prd)
		}
	}
	return p
}

// sessionNameRoleKind reads the launch role and kind a session's name
// carries, in the known launch vocabulary (see the server-side launchRoles
// and launchKinds) — the same words the launch surface validates. The
// names the autonomous machinery gives its non-task sessions carry their
// role as the first word after the workerSessionNamePrefix; a task worker's
// name carries no such word and is the default case. A name that names
// neither a known role nor kind (an ordinary session, a non-worker role the
// launch vocabulary does not know) gives back the empty facts and takes no
// role or kind tag — the sweep never guesses one where the name does not
// carry one.
func sessionNameRoleKind(name string) (role, kind string) {
	rest := strings.TrimSpace(name)
	if !strings.HasPrefix(rest, workerSessionNamePrefix) {
		return "", ""
	}
	rest = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(rest, workerSessionNamePrefix)))
	switch {
	case strings.HasPrefix(rest, "verify") || strings.HasPrefix(rest, "review"):
		return "verifier", "review"
	case strings.HasPrefix(rest, "decompose") || strings.HasPrefix(rest, "plan"):
		return "planner", "decompose"
	case strings.HasPrefix(rest, "rejudge") || strings.HasPrefix(rest, "judge"):
		return "judge", "rejudge"
	case strings.HasPrefix(rest, "auto") && strings.Contains(rest, "fix"):
		return "worker", autoFixTaskKind
	case strings.HasPrefix(rest, "supervisor"):
		return "supervisor", ""
	// The non-task roles the automaton machinery itself runs that are not in
	// the launch role vocabulary (see nonWorkerRolePrefixes) carry neither a
	// role nor a kind — the sweep does not guess one for them.
	case strings.HasPrefix(rest, "guardrail") || strings.HasPrefix(rest, "council") || strings.HasPrefix(rest, "eval"):
		return "", ""
	default:
		return "worker", "task"
	}
}
