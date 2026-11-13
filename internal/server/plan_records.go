// plan_records.go — the plan record routes over REST.
//
// A plan record is a registry row (a numbered item of the plans registry) or a
// finding, kept by the daemon in internal/planrecords. The routes:
//
//	GET  /api/plan-records/registry-rows        — list (autonomous:read)
//	POST /api/plan-records/registry-rows        — create (autonomous:write)
//	GET  /api/plan-records/registry-rows/{ref}  — one record (autonomous:read)
//	PUT  /api/plan-records/registry-rows/{ref}  — change a record (autonomous:write)
//	GET  /api/plan-records/findings             — list (autonomous:read)
//	POST /api/plan-records/findings             — create (autonomous:write)
//	GET  /api/plan-records/findings/{ref}       — one record (autonomous:read)
//	PUT  /api/plan-records/findings/{ref}       — change a record (autonomous:write)
//	GET  /api/plan-records/search?q=            — search both kinds (autonomous:read)
//	GET  /api/plan-records/browse               — rows and findings with plan and Automaton links, paged (autonomous:read)
//	POST /api/plan-records/import[?dry_run=true] — import the archive (autonomous:write)
//
// There is no delete route: a record is closed by changing its state.
//
// {ref} is an id, a uid, "row-<n>" (the old row number), an exact title, an old
// title or an unambiguous title prefix. A title that matches more than one
// record answers 409 with the candidates so the caller can say which one.
//
// autonomous.plan_records.enabled false answers every route with 404. A
// request only reads and writes record files (and, for an import, reads the
// project's archive files); it never starts a process.

package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/planrecords"
)

// maxPlanRecordBodyBytes caps a request body: a record body is capped at
// 100 KiB by the store, so the request cap leaves room for the other fields.
const maxPlanRecordBodyBytes = 256 << 10

const planRecordsPrefix = "/api/plan-records"

// PlanRecordsAuditFunc returns the hook the plan-records store calls once per
// write. It writes one entry to the operator audit log; a nil log gives a nil
// hook (no audit). The actor travels in the details the store builds.
func PlanRecordsAuditFunc(l *audit.Log) planrecords.AuditFunc {
	if l == nil {
		return nil
	}
	return func(action, subject string, details map[string]any) {
		actor, _ := details["actor"].(string)
		if actor == "" {
			actor = "system"
		}
		name := audit.ActionPlanRecordUpdated
		switch action {
		case planrecords.AuditCreated:
			name = audit.ActionPlanRecordCreated
		case planrecords.AuditMigrated:
			name = audit.ActionPlanRecordImported
		}
		_ = l.Write(audit.Entry{Actor: actor, Action: name, Subject: subject, Details: details})
	}
}

// SetPlanRecords wires the plan-records store into the server. Nil (the
// default) leaves the routes answering 503.
func (s *Server) SetPlanRecords(st *planrecords.Store) {
	if s == nil {
		return
	}
	s.planRecords = st
}

// SetPlanRecords is the HTTPServer delegate for main.go.
func (s *HTTPServer) SetPlanRecords(st *planrecords.Store) {
	if s == nil || s.api == nil {
		return
	}
	s.api.SetPlanRecords(st)
}

// planRecordView is a record on the wire. The title comes first and the id
// second on every response; the field order below is the JSON order.
type planRecordView struct {
	Title          string             `json:"title"`
	ID             string             `json:"id"`
	UID            string             `json:"uid"`
	Kind           planrecords.Kind   `json:"kind"`
	Project        string             `json:"project"`
	State          planrecords.State  `json:"state"`
	Body           string             `json:"body,omitempty"`
	LegacyNumber   int                `json:"legacy_number,omitempty"`
	LegacySource   string             `json:"legacy_source,omitempty"`
	ShippedBy      string             `json:"shipped_by,omitempty"`
	RaisedBy       string             `json:"raised_by,omitempty"`
	ShippedVersion string             `json:"shipped_version,omitempty"`
	Links          []planrecords.Link `json:"links"`
	Aliases        []string           `json:"aliases"`
	NeedsReview    bool               `json:"needs_review"`
	ReviewReason   string             `json:"review_reason,omitempty"`
	CreatedAt      string             `json:"created_at"`
	UpdatedAt      string             `json:"updated_at"`
	ClosedAt       string             `json:"closed_at,omitempty"`
}

