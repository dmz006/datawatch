package server

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/planrecords"
)

const (
	prdListDefaultLimit = 50
	prdListMaxLimit     = 500
)

// prdListActiveStatuses are the statuses active=1 selects: running, queued,
// waiting, awaiting approval and blocked work.
var prdListActiveStatuses = map[string]bool{
	"running":         true,
	"active":          true,
	"planning":        true,
	"queued":          true,
	"waiting":         true,
	"waiting_input":   true,
	"needs_review":    true,
	"revisions_asked": true,
	"approved":        true,
	"blocked":         true,
	"paused":          true,
}

// prdListStatusWords is the ?status filter vocabulary of the PRD list: the
// current and legacy PRD statuses, plus the legacy "shipped" alias that
// selects records carrying a shipped record. It feeds
// planrecords.ParseStateList, whose error names exactly these words.
var prdListStatusWords = []string{
	string(autonomous.PRDDraft),
	string(autonomous.PRDPlanning),
	"decomposing", // legacy alias for planning
	string(autonomous.PRDNeedsReview),
	string(autonomous.PRDApproved),
	string(autonomous.PRDRevisionsAsked),
	string(autonomous.PRDActive), // legacy alias for running
	string(autonomous.PRDRunning),
	"queued",
	"waiting",
	"waiting_input",
	string(autonomous.PRDCompleted),
	string(autonomous.PRDRejected),
	string(autonomous.PRDCancelled),
	string(autonomous.PRDArchived),
	string(autonomous.PRDBlocked),
	string(autonomous.PRDFailed),
	string(autonomous.PRDPaused),
	"shipped", // legacy alias: carries a shipped record
}

// prdListResponse is the body of the summary form of GET /api/autonomous/prds.
type prdListResponse struct {
	PRDs       []autonomous.SummaryRow `json:"prds"`
	Total      int                     `json:"total"`
	NextCursor string                  `json:"next_cursor,omitempty"`
}

// prdListCursor is the decoded form of the opaque cursor: the position of
// the last row of the previous page in (updated_at desc, id desc) order.
type prdListCursor struct {
	UpdatedAt time.Time `json:"u"`
	ID        string    `json:"i"`
}

