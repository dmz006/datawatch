// Pending item REST surface. A pending item is a named piece of work that has
// no Automaton yet; an Automation member can wait on it.
//
//   GET  /api/autonomous/pending-items[?state=<s>&automation_id=<id>] — the
//        policy and file registry settings in effect, the last registry
//        refresh, every item with the Automations that hold it, and the
//        Automations held on a waiting item right now.
//   POST /api/autonomous/pending-items {title, ref?} — record a waiting item (201).
//   POST /api/autonomous/pending-items/{id}/attach {automaton_id} — attach an
//        Automaton to a waiting item.
//   POST /api/autonomous/pending-items/refresh — read the file registry now.
//
// It also carries the strict write checks and the apply step for the nine
// autonomous.automation.pending_item_* config keys that PUT /api/config and
// PUT /api/autonomous/config accept.

package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
)

// pendingItemManager is the narrow Manager surface the pending item routes
// need. The daemon's autonomous API exposes it through its Manager; minimal
// test doubles implement it directly.
type pendingItemManager interface {
	ListPendingItems(f autonomous.PendingItemFilter) []*autonomous.PendingItem
	CreatePendingItem(title, ref, actor string) (*autonomous.PendingItem, error)
	AttachPendingItem(itemID, automatonID, actor string) (*autonomous.PendingItemAttachResult, error)
	RefreshPendingItems() autonomous.PendingItemRefreshResult
	LastPendingItemRefresh() (autonomous.PendingItemRefreshResult, bool)
}

// pendingItemMgr returns the pending item surface of the live manager, or nil
// when the manager does not carry one.
func (s *Server) pendingItemMgr() pendingItemManager {
	if pm, ok := s.autonomousMgr.(pendingItemManager); ok {
		return pm
	}
	if api, ok := s.autonomousMgr.(*autonomous.API); ok && api != nil && api.M != nil {
		return api.M
	}
	return nil
}

// pendingItemAutomations is the narrow read of the Automations list the
// routes use to say which Automations hold an item.
type pendingItemAutomations interface {
	ListAutomations() []any
}

// ---- response shapes ----

type pendingItemSourceView struct {
	Configured   bool     `json:"configured"`
	Path         string   `json:"path"`
	IDPattern    string   `json:"id_pattern"`
	IDColumn     string   `json:"id_column"`
	TitleColumn  string   `json:"title_column"`
	StateColumn  string   `json:"state_column"`
	DoneValues   []string `json:"done_values"`
	OnMissingRow string   `json:"on_missing_row"`
}

type pendingItemRefreshView struct {
	Configured bool       `json:"configured"`
	At         *time.Time `json:"at"`
	Rows       int        `json:"rows"`
	Added      int        `json:"added"`
	Updated    int        `json:"updated"`
	Done       int        `json:"done"`
	Dropped    int        `json:"dropped"`
	Kept       int        `json:"kept"`
	Skipped    int        `json:"skipped"`
	Error      string     `json:"error"`
}

type pendingItemAutomationRef struct {
	AutomationID string `json:"automation_id"`
	Name         string `json:"name"`
	MemberID     string `json:"member_id"`
	Held         bool   `json:"held"`
}

type pendingItemView struct {
	ID          string                     `json:"id"`
	Ref         string                     `json:"ref"`
	Title       string                     `json:"title"`
	State       string                     `json:"state"`
	Source      string                     `json:"source"`
	AutomatonID string                     `json:"automaton_id"`
	CreatedAt   time.Time                  `json:"created_at"`
	UpdatedAt   time.Time                  `json:"updated_at"`
	AttachedAt  *time.Time                 `json:"attached_at,omitempty"`
	DoneAt      *time.Time                 `json:"done_at,omitempty"`
	Automations []pendingItemAutomationRef `json:"automations"`
}