func newPlanRecordView(r planrecords.Record) planRecordView {
	v := planRecordView{
		Title: r.Title, ID: r.ID, UID: r.UID, Kind: r.Kind, Project: r.Project, State: r.State,
		Body: r.Body, LegacyNumber: r.LegacyNumber, LegacySource: r.LegacySource,
		ShippedBy: r.ShippedBy, RaisedBy: r.RaisedBy, ShippedVersion: r.ShippedVersion,
		Links: r.Links, Aliases: r.Aliases, NeedsReview: r.NeedsReview, ReviewReason: r.ReviewReason,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, ClosedAt: r.ClosedAt,
	}
	if v.Links == nil {
		v.Links = []planrecords.Link{}
	}
	if v.Aliases == nil {
		v.Aliases = []string{}
	}
	return v
}

func planRecordViews(rs []planrecords.Record) []planRecordView {
	out := make([]planRecordView, 0, len(rs))
	for _, r := range rs {
		out = append(out, newPlanRecordView(r))
	}
	return out
}

// planRecordCreateBody is the create body. Kind comes from the route, never
// from the body, and the store sets the id, uid and dates.
type planRecordCreateBody struct {
	Project        string             `json:"project"`
	Title          string             `json:"title"`
	Body           string             `json:"body"`
	State          planrecords.State  `json:"state"`
	LegacyNumber   int                `json:"legacy_number"`
	LegacySource   string             `json:"legacy_source"`
	ShippedBy      string             `json:"shipped_by"`
	RaisedBy       string             `json:"raised_by"`
	ShippedVersion string             `json:"shipped_version"`
	Links          []planrecords.Link `json:"links"`
	Aliases        []string           `json:"aliases"`
	NeedsReview    bool               `json:"needs_review"`
	ReviewReason   string             `json:"review_reason"`
}

// planRecordUpdateBody is the update body: a field left out is left alone.
// The project and the kind cannot change.
type planRecordUpdateBody struct {
	Title          *string             `json:"title"`
	Body           *string             `json:"body"`
	State          *planrecords.State  `json:"state"`
	LegacySource   *string             `json:"legacy_source"`
	ShippedBy      *string             `json:"shipped_by"`
	RaisedBy       *string             `json:"raised_by"`
	ShippedVersion *string             `json:"shipped_version"`
	Links          *[]planrecords.Link `json:"links"`
	AddAliases     []string            `json:"add_aliases"`
	NeedsReview    *bool               `json:"needs_review"`
	ReviewReason   *string             `json:"review_reason"`
}

// planRecordImportBody is the import body.
type planRecordImportBody struct {
	Project        string `json:"project"`
	Source         string `json:"source"`
	ConflictPolicy string `json:"conflict_policy"`
	FindingsGlob   string `json:"findings_glob"`
}

// planRecordsRoute is a parsed request path.
type planRecordsRoute struct {
	shape string // collection, item, search, import, reserved
	kind  planrecords.Kind
	ref   string
}

func parsePlanRecordsPath(path string) planRecordsRoute {
	rest := strings.Trim(strings.TrimPrefix(path, planRecordsPrefix), "/")
	first, tail, hasTail := strings.Cut(rest, "/")
	switch first {
	case "search":
		if !hasTail {
			return planRecordsRoute{shape: "search"}
		}
	case "browse":
		if !hasTail {
			return planRecordsRoute{shape: "browse"}
		}
	case "import":
		if !hasTail {
			return planRecordsRoute{shape: "import"}
		}
	case "registry-rows", "findings":
		k := planrecords.KindRegistryRow
		if first == "findings" {
			k = planrecords.KindFinding
		}
		if !hasTail {
			return planRecordsRoute{shape: "collection", kind: k}
		}
		if ref := strings.TrimSpace(tail); ref != "" {
			return planRecordsRoute{shape: "item", kind: k, ref: ref}
		}
		return planRecordsRoute{shape: "collection", kind: k}
	}
	return planRecordsRoute{shape: "reserved"}
}

