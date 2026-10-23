package server

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
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

// writePRDList serves the collection GET. full=1 keeps the original shape
// (every PRD, in full, under "prds"); anything else is the summary list.
func (s *Server) writePRDList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if queryBool(r, "full", false) {
		writeJSONOK(w, map[string]any{"prds": s.autonomousMgr.ListPRDs()})
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
	if limit > prdListMaxLimit {
		limit = prdListMaxLimit
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

	statuses := map[string]bool{}
	for _, st := range strings.Split(q.Get("status"), ",") {
		if st = strings.ToLower(strings.TrimSpace(st)); st != "" {
			statuses[st] = true
		}
	}
	active := queryBool(r, "active", false)
	needle := strings.ToLower(strings.TrimSpace(q.Get("q")))
	tag := q.Get("tag_id")
	if tag == "" {
		tag = q.Get("tag")
	}
	filtered := len(statuses) > 0 || active || needle != "" || tag != ""

	var rows []autonomous.SummaryRow
	for _, raw := range s.autonomousMgr.ListPRDs() {
		prd, err := toPRD(raw)
		if err != nil || prd == nil {
			continue
		}
		row := autonomous.BuildSummaryRow(prd)
		st := string(row.Status)
		if len(statuses) > 0 && !statuses[st] {
			continue
		}
		if active && !prdListActiveStatuses[st] {
			continue
		}
		if needle != "" && !strings.Contains(strings.ToLower(row.Title), needle) &&
			!strings.Contains(strings.ToLower(row.ID), needle) {
			continue
		}
		if tag != "" && !containsString(row.Tags, tag) {
			continue
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		return prdBefore(rows[i].UpdatedAt, rows[i].ID, rows[j].UpdatedAt, rows[j].ID)
	})

	// With no filter the first page carries every active Automaton plus the
	// most recent limit others; cursor pages then walk only the others, so
	// no active row is repeated and none is skipped.
	total := len(rows)
	var actives []autonomous.SummaryRow
	if !filtered {
		var rest []autonomous.SummaryRow
		for _, row := range rows {
			if prdListActiveStatuses[string(row.Status)] {
				actives = append(actives, row)
			} else {
				rest = append(rest, row)
			}
		}
		rows = rest
	}
	start := 0
	if cur != nil {
		actives = nil
		start = sort.Search(len(rows), func(i int) bool {
			return prdBefore(cur.UpdatedAt, cur.ID, rows[i].UpdatedAt, rows[i].ID)
		})
	}
	end := start + limit
	if end > len(rows) {
		end = len(rows)
	}
	page := rows[start:end]
	if len(actives) > 0 {
		page = append(append([]autonomous.SummaryRow{}, actives...), page...)
		sort.Slice(page, func(i, j int) bool {
			return prdBefore(page[i].UpdatedAt, page[i].ID, page[j].UpdatedAt, page[j].ID)
		})
	}
	resp := prdListResponse{PRDs: page, Total: total}
	if end < len(rows) && end > start {
		last := rows[end-1]
		resp.NextCursor = encodePRDListCursor(prdListCursor{UpdatedAt: last.UpdatedAt, ID: last.ID})
	}
	writePRDListResponse(w, r, resp)
}

func containsString(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// writePRDListResponse marshals resp, sets an ETag built from the body hash
// plus each included row's id, rev and updated_at, and answers a matching
// If-None-Match with 304 and no body.
func writePRDListResponse(w http.ResponseWriter, r *http.Request, resp prdListResponse) {
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