type pendingItemHeldView struct {
	AutomationID string    `json:"automation_id"`
	Name         string    `json:"name"`
	MemberID     string    `json:"member_id"`
	ItemID       string    `json:"item_id"`
	Title        string    `json:"title"`
	Ref          string    `json:"ref"`
	Since        time.Time `json:"since"`
	Reason       string    `json:"reason"`
}

type pendingItemsReply struct {
	Policy      string                 `json:"policy"`
	AutoResume  bool                   `json:"auto_resume"`
	Source      pendingItemSourceView  `json:"source"`
	LastRefresh pendingItemRefreshView `json:"last_refresh"`
	Items       []pendingItemView      `json:"items"`
	Held        []pendingItemHeldView  `json:"held"`
}

// ---- reading the Automations an item belongs to ----

// pendingAutomationWire is the part of an Automation (or its view) the item
// routes read.
type pendingAutomationWire struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Status          string     `json:"status"`
	CurrentMemberID string     `json:"current_member_id"`
	ActiveCursors   []string   `json:"active_cursors"`
	IdleSince       *time.Time `json:"idle_since"`
	UpdatedAt       time.Time  `json:"updated_at"`
	Members         []struct {
		ID            string `json:"id"`
		Kind          string `json:"kind"`
		PendingItemID string `json:"pending_item_id"`
	} `json:"members"`
	Decisions []struct {
		At   time.Time `json:"at"`
		Kind string    `json:"kind"`
		Note string    `json:"note"`
	} `json:"decisions"`
}

// holdsCursor reports whether member id holds a cursor of the Automation.
func (a *pendingAutomationWire) holdsCursor(id string) bool {
	if id == "" {
		return false
	}
	if a.CurrentMemberID == id {
		return true
	}
	for _, c := range a.ActiveCursors {
		if c == id {
			return true
		}
	}
	return false
}

// pendingAutomations reads every Automation the manager reports. A manager
// that cannot list them, or an entry that does not decode, reads as none.
func (s *Server) pendingAutomations() []pendingAutomationWire {
	lister, ok := s.autonomousMgr.(pendingItemAutomations)
	if !ok {
		return nil
	}
	var out []pendingAutomationWire
	for _, raw := range lister.ListAutomations() {
		b, err := json.Marshal(raw)
		if err != nil {
			continue
		}
		var a pendingAutomationWire
		if json.Unmarshal(b, &a) == nil && a.ID != "" {
			out = append(out, a)
		}
	}
	return out
}

// pendingHeldReason is why Automation a is blocked: the note of its latest
// blocked decision, else a plain statement built from the item.
func pendingHeldReason(a *pendingAutomationWire, title string) string {
	for i := len(a.Decisions) - 1; i >= 0; i-- {
		if a.Decisions[i].Kind == "blocked" && strings.TrimSpace(a.Decisions[i].Note) != "" {
			return a.Decisions[i].Note
		}
	}
	return fmt.Sprintf("waiting for an Automaton to be attached to %q", title)
}

// buildPendingItemViews turns registry items into their views and finds the
// Automations held on a waiting item. An Automation holds an item when it is
// blocked and a pending item member of that item has its cursor.
func buildPendingItemViews(items []*autonomous.PendingItem, autos []pendingAutomationWire) ([]pendingItemView, []pendingItemHeldView) {
	byID := make(map[string]*autonomous.PendingItem, len(items))
	for _, it := range items {
		byID[it.ID] = it
	}
	refs := map[string][]pendingItemAutomationRef{}
	held := []pendingItemHeldView{}
	for i := range autos {
		a := &autos[i]
		for _, m := range a.Members {
			if m.PendingItemID == "" {
				continue
			}
			isHeld := a.Status == string(autonomous.AutomationBlocked) &&
				m.Kind == autonomous.MemberKindPending && a.holdsCursor(m.ID)
			refs[m.PendingItemID] = append(refs[m.PendingItemID], pendingItemAutomationRef{
				AutomationID: a.ID, Name: a.Name, MemberID: m.ID, Held: isHeld,
			})
			if !isHeld {
				continue
			}
			h := pendingItemHeldView{AutomationID: a.ID, Name: a.Name, MemberID: m.ID, ItemID: m.PendingItemID}
			if it := byID[m.PendingItemID]; it != nil {
				h.Title, h.Ref = it.Title, it.Ref
			}
			h.Since = a.UpdatedAt
			if a.IdleSince != nil {
				h.Since = *a.IdleSince
			}
			h.Reason = pendingHeldReason(a, h.Title)
			held = append(held, h)
		}
	}
	views := make([]pendingItemView, 0, len(items))
	for _, it := range items {
		v := newPendingItemView(it)
		if r := refs[it.ID]; r != nil {
			v.Automations = r
		}
		views = append(views, v)
	}
	return views, held
}