// normalizePlanRef turns "row-12" into the store's "row 12".
func normalizePlanRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if len(ref) > 4 && strings.EqualFold(ref[:4], "row-") {
		if n, err := strconv.Atoi(ref[4:]); err == nil && n > 0 {
			return "row " + strconv.Itoa(n)
		}
	}
	return ref
}

func planRecordsError(w http.ResponseWriter, status int, msg string) {
	writeJSONError(w, status, map[string]string{"error": msg})
}

func (s *Server) handlePlanRecords(w http.ResponseWriter, r *http.Request) {
	route := parsePlanRecordsPath(r.URL.Path)
	if route.shape == "reserved" {
		planRecordsError(w, http.StatusNotFound, "not found")
		return
	}
	// The master switch comes first: while it is off nothing here exists.
	if !s.cfg.Autonomous.PlanRecords.EffectiveEnabled() {
		http.Error(w, "plan records are turned off (autonomous.plan_records.enabled)", http.StatusNotFound)
		return
	}

	var allowed []string
	switch route.shape {
	case "collection":
		allowed = []string{http.MethodGet, http.MethodPost}
	case "item":
		allowed = []string{http.MethodGet, http.MethodPut}
	case "search", "browse":
		allowed = []string{http.MethodGet}
	case "import":
		allowed = []string{http.MethodPost}
	}
	okMethod := false
	for _, m := range allowed {
		if r.Method == m {
			okMethod = true
		}
	}
	if !okMethod {
		w.Header().Set("Allow", strings.Join(allowed, ", "))
		planRecordsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Reads need autonomous:read, every change needs autonomous:write.
	needed := federation.CapAutonomousRead
	if r.Method != http.MethodGet {
		needed = federation.CapAutonomousWrite
	}
	if !s.fedCap(w, r, needed) {
		return
	}
	if s.planRecords == nil {
		planRecordsError(w, http.StatusServiceUnavailable, "plan records are not available")
		return
	}

	switch route.shape {
	case "collection":
		if r.Method == http.MethodPost {
			s.handlePlanRecordCreate(w, r, route.kind)
			return
		}
		s.handlePlanRecordList(w, r, route.kind)
	case "item":
		if r.Method == http.MethodPut {
			s.handlePlanRecordUpdate(w, r, route.kind, route.ref)
			return
		}
		s.handlePlanRecordGet(w, r, route.kind, route.ref)
	case "search":
		s.handlePlanRecordSearch(w, r)
	case "browse":
		s.handlePlanRecordBrowse(w, r)
	case "import":
		s.handlePlanRecordImport(w, r)
	}
}

// decodePlanRecordJSON reads exactly one JSON object from the capped body.
func decodePlanRecordJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPlanRecordBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		switch {
		case errors.As(err, &tooBig):
			planRecordsError(w, http.StatusRequestEntityTooLarge, "request body too large (cap 256 KiB)")
		default:
			planRecordsError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		}
		return false
	}
	if _, err := dec.Token(); err != io.EOF {
		planRecordsError(w, http.StatusBadRequest, "invalid JSON body: expected exactly one JSON object")
		return false
	}
	return true
}