func encodePRDListCursor(c prdListCursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodePRDListCursor(s string) (prdListCursor, bool) {
	var c prdListCursor
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || json.Unmarshal(b, &c) != nil || c.ID == "" {
		return c, false
	}
	return c, true
}

// prdBefore reports whether row a sorts before row b: newest updated_at
// first, then id descending.
func prdBefore(aT time.Time, aID string, bT time.Time, bID string) bool {
	if !aT.Equal(bT) {
		return aT.After(bT)
	}
	return aID > bID
}

// prdSummaryIndex is the optional read side of the Automaton summary index.
// A manager that has it answers the list from the index alone; one that does
// not (a test fake) falls back to reading each record.
type prdSummaryIndex interface {
	SummaryIndexEntries() []autonomous.SummaryEntry
	SummaryIndexRevision() uint64
	NotifyEffectiveForEntry(autonomous.SummaryEntry) autonomous.NotifyEventSet
}

// prdListFullMaxLimit caps one full-record page. A full read with no limit is
// still allowed for compatibility, but it writes every record.
const prdListFullMaxLimit = 100

// prdListItem is one candidate row of a list read: the keys the filter, sort
// and cursor need, plus a builder that makes the row only when it is paged in.
type prdListItem struct {
	ID        string
	UpdatedAt time.Time
	Status    autonomous.PRDStatus
	row       func() autonomous.SummaryRow
}

// prdListFilter is the filter set of a list read, decoded once per request.
type prdListFilter struct {
	statuses      map[string]bool
	active        bool
	needle        string
	tagSel        autonomous.ObjectTagSelection
	projectWanted string
	shipWanted    bool
	shipSet       bool
	shipEnabled   bool
	statusShipped bool
}

func (f *prdListFilter) any() bool {
	return len(f.statuses) > 0 || f.active || f.needle != "" || len(f.tagSel.Tags) > 0 ||
		f.projectWanted != "" || f.shipSet || (f.statusShipped && f.shipEnabled)
}

// matches applies every filter to the keys of one Automaton.
func (f *prdListFilter) matches(st, title, id string, shipped bool, systemTags, tags []string, project string) bool {
	if len(f.statuses) > 0 && !f.statuses[st] && (!f.statusShipped || !f.shipEnabled || !shipped) {
		return false
	}
	// status=shipped alone (no other statuses) is its own axis selection
	// while the shipped surface is enabled.
	if len(f.statuses) == 0 && f.statusShipped && f.shipEnabled && !shipped {
		return false
	}
	if f.shipSet {
		if f.shipWanted {
			if !f.shipEnabled || !shipped {
				return false
			}
		} else if f.shipEnabled && shipped {
			return false
		}
	}
	if f.active && !prdListActiveStatuses[st] {
		return false
	}
	if f.needle != "" && !strings.Contains(strings.ToLower(title), f.needle) &&
		!strings.Contains(strings.ToLower(id), f.needle) {
		return false
	}
	if !f.tagSel.MatchesObjectTags(systemTags, tags) {
		return false
	}
	return projectFilterMatches(f.projectWanted, project, systemTags)
}

// parsePRDListFilter decodes the filter query parameters. It writes the 400
// itself and reports false on a bad value.
func (s *Server) parsePRDListFilter(w http.ResponseWriter, r *http.Request) (*prdListFilter, bool) {
	q := r.URL.Query()
	f := &prdListFilter{statuses: map[string]bool{}}
	if v := q.Get("status"); v != "" {
		sv := strings.ToLower(v)
		// The PRD list always trimmed a single status, so keep the old single
		// form: lowercase, then trim when no comma is present at all.
		if !strings.ContainsRune(sv, ',') {
			sv = strings.TrimSpace(sv)
		}
		words, err := planrecords.ParseStateList(sv, "status", prdListStatusWords)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return nil, false
		}
		for _, st := range words {
			f.statuses[st] = true
		}
	}
	f.active = queryBool(r, "active", false)
	f.needle = strings.ToLower(strings.TrimSpace(q.Get("q")))
	tagSel, perr := objectTagSelectionFromRequest(r)
	if perr != nil {
		http.Error(w, perr.Error(), http.StatusBadRequest)
		return nil, false
	}
	// ?project=<alias> is the project axis filter: it matches the derived
	// project alias as well as the project system tag.
	f.tagSel, f.projectWanted = splitProjectFilter(r, tagSel)
	// Shipped-state read selection (shipped.go). shipped=true|false wins; the
	// autonomous.shipped.filter_default view (all/shipped/unshipped) sets the
	// default. Both act on the record's shipped state (a non-empty merge
	// commit). The status filter matches the displayed status: a record whose
	// work landed shows as "completed" (also when its stored status is still
	// cancelled) while the shipped surface is enabled. status=shipped stays
	// accepted as a legacy alias that selects records carrying a shipped record.
	ship := s.shippedSettings()
	f.shipEnabled = ship.EffectiveEnabled()
	var err error
	f.shipWanted, f.shipSet, err = shippedSelectionFromQuery(r, ship.EffectiveFilterDefault())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return nil, false
	}
	f.statusShipped = f.statuses["shipped"]
	delete(f.statuses, "shipped")
	return f, true
}