func newPendingItemView(it *autonomous.PendingItem) pendingItemView {
	return pendingItemView{
		ID: it.ID, Ref: it.Ref, Title: it.Title, State: string(it.State), Source: string(it.Source),
		AutomatonID: it.AutomatonID, CreatedAt: it.CreatedAt, UpdatedAt: it.UpdatedAt,
		AttachedAt: it.AttachedAt, DoneAt: it.DoneAt, Automations: []pendingItemAutomationRef{},
	}
}

// ---- config in effect ----

// pendingItemConfig reads the pending item settings in effect. The live
// manager's config is the source of truth; a manager that reports none (a test
// double) falls back to the server's own config.
func (s *Server) pendingItemConfig() config.AutonomousAutomationConfig {
	if s.autonomousMgr != nil {
		if raw := s.autonomousMgr.Config(); raw != nil {
			if b, err := json.Marshal(raw); err == nil {
				var wire struct {
					Automation struct {
						Policy     string                         `json:"pending_item_policy"`
						AutoResume *bool                          `json:"pending_item_auto_resume"`
						Source     config.PendingItemSourceConfig `json:"pending_item_source"`
					} `json:"automation"`
				}
				if json.Unmarshal(b, &wire) == nil {
					return config.AutonomousAutomationConfig{
						PendingItemPolicy:     wire.Automation.Policy,
						PendingItemAutoResume: wire.Automation.AutoResume,
						PendingItemSource:     wire.Automation.Source,
					}
				}
			}
		}
	}
	if s.cfg != nil {
		return s.cfg.Autonomous.Automation
	}
	return config.AutonomousAutomationConfig{}
}

// pendingItemSourceSurface renders the file registry settings as effective
// values: every unset key reads its default from the Effective* accessors.
func pendingItemSourceSurface(c config.PendingItemSourceConfig) map[string]interface{} {
	return map[string]interface{}{
		"path":           c.EffectivePath(),
		"id_pattern":     c.EffectiveIDPattern(),
		"id_column":      c.EffectiveIDColumn(),
		"title_column":   c.EffectiveTitleColumn(),
		"state_column":   c.EffectiveStateColumn(),
		"done_values":    c.EffectiveDoneValues(),
		"on_missing_row": c.EffectiveOnMissingRow(),
	}
}

func newPendingItemSourceView(c config.PendingItemSourceConfig) pendingItemSourceView {
	return pendingItemSourceView{
		Configured:   c.EffectivePath() != "",
		Path:         c.EffectivePath(),
		IDPattern:    c.EffectiveIDPattern(),
		IDColumn:     c.EffectiveIDColumn(),
		TitleColumn:  c.EffectiveTitleColumn(),
		StateColumn:  c.EffectiveStateColumn(),
		DoneValues:   c.EffectiveDoneValues(),
		OnMissingRow: c.EffectiveOnMissingRow(),
	}
}

func newPendingItemRefreshView(res autonomous.PendingItemRefreshResult, has bool, configured bool) pendingItemRefreshView {
	if !has {
		return pendingItemRefreshView{Configured: configured}
	}
	at := res.At
	return pendingItemRefreshView{
		Configured: res.Configured, At: &at, Rows: res.Rows, Added: res.Added, Updated: res.Updated,
		Done: res.Done, Dropped: res.Dropped, Kept: res.Kept, Skipped: res.Skipped, Error: res.Error,
	}
}