// writePlanRecordStoreError answers a failure of a store call.
func writePlanRecordStoreError(w http.ResponseWriter, err error, what string) {
	var amb *planrecords.ErrAmbiguous
	var tc *planrecords.ErrTitleConflict
	var lc *planrecords.ErrLegacyNumberConflict
	var inv *planrecords.ErrInvalid
	switch {
	case errors.As(err, &amb):
		writeJSONError(w, http.StatusConflict, map[string]any{
			"error":      fmt.Sprintf("%q matches more than one record; say which one (use its id)", amb.Ref),
			"candidates": planRecordViews(amb.Candidates),
		})
	case errors.As(err, &tc):
		writeJSONError(w, http.StatusConflict, map[string]any{
			"error": fmt.Sprintf("the title %q is already used in project %s by %q (%s)",
				tc.Title, tc.Existing.Project, tc.Existing.Title, tc.Existing.ID),
			"existing": newPlanRecordView(tc.Existing),
		})
	case errors.As(err, &lc):
		writeJSONError(w, http.StatusConflict, map[string]any{
			"error": fmt.Sprintf("row %d is already held by %q (%s)",
				lc.Number, lc.Existing.Title, lc.Existing.ID),
			"existing": newPlanRecordView(lc.Existing),
		})
	case errors.Is(err, planrecords.ErrNotFound):
		planRecordsError(w, http.StatusNotFound, "no plan record matches that reference")
	case errors.As(err, &inv):
		planRecordsError(w, http.StatusBadRequest, err.Error())
	default:
		planRecordsError(w, http.StatusInternalServerError, "cannot "+what+": "+err.Error())
	}
}

// checkPlanRecordProject answers 400 (and returns false) when project is empty
// or is not a registered project alias.
func (s *Server) checkPlanRecordProject(w http.ResponseWriter, project string) bool {
	project = strings.TrimSpace(project)
	if project == "" {
		planRecordsError(w, http.StatusBadRequest, "project is required: name a registered project alias")
		return false
	}
	if err := autonomous.ValidateProjectAlias(project, s.projectAliasMap()); err != nil {
		planRecordsError(w, http.StatusBadRequest, err.Error())
		return false
	}
	return true
}

func (s *Server) handlePlanRecordCreate(w http.ResponseWriter, r *http.Request, kind planrecords.Kind) {
	var body planRecordCreateBody
	if !decodePlanRecordJSON(w, r, &body) {
		return
	}
	if !s.checkPlanRecordProject(w, body.Project) {
		return
	}
	if body.State != "" && !planrecords.ValidState(kind, body.State) {
		planRecordsError(w, http.StatusBadRequest, fmt.Sprintf("state %q is not valid for %s; use one of %s",
			body.State, kind, planStateList(kind)))
		return
	}
	rec, err := s.planRecords.Create(planrecords.Record{
		Kind: kind, Project: strings.TrimSpace(body.Project), Title: body.Title, Body: body.Body,
		State: body.State, LegacyNumber: body.LegacyNumber, LegacySource: body.LegacySource,
		ShippedBy: body.ShippedBy, RaisedBy: body.RaisedBy, ShippedVersion: body.ShippedVersion,
		Links: body.Links, Aliases: body.Aliases, NeedsReview: body.NeedsReview,
		ReviewReason: body.ReviewReason, Actor: s.auditActor(r.Context()),
	})
	if err != nil {
		writePlanRecordStoreError(w, err, "create the record")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{"record": newPlanRecordView(rec)})
}

// resolvePlanRecord finds the record a reference names inside one kind. It
// answers the failure itself and returns false.
func (s *Server) resolvePlanRecord(w http.ResponseWriter, r *http.Request, kind planrecords.Kind, ref string) (planrecords.Record, bool) {
	rec, err := s.planRecords.Resolve(normalizePlanRef(ref), strings.TrimSpace(r.URL.Query().Get("project")))
	if err == nil && rec.Kind != kind {
		err = planrecords.ErrNotFound
	}
	if err != nil {
		writePlanRecordStoreError(w, err, "read the record")
		return planrecords.Record{}, false
	}
	return rec, true
}

func (s *Server) handlePlanRecordGet(w http.ResponseWriter, r *http.Request, kind planrecords.Kind, ref string) {
	rec, ok := s.resolvePlanRecord(w, r, kind, ref)
	if !ok {
		return
	}
	writeJSONOK(w, map[string]any{"record": newPlanRecordView(rec)})
}