// writePRDList serves the collection GET. The default is the summary list,
// built from the summary index alone. full=true returns whole records: with a
// limit (or a cursor) it is one filtered page of them, without one it is every
// record, the heavy compatibility form.
func (s *Server) writePRDList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	idx, indexed := s.autonomousMgr.(prdSummaryIndex)
	full := queryBool(r, "full", false)
	if full && q.Get("limit") == "" && q.Get("cursor") == "" {
		s.writePRDListEverything(w)
		return
	}

	limit := prdListDefaultLimit
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			http.Error(w, "invalid limit", http.StatusBadRequest)
			return
		}
		limit = n
	}
	maxLimit := prdListMaxLimit
	if full {
		maxLimit = prdListFullMaxLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	var cur *prdListCursor
	if v := q.Get("cursor"); v != "" {
		c, ok := decodePRDListCursor(v)
		if !ok {
			http.Error(w, "invalid cursor", http.StatusBadRequest)
			return
		}
		cur = &c
	}
	filter, ok := s.parsePRDListFilter(w, r)
	if !ok {
		return
	}
	aliasRes := autonomous.NewProjectAliasResolver(s.projectAliasMap())

	// A list the client already holds answers 304 before any row is built:
	// the key is the index revision plus everything else the body depends on.
	etag := ""
	if indexed {
		etag = s.prdListIndexETag(idx, r, filter)
		if etagMatches(r.Header.Get("If-None-Match"), etag) {
			w.Header().Set("ETag", etag)
			w.Header().Set("Cache-Control", "no-cache")
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}

	var items []prdListItem
	fillTags := s.inheritedTagFiller() // one snapshot for the whole list
	fillRow := func(row *autonomous.SummaryRow, project string) {
		row.Project = project
		fillTags(&row.SummaryView)
		row.ApplyShippedSettings(filter.shipEnabled)
	}
	if indexed {
		for _, e := range idx.SummaryIndexEntries() {
			e := e
			project := aliasRes.Resolve(e.ProjectProfile, e.ProjectDir)
			if !filter.matches(displayStatusOf(&e, filter.shipEnabled), e.Title, e.ID, e.IsShipped(), e.Row.SystemTags, e.Row.Tags, project) {
				continue
			}
			items = append(items, prdListItem{ID: e.ID, UpdatedAt: e.UpdatedAt, Status: e.Status,
				row: func() autonomous.SummaryRow {
					row := e.IndexRow()
					set := idx.NotifyEffectiveForEntry(e)
					row.NotifyEventsEffective = &set
					fillRow(&row, project)
					finished := e.Status == autonomous.PRDCompleted || e.Status == autonomous.PRDArchived
					if e.HasPlanningFailure || finished {
						if raw, ok := s.autonomousMgr.GetPRD(e.ID); ok {
							if prd, err := toPRD(raw); err == nil {
								if e.HasPlanningFailure {
									row.PlanningFailure = s.planningFailureOf(prd)
								}
								if finished {
									row.MergeState = s.mergeStateOf(prd)
								}
							}
						}
					}
					return row
				}})
		}
	} else {
		for _, raw := range s.autonomousMgr.ListPRDs() {
			prd, err := toPRD(raw)
			if err != nil || prd == nil {
				continue
			}
			row := autonomous.BuildSummaryRow(prd)
			project := aliasRes.Resolve(prd.ProjectProfile, prd.ProjectDir)
			if !filter.matches(autonomous.PRDEffectiveDisplayStatus(prd, filter.shipEnabled), row.Title, row.ID, prd.IsShipped(), row.SystemTags, row.Tags, project) {
				continue
			}
			fillRow(&row, project)
			row.PlanningFailure = s.planningFailureOf(prd)
			row.MergeState = s.mergeStateOf(prd)
			items = append(items, prdListItem{ID: row.ID, UpdatedAt: row.UpdatedAt, Status: row.Status,
				row: func() autonomous.SummaryRow { return row }})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		return prdBefore(items[i].UpdatedAt, items[i].ID, items[j].UpdatedAt, items[j].ID)
	})

	// With no filter the first page carries every active Automaton plus the
	// most recent limit others; cursor pages then walk only the others, so
	// no active row is repeated and none is skipped.
	total := len(items)
	var actives []prdListItem
	if !filter.any() && !full { // a full page never exceeds its limit
		var rest []prdListItem
		for _, it := range items {
			if prdListActiveStatuses[string(it.Status)] {
				actives = append(actives, it)
			} else {
				rest = append(rest, it)
			}
		}
		items = rest
	}
	start := 0
	if cur != nil {
		actives = nil
		start = sort.Search(len(items), func(i int) bool {
			return prdBefore(cur.UpdatedAt, cur.ID, items[i].UpdatedAt, items[i].ID)
		})
	}
	end := start + limit
	if end > len(items) {
		end = len(items)
	}
	pageItems := items[start:end]
	if len(actives) > 0 {
		pageItems = append(append([]prdListItem{}, actives...), pageItems...)
		sort.Slice(pageItems, func(i, j int) bool {
			return prdBefore(pageItems[i].UpdatedAt, pageItems[i].ID, pageItems[j].UpdatedAt, pageItems[j].ID)
		})
	}
	next := ""
	if end < len(items) && end > start {
		last := items[end-1]
		next = encodePRDListCursor(prdListCursor{UpdatedAt: last.UpdatedAt, ID: last.ID})
	}

	if full {
		recs := make([]any, 0, len(pageItems))
		for _, it := range pageItems {
			if raw, ok := s.autonomousMgr.GetPRD(it.ID); ok {
				recs = append(recs, s.withPRDListProject(raw, aliasRes))
			}
		}
		writeJSONOK(w, map[string]any{"prds": recs, "total": total, "next_cursor": next})
		return
	}
	page := make([]autonomous.SummaryRow, 0, len(pageItems))
	for _, it := range pageItems {
		page = append(page, it.row())
	}
	writePRDListResponse(w, r, prdListResponse{PRDs: page, Total: total, NextCursor: next}, etag)
}

// withPRDListProject adds the derived project field to a full record when
// project aliases are registered, the same as the unpaged full form.
func (s *Server) withPRDListProject(raw any, res *autonomous.ProjectAliasResolver) any {
	if p, err := toPRD(raw); err == nil && p != nil {
		return withProjectField(raw, res.Resolve(p.ProjectProfile, p.ProjectDir))
	}
	return raw
}

// writePRDListEverything is the unpaged full form: every record, whole. It is
// kept for callers that read it today; it writes the whole store, so prefer
// full=true with a limit.
func (s *Server) writePRDListEverything(w http.ResponseWriter) {
	list := s.autonomousMgr.ListPRDs()
	aliases := s.projectAliasMap()
	var res *autonomous.ProjectAliasResolver
	if len(aliases) > 0 {
		res = autonomous.NewProjectAliasResolver(aliases)
	}
	out := make([]any, len(list))
	for i, raw := range list {
		out[i] = raw
		p, err := toPRD(raw)
		if err != nil || p == nil {
			continue
		}
		if len(aliases) > 0 {
			out[i] = withProjectField(out[i], res.Resolve(p.ProjectProfile, p.ProjectDir))
		}
		out[i] = s.withPlanningFailure(out[i], p)
	}
	writeJSONOK(w, map[string]any{"prds": out})
}

// prdListIndexETag is the ETag of an index-backed list: the index revision,
// the whole query, and the settings the rows are folded with.
func (s *Server) prdListIndexETag(idx prdSummaryIndex, r *http.Request, f *prdListFilter) string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "rev=%d\nq=%s\nship=%t\n", idx.SummaryIndexRevision(), r.URL.Query().Encode(), f.shipEnabled)
	aliases := s.projectAliasMap()
	keys := make([]string, 0, len(aliases))
	for k := range aliases {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		_, _ = fmt.Fprintf(h, "alias %s=%s\n", k, aliases[k])
	}
	set := idx.NotifyEffectiveForEntry(autonomous.SummaryEntry{})
	_, _ = fmt.Fprintf(h, "notify=%s\n", strings.Join(set.Events, ","))
	return `"` + hex.EncodeToString(h.Sum(nil))[:32] + `"`
}

// writePRDListResponse marshals resp, sets an ETag built from the body hash
// plus each included row's id, rev and updated_at, and answers a matching
// If-None-Match with 304 and no body.
func writePRDListResponse(w http.ResponseWriter, r *http.Request, resp prdListResponse, etagOverride ...string) {
	if resp.PRDs == nil {
		resp.PRDs = []autonomous.SummaryRow{}
	}
	body, err := json.Marshal(resp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h := sha256.New()
	h.Write(body)
	for _, row := range resp.PRDs {
		h.Write([]byte(row.ID + "|" + strconv.Itoa(row.Rev) + "|" + row.UpdatedAt.UTC().Format(time.RFC3339Nano) + "\n"))
	}
	etag := `"` + hex.EncodeToString(h.Sum(nil))[:32] + `"`
	if len(etagOverride) > 0 && etagOverride[0] != "" {
		etag = etagOverride[0]
	}
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	if etagMatches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

func etagMatches(header, etag string) bool {
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimPrefix(strings.TrimSpace(part), "W/")
		if part == "*" || part == etag {
			return true
		}
	}
	return false
}