// ---- handlers ----

// pendingItemErrStatus maps a manager error to its HTTP status. The message
// keeps the status word the manager wrote ("404:", "409:", "400:").
func pendingItemErrStatus(err error) (int, string) {
	msg := err.Error()
	switch {
	case strings.HasPrefix(msg, "400:"):
		return http.StatusBadRequest, msg
	case strings.HasPrefix(msg, "404:"):
		return http.StatusNotFound, msg
	case strings.HasPrefix(msg, "409:"):
		return http.StatusConflict, msg
	}
	if !strings.HasPrefix(msg, "500:") {
		msg = "500: " + msg
	}
	return http.StatusInternalServerError, msg
}

// pendingItemGate runs the checks every pending item route shares: the
// Automata must be enabled, the caller needs the capability, and the manager
// must carry the pending item surface. It writes the refusal itself.
func (s *Server) pendingItemGate(w http.ResponseWriter, r *http.Request, write bool) pendingItemManager {
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
		return nil
	}
	need := federation.CapAutonomousRead
	if write {
		need = federation.CapAutonomousWrite
	}
	if !s.fedCap(w, r, need) {
		return nil
	}
	pm := s.pendingItemMgr()
	if pm == nil {
		http.Error(w, "Automaton pending item registry unavailable", http.StatusServiceUnavailable)
		return nil
	}
	return pm
}

func pendingItemMethodNotAllowed(w http.ResponseWriter, allow ...string) {
	w.Header().Set("Allow", strings.Join(allow, ", "))
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// decodePendingItemBody reads an optional JSON object body into v. An empty
// body is accepted and leaves v alone.
func decodePendingItemBody(r *http.Request, v any) error {
	if r.Body == nil {
		return nil
	}
	if err := json.NewDecoder(r.Body).Decode(v); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("400: bad request: %v", err)
	}
	return nil
}