func (s *Server) handlePlanRecordUpdate(w http.ResponseWriter, r *http.Request, kind planrecords.Kind, ref string) {
	var body planRecordUpdateBody
	if !decodePlanRecordJSON(w, r, &body) {
		return
	}
	cur, ok := s.resolvePlanRecord(w, r, kind, ref)
	if !ok {
		return
	}
	if body.State != nil && !planrecords.ValidState(kind, *body.State) {
		planRecordsError(w, http.StatusBadRequest, fmt.Sprintf("state %q is not valid for %s; use one of %s",
			*body.State, kind, planStateList(kind)))
		return
	}
	rec, err := s.planRecords.Update(cur.UID, planrecords.Patch{
		Title: body.Title, Body: body.Body, State: body.State, LegacySource: body.LegacySource,
		ShippedBy: body.ShippedBy, RaisedBy: body.RaisedBy, ShippedVersion: body.ShippedVersion,
		Links: body.Links, AddAliases: body.AddAliases, NeedsReview: body.NeedsReview,
		ReviewReason: body.ReviewReason, Actor: s.auditActor(r.Context()),
	})
	if err != nil {
		writePlanRecordStoreError(w, err, "update the record")
		return
	}
	writeJSONOK(w, map[string]any{"record": newPlanRecordView(rec)})
}

func planStateList(kind planrecords.Kind) string {
	var parts []string
	for _, st := range planrecords.States(kind) {
		parts = append(parts, string(st))
	}
	return strings.Join(parts, ", ")
}

// planRecordFilter reads the shared list and search filters. fixedKind is the
// kind a collection route pins; empty for search. It answers 400 itself.
func planRecordFilter(w http.ResponseWriter, r *http.Request, fixedKind planrecords.Kind) (planrecords.Filter, bool) {
	q := r.URL.Query()
	f := planrecords.Filter{Kind: fixedKind, Project: strings.TrimSpace(q.Get("project")),
		Automaton: strings.TrimSpace(q.Get("automaton"))}
	if v := q.Get("kind"); v != "" {
		k := planrecords.Kind(v)
		if !planrecords.ValidKind(k) || (fixedKind != "" && k != fixedKind) {
			planRecordsError(w, http.StatusBadRequest, `kind must match the route: "registry_row" or "finding"`)
			return f, false
		}
		f.Kind = k
	}
	if v := q.Get("state"); v != "" {
		st := planrecords.State(v)
		valid := false
		for _, k := range []planrecords.Kind{planrecords.KindRegistryRow, planrecords.KindFinding} {
			if (f.Kind == "" || f.Kind == k) && planrecords.ValidState(k, st) {
				valid = true
			}
		}
		if !valid {
			planRecordsError(w, http.StatusBadRequest, fmt.Sprintf("state %q is not a valid state for this kind", v))
			return f, false
		}
		f.State = st
	}
	for name, dst := range map[string]*int{"legacy_number": &f.LegacyNumber, "limit": &f.Limit, "offset": &f.Offset} {
		v := q.Get(name)
		if v == "" {
			continue
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || (name != "offset" && n == 0) {
			planRecordsError(w, http.StatusBadRequest, name+" must be a positive whole number")
			return f, false
		}
		*dst = n
	}
	return f, true
}

func (s *Server) handlePlanRecordList(w http.ResponseWriter, r *http.Request, kind planrecords.Kind) {
	f, ok := planRecordFilter(w, r, kind)
	if !ok {
		return
	}
	if q := strings.TrimSpace(r.URL.Query().Get("q")); q != "" {
		hits, total := s.planRecords.Search(q, f)
		recs := make([]planrecords.Record, 0, len(hits))
		for _, h := range hits {
			recs = append(recs, h.Record)
		}
		writeJSONOK(w, planRecordPage(recs, total, f))
		return
	}
	recs, total := s.planRecords.List(f)
	writeJSONOK(w, planRecordPage(recs, total, f))
}

func planRecordPage(recs []planrecords.Record, total int, f planrecords.Filter) map[string]any {
	limit := f.Limit
	if limit <= 0 {
		limit = planrecords.DefaultLimit
	}
	if limit > planrecords.MaxLimit {
		limit = planrecords.MaxLimit
	}
	return map[string]any{"records": planRecordViews(recs), "total": total, "limit": limit, "offset": f.Offset}
}

type planRecordHitView struct {
	Record  planRecordView `json:"record"`
	Score   int            `json:"score"`
	Matched string         `json:"matched"`
}

func (s *Server) handlePlanRecordSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		planRecordsError(w, http.StatusBadRequest, "q is required: the words to search for")
		return
	}
	f, ok := planRecordFilter(w, r, "")
	if !ok {
		return
	}
	hits, total := s.planRecords.Search(q, f)
	out := make([]planRecordHitView, 0, len(hits))
	for _, h := range hits {
		out = append(out, planRecordHitView{Record: newPlanRecordView(h.Record), Score: h.Score, Matched: h.Matched})
	}
	limit := f.Limit
	if limit <= 0 {
		limit = planrecords.DefaultLimit
	}
	if limit > planrecords.MaxLimit {
		limit = planrecords.MaxLimit
	}
	writeJSONOK(w, map[string]any{"hits": out, "total": total, "limit": limit, "offset": f.Offset})
}

