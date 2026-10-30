package server

// overlap_create.go runs the search-before-create check for the create
// route. The check runs after the Automaton is stored and only reads: it
// never changes the stored record. Mode off runs nothing; shadow writes one
// audit entry and leaves the reply alone; on adds an overlap_warnings array
// to that one reply. A search that fails or panics is logged, and the create
// still succeeds with the reply mode off gives.

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// overlapCreateSearch searches for the overlaps of one stored Automaton at
// the given threshold. The search excludes p's own id and reads p's title,
// spec and project directory. A non-nil error is logged and the create goes
// on without the check.
type overlapCreateSearch func(p *autonomous.PRD, threshold float64) ([]autonomous.OverlapWarning, error)

// createSearchSettings returns the search-before-create settings in effect.
// A server without a config reads the same defaults an unset config does.
func (s *Server) createSearchSettings() config.AutonomousAutomationConfig {
	if s.cfg == nil {
		return config.AutonomousAutomationConfig{}
	}
	return s.cfg.Autonomous.Automation
}

// createSearchAfterStore runs the check for the Automaton the create route
// has just stored. In shadow mode it writes the audit entry and returns nil.
// In on mode it returns the warnings to add to the reply, empty but non-nil
// when there are none. It returns nil in every other case, and whenever the
// search fails, so the caller writes the reply mode off gives.
func (s *Server) createSearchAfterStore(r *http.Request, prd any, prdID string) []autonomous.OverlapWarning {
	settings := s.createSearchSettings()
	mode := settings.EffectiveCreateSearchMode()
	if mode != config.CreateSearchModeShadow && mode != config.CreateSearchModeOn {
		return nil
	}
	p, err := toPRD(prd)
	if err != nil {
		log.Printf("[overlap] create search for %s skipped: %v", prdID, err)
		return nil
	}
	warnings, ok := s.runCreateSearch(p, settings.EffectiveCreateSearchThreshold())
	if !ok {
		return nil
	}
	if mode == config.CreateSearchModeShadow {
		autonomous.RecordCreateSearchShadow(s.createSearchAuditSink(), s.auditActor(r.Context()), p.ID, mode, warnings)
		return nil
	}
	return warnings
}

// runCreateSearch runs the search and reports whether it finished. An error
// or a panic is logged and reports false, so a broken search never fails the
// create that triggered it.
func (s *Server) runCreateSearch(p *autonomous.PRD, threshold float64) (warnings []autonomous.OverlapWarning, ok bool) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("[overlap] create search for %s panicked: %v", p.ID, rec)
			warnings, ok = nil, false
		}
	}()
	search := s.overlapSearch
	if search == nil {
		search = s.defaultOverlapSearch
	}
	var err error
	warnings, err = search(p, threshold)
	if err != nil {
		log.Printf("[overlap] create search for %s failed: %v", p.ID, err)
		return nil, false
	}
	if warnings == nil {
		warnings = []autonomous.OverlapWarning{}
	}
	return warnings, true
}

// defaultOverlapSearch runs the autonomous finder over every Automaton the
// manager holds, with the worktree base repository as the project root when
// the Automaton names no project directory.
func (s *Server) defaultOverlapSearch(p *autonomous.PRD, threshold float64) ([]autonomous.OverlapWarning, error) {
	base := ""
	if s.cfg != nil {
		base = s.cfg.Autonomous.WorktreeBaseRepo
	}
	finder := autonomous.NewCreateSearchFinder(apiAutomatonLister{api: s.autonomousMgr}, threshold, base)
	warnings, _ := finder.Find(p.Title, p.Spec, p.ID, p.ProjectDir)
	return warnings, nil
}

// createSearchAuditSink returns the sink the shadow entry is written to: the
// operator audit log, the same writer every other audit entry uses. It is
// nil when the audit log is not wired, which drops the entry.
func (s *Server) createSearchAuditSink() func(audit.Entry) {
	if s.auditLog == nil {
		return nil
	}
	return func(e audit.Entry) {
		if err := s.auditLog.Write(e); err != nil {
			log.Printf("[overlap] create search audit entry not written: %v", err)
		}
	}
}

// apiAutomatonLister lists every Automaton of every status through the
// AutonomousAPI the REST handlers already hold.
type apiAutomatonLister struct {
	api AutonomousAPI
}

// ListAutomata returns each Automaton the manager lists, whatever its status.
func (l apiAutomatonLister) ListAutomata() []autonomous.OverlapAutomaton {
	prds := l.api.ListPRDs()
	out := make([]autonomous.OverlapAutomaton, 0, len(prds))
	for _, v := range prds {
		p, err := toPRD(v)
		if err != nil || p.ID == "" {
			continue
		}
		out = append(out, autonomous.OverlapAutomaton{
			ID:     p.ID,
			Title:  p.Title,
			Spec:   p.Spec,
			Status: string(p.Status),
		})
	}
	return out
}

// writeCreateReplyWithWarnings writes the create reply with a top-level
// overlap_warnings array. The reply is built by writeAutonomousResult into a
// buffer first, so the view rules and every other field stay as they are,
// and only the one key is added.
func (s *Server) writeCreateReplyWithWarnings(w http.ResponseWriter, r *http.Request, prd any, warnings []autonomous.OverlapWarning) {
	buf := &bufferedReply{header: http.Header{}}
	s.writeAutonomousResult(buf, r, prd, "", "", "")

	var fields map[string]json.RawMessage
	if (buf.status != 0 && buf.status != http.StatusOK) || json.Unmarshal(buf.body.Bytes(), &fields) != nil || fields == nil {
		buf.writeTo(w)
		return
	}
	list, err := json.Marshal(warnings)
	if err != nil {
		http.Error(w, "overlap warnings: "+err.Error(), http.StatusInternalServerError)
		return
	}
	fields["overlap_warnings"] = list
	out, err := json.Marshal(fields)
	if err != nil {
		http.Error(w, "overlap warnings: "+err.Error(), http.StatusInternalServerError)
		return
	}
	buf.body.Reset()
	buf.body.Write(append(out, '\n'))
	buf.header.Del("Content-Length")
	buf.writeTo(w)
}

// bufferedReply is an http.ResponseWriter that keeps the status, the headers
// and the body of a reply so the body can be changed before it is sent.
type bufferedReply struct {
	header http.Header
	status int
	body   bytes.Buffer
}

// Header returns the headers the reply will be sent with.
func (b *bufferedReply) Header() http.Header { return b.header }

// WriteHeader records the first status written; later calls are ignored, as
// they are on a real response.
func (b *bufferedReply) WriteHeader(code int) {
	if b.status == 0 {
		b.status = code
	}
}

// Write appends to the body.
func (b *bufferedReply) Write(p []byte) (int, error) { return b.body.Write(p) }

// writeTo sends the buffered status, headers and body to w.
func (b *bufferedReply) writeTo(w http.ResponseWriter) {
	for k, v := range b.header {
		w.Header()[k] = v
	}
	if b.status != 0 {
		w.WriteHeader(b.status)
	}
	_, _ = w.Write(b.body.Bytes())
}