// handlePendingItems serves /api/autonomous/pending-items: GET reads the
// registry, POST records a waiting item. Any other method is a 405.
func (s *Server) handlePendingItems(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.pendingItemsList(w, r)
	case http.MethodPost:
		s.pendingItemsCreate(w, r)
	default:
		pendingItemMethodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

// handlePendingItemsSub serves the routes under /api/autonomous/pending-items/:
// POST refresh and POST {id}/attach.
func (s *Server) handlePendingItemsSub(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/autonomous/pending-items/"), "/")
	parts := strings.Split(rest, "/")
	switch {
	case rest == "":
		s.handlePendingItems(w, r)
	case len(parts) == 1 && parts[0] == "refresh":
		if r.Method != http.MethodPost {
			pendingItemMethodNotAllowed(w, http.MethodPost)
			return
		}
		s.pendingItemsRefresh(w, r)
	case len(parts) == 2 && parts[1] == "attach":
		if r.Method != http.MethodPost {
			pendingItemMethodNotAllowed(w, http.MethodPost)
			return
		}
		s.pendingItemsAttach(w, r, parts[0])
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) pendingItemsList(w http.ResponseWriter, r *http.Request) {
	pm := s.pendingItemGate(w, r, false)
	if pm == nil {
		return
	}
	var filter autonomous.PendingItemFilter
	if st := strings.TrimSpace(r.URL.Query().Get("state")); st != "" {
		switch autonomous.PendingItemState(st) {
		case autonomous.PendingItemWaiting, autonomous.PendingItemAttached, autonomous.PendingItemDone:
			filter.State = autonomous.PendingItemState(st)
		default:
			http.Error(w, fmt.Sprintf("400: state must be %q, %q or %q, got %q",
				autonomous.PendingItemWaiting, autonomous.PendingItemAttached, autonomous.PendingItemDone, st), http.StatusBadRequest)
			return
		}
	}
	items := pm.ListPendingItems(filter)
	views, held := buildPendingItemViews(items, s.pendingAutomations())

	if want := strings.TrimSpace(r.URL.Query().Get("automation_id")); want != "" {
		kept := make([]pendingItemView, 0, len(views))
		for _, v := range views {
			for _, a := range v.Automations {
				if a.AutomationID == want {
					kept = append(kept, v)
					break
				}
			}
		}
		views = kept
		heldKept := make([]pendingItemHeldView, 0, len(held))
		for _, h := range held {
			if h.AutomationID == want {
				heldKept = append(heldKept, h)
			}
		}
		held = heldKept
	}

	cfg := s.pendingItemConfig()
	src := newPendingItemSourceView(cfg.PendingItemSource)
	last, has := pm.LastPendingItemRefresh()
	writeJSONOK(w, pendingItemsReply{
		Policy:      cfg.EffectivePendingItemPolicy(),
		AutoResume:  cfg.EffectivePendingItemAutoResume(),
		Source:      src,
		LastRefresh: newPendingItemRefreshView(last, has, src.Configured),
		Items:       views,
		Held:        held,
	})
}

func (s *Server) pendingItemsCreate(w http.ResponseWriter, r *http.Request) {
	pm := s.pendingItemGate(w, r, true)
	if pm == nil {
		return
	}
	var req struct {
		Title string `json:"title"`
		Ref   string `json:"ref"`
		Actor string `json:"actor"`
	}
	if err := decodePendingItemBody(r, &req); err != nil {
		code, msg := pendingItemErrStatus(err)
		http.Error(w, msg, code)
		return
	}
	item, err := pm.CreatePendingItem(req.Title, strings.TrimSpace(req.Ref), req.Actor)
	if err != nil {
		code, msg := pendingItemErrStatus(err)
		http.Error(w, msg, code)
		return
	}
	s.audit(r.Context(), "create", "pending_item", item.ID, map[string]any{"ref": item.Ref, "title": item.Title})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(newPendingItemView(item))
}

func (s *Server) pendingItemsAttach(w http.ResponseWriter, r *http.Request, itemID string) {
	pm := s.pendingItemGate(w, r, true)
	if pm == nil {
		return
	}
	var req struct {
		AutomatonID string `json:"automaton_id"`
		Actor       string `json:"actor"`
	}
	if err := decodePendingItemBody(r, &req); err != nil {
		code, msg := pendingItemErrStatus(err)
		http.Error(w, msg, code)
		return
	}
	res, err := pm.AttachPendingItem(itemID, req.AutomatonID, req.Actor)
	if err != nil {
		code, msg := pendingItemErrStatus(err)
		http.Error(w, msg, code)
		return
	}
	if res == nil {
		http.Error(w, "500: attach returned no result", http.StatusInternalServerError)
		return
	}
	out := *res
	if out.Converted == nil {
		out.Converted = []autonomous.PendingItemAttachConversion{}
	}
	if out.Resumed == nil {
		out.Resumed = []string{}
	}
	s.audit(r.Context(), "attach", "pending_item", itemID, map[string]any{"automaton_id": req.AutomatonID})
	writeJSONOK(w, out)
}

func (s *Server) pendingItemsRefresh(w http.ResponseWriter, r *http.Request) {
	pm := s.pendingItemGate(w, r, true)
	if pm == nil {
		return
	}
	res := pm.RefreshPendingItems()
	s.audit(r.Context(), "refresh", "pending_item", "", map[string]any{
		"configured": res.Configured, "added": res.Added, "updated": res.Updated, "done": res.Done,
	})
	writeJSONOK(w, res)
}

// ---- config keys ----

// pendingItemConfigKeys lists the nine keys, with the words each allows.
var pendingItemConfigKeys = []struct {
	key     string
	allowed string
}{
	{config.PendingItemKeyPolicy, `"hold" or "skip"`},
	{config.PendingItemKeyAutoResume, `"true" or "false"`},
	{config.PendingItemKeyPath, "a file path"},
	{config.PendingItemKeyIDPattern, "a regular expression"},
	{config.PendingItemKeyIDColumn, "a header name or a 1-based column number"},
	{config.PendingItemKeyTitleColumn, "a header name or a 1-based column number"},
	{config.PendingItemKeyStateColumn, "a header name or a 1-based column number"},
	{config.PendingItemKeyDoneValues, "a list of words"},
	{config.PendingItemKeyOnMissingRow, `"keep" or "drop"`},
}

// pendingItemValueText turns the raw value of one key into the text
// config.ValidatePendingItemKey checks: strings stay as they are, a bool reads
// "true" or "false", and a list of strings reads comma-separated. Any other
// shape is an error naming the allowed words.
func pendingItemValueText(key string, v interface{}) (string, error) {
	allowed := ""
	for _, k := range pendingItemConfigKeys {
		if k.key == key {
			allowed = k.allowed
		}
	}
	switch t := v.(type) {
	case string:
		return t, nil
	case bool:
		if key == config.PendingItemKeyAutoResume {
			return fmt.Sprintf("%t", t), nil
		}
	case []interface{}:
		if key == config.PendingItemKeyDoneValues {
			words := make([]string, 0, len(t))
			for _, item := range t {
				s, ok := item.(string)
				if !ok {
					return "", fmt.Errorf("%s: every entry must be a string (%s)", key, allowed)
				}
				words = append(words, s)
			}
			return strings.Join(words, ","), nil
		}
	case []string:
		if key == config.PendingItemKeyDoneValues {
			return strings.Join(t, ","), nil
		}
	}
	return "", fmt.Errorf("%s: wrong type of value (%s)", key, allowed)
}

// validatePendingItemPatch checks every pending item key present in a PUT
// /api/config patch before anything is applied, so one bad value rejects the
// whole PUT. The text of a refusal names the allowed words.
func validatePendingItemPatch(patch map[string]interface{}) error {
	for _, k := range pendingItemConfigKeys {
		v, ok := patch[k.key]
		if !ok {
			continue
		}
		text, err := pendingItemValueText(k.key, v)
		if err != nil {
			return err
		}
		if err := config.ValidatePendingItemKey(k.key, text); err != nil {
			return err
		}
	}
	return nil
}

// applyPendingItemValue stores one pending item key (validated first in
// handlePutConfig; a value that fails the check here is left unchanged).
func applyPendingItemValue(a *config.AutonomousAutomationConfig, key string, v interface{}) {
	text, err := pendingItemValueText(key, v)
	if err != nil || config.ValidatePendingItemKey(key, text) != nil {
		return
	}
	switch key {
	case config.PendingItemKeyPolicy:
		a.PendingItemPolicy = text
	case config.PendingItemKeyAutoResume:
		b := text == "true"
		a.PendingItemAutoResume = &b
	case config.PendingItemKeyPath:
		a.PendingItemSource.Path = text
	case config.PendingItemKeyIDPattern:
		a.PendingItemSource.IDPattern = text
	case config.PendingItemKeyIDColumn:
		a.PendingItemSource.IDColumn = text
	case config.PendingItemKeyTitleColumn:
		a.PendingItemSource.TitleColumn = text
	case config.PendingItemKeyStateColumn:
		a.PendingItemSource.StateColumn = text
	case config.PendingItemKeyDoneValues:
		var words []string
		for _, w := range strings.Split(text, ",") {
			if w = strings.TrimSpace(w); w != "" {
				words = append(words, w)
			}
		}
		a.PendingItemSource.DoneValues = words
	case config.PendingItemKeyOnMissingRow:
		a.PendingItemSource.OnMissingRow = text
	}
}

// patchTouchesPendingItem reports whether a PUT /api/config patch carries any
// pending item key.
func patchTouchesPendingItem(patch map[string]interface{}) bool {
	for _, k := range pendingItemConfigKeys {
		if _, ok := patch[k.key]; ok {
			return true
		}
	}
	return false
}

// withPendingItemClears makes the pending item keys of the automation JSON
// carry their string and list values explicitly. Those fields are omitempty,
// so a PUT that clears one back to empty (no registry path, the default id
// pattern) was dropped from the pushed block and the live manager kept the old
// value until a restart. Writing the current value lets the live merge apply
// the clear. A block that cannot be decoded is returned unchanged.
func withPendingItemClears(automation json.RawMessage, a config.AutonomousAutomationConfig) json.RawMessage {
	var auto map[string]json.RawMessage
	if json.Unmarshal(automation, &auto) != nil {
		return automation
	}
	block := map[string]json.RawMessage{}
	if raw, ok := auto["pending_item_source"]; ok {
		if json.Unmarshal(raw, &block) != nil {
			return automation
		}
	}
	src := a.PendingItemSource
	doneValues := src.DoneValues
	if doneValues == nil {
		doneValues = []string{}
	}
	for key, val := range map[string]interface{}{
		"path":           src.Path,
		"id_pattern":     src.IDPattern,
		"id_column":      src.IDColumn,
		"title_column":   src.TitleColumn,
		"state_column":   src.StateColumn,
		"done_values":    doneValues,
		"on_missing_row": src.OnMissingRow,
	} {
		b, err := json.Marshal(val)
		if err != nil {
			return automation
		}
		block[key] = b
	}
	nb, err := json.Marshal(block)
	if err != nil {
		return automation
	}
	auto["pending_item_source"] = nb
	pol, err := json.Marshal(a.PendingItemPolicy)
	if err != nil {
		return automation
	}
	auto["pending_item_policy"] = pol
	out, err := json.Marshal(auto)
	if err != nil {
		return automation
	}
	return out
}

// pendingItemAutonomousBody holds the pending item keys of a PUT
// /api/autonomous/config automation block, for the strict checks below.
type pendingItemAutonomousBody struct {
	Policy     *json.RawMessage `json:"pending_item_policy"`
	AutoResume *json.RawMessage `json:"pending_item_auto_resume"`
	Source     *json.RawMessage `json:"pending_item_source"`
}

// validatePendingItemAutomationBlock checks the pending item keys of the
// automation object of a PUT /api/autonomous/config body. The nested names
// match the dotted ones: pending_item_policy, pending_item_auto_resume and
// the keys of pending_item_source. An absent key keeps its value; a null is
// absent; a present one must pass the same check PUT /api/config runs.
func validatePendingItemAutomationBlock(rawAuto json.RawMessage) error {
	var body pendingItemAutonomousBody
	if json.Unmarshal(rawAuto, &body) != nil {
		return nil // the caller reports a malformed automation object
	}
	patch := map[string]interface{}{}
	take := func(key string, raw *json.RawMessage) error {
		if raw == nil || string(*raw) == "null" {
			return nil
		}
		var v interface{}
		if err := json.Unmarshal(*raw, &v); err != nil {
			return fmt.Errorf("%s: wrong type of value", key)
		}
		patch[key] = v
		return nil
	}
	if err := take(config.PendingItemKeyPolicy, body.Policy); err != nil {
		return err
	}
	if err := take(config.PendingItemKeyAutoResume, body.AutoResume); err != nil {
		return err
	}
	if body.Source != nil && string(*body.Source) != "null" {
		var fields map[string]json.RawMessage
		if json.Unmarshal(*body.Source, &fields) != nil {
			return fmt.Errorf("autonomous.automation.pending_item_source: must be an object")
		}
		for name, raw := range fields {
			raw := raw
			key := "autonomous.automation.pending_item_source." + name
			known := false
			for _, k := range pendingItemConfigKeys {
				if k.key == key {
					known = true
				}
			}
			if !known {
				continue
			}
			if err := take(key, &raw); err != nil {
				return err
			}
		}
	}
	return validatePendingItemPatch(patch)
}