func (s *Server) handlePlanRecordImport(w http.ResponseWriter, r *http.Request) {
	var body planRecordImportBody
	if !decodePlanRecordJSON(w, r, &body) {
		return
	}
	dry := false
	if v := strings.TrimSpace(r.URL.Query().Get("dry_run")); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			planRecordsError(w, http.StatusBadRequest, "dry_run must be true or false")
			return
		}
		dry = b
	}
	project := strings.TrimSpace(body.Project)
	if project == "" {
		project = planrecords.DefaultImportProject
	}
	if !s.checkPlanRecordProject(w, project) {
		return
	}
	switch body.Source {
	case "", planrecords.ImportSourceAll, planrecords.ImportSourceRegistry, planrecords.ImportSourceFindings:
	default:
		planRecordsError(w, http.StatusBadRequest, `source must be "registry", "findings" or "all"`)
		return
	}
	policyText := strings.TrimSpace(body.ConflictPolicy)
	if policyText == "" {
		policyText = s.cfg.Autonomous.PlanRecords.EffectiveImportConflictPolicy()
	}
	policy, err := planrecords.ParsePolicy(policyText)
	if err != nil {
		planRecordsError(w, http.StatusBadRequest, `conflict_policy must be "suffix", "rename" or "skip"`)
		return
	}
	aliases := s.projectAliasMap()
	im := &planrecords.Importer{
		Store: s.planRecords,
		ProjectDir: func(alias string) (string, bool) {
			d, ok := aliases[alias]
			return d, ok && strings.TrimSpace(d) != ""
		},
		Roots: s.cfg.AllowedProjectRoots,
	}
	actor := s.auditActor(r.Context())
	res, err := im.Import(planrecords.ImportOptions{
		Project: project, Source: body.Source, FindingsGlob: body.FindingsGlob,
		Policy: policy, DryRun: dry, Actor: actor,
	})
	if err != nil {
		var unk *planrecords.ErrUnknownProject
		if errors.As(err, &unk) || strings.Contains(err.Error(), "planrecords:") {
			planRecordsError(w, http.StatusBadRequest, err.Error())
			return
		}
		planRecordsError(w, http.StatusInternalServerError, "cannot import: "+err.Error())
		return
	}
	if !dry && s.auditLog != nil {
		src := body.Source
		if src == "" {
			src = planrecords.ImportSourceAll
		}
		_ = s.auditLog.Write(audit.Entry{Actor: actor, Action: audit.ActionPlanRecordImported, Subject: project,
			Details: map[string]any{
				"project": project, "source": src, "policy": string(res.Policy),
				"created": res.Counts.Create, "unchanged": res.Counts.Unchanged, "changed": res.Counts.Changed,
				"conflicts": res.Counts.Conflicts, "skipped": res.Counts.Skipped,
			}})
	}
	note := "Preview only: nothing was written."
	if !dry {
		note = "Import finished. Run it again any time; records already imported are left alone."
	}
	writeJSONOK(w, map[string]any{"note": note, "result": res})
}
