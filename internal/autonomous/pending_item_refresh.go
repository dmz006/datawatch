// Pending item registry refresh — feeds the daemon's internal registry from
// the optional file registry (autonomous.automation.pending_item_source.*).
// The file is only ever read. Rows are merged into the registry by (source
// file, ref): a new row adds a waiting item, a changed title updates it, a
// done row moves a waiting or attached item to done (and releases an
// Automation held on that item's member), and a ref that vanished from the
// file follows on_missing_row. Items whose source is internal are never
// touched. A refresh that cannot read or parse the file records the error and
// leaves the registry exactly as it was. Nothing here assumes a file name, a
// numbering sequence or any naming convention of this repository.

package autonomous

import (
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// pendingItemRefreshActor is the actor named on the audit entries and
// Decisions a refresh writes.
const pendingItemRefreshActor = "pending_item_refresh"

// controlPendingItemRefresh is the audit action for a refresh that changed
// the registry.
const controlPendingItemRefresh = "pending_item_refresh"

// PendingItemRefreshResult is what one refresh did. With no file registry
// configured only Configured (false) and At are set.
type PendingItemRefreshResult struct {
	Configured bool      `json:"configured"`
	Path       string    `json:"path,omitempty"`
	At         time.Time `json:"at"`
	Rows       int       `json:"rows"`    // rows read from the file
	Added      int       `json:"added"`   // new waiting items created
	Updated    int       `json:"updated"` // items whose title changed
	Done       int       `json:"done"`    // items moved to done
	Dropped    int       `json:"dropped"` // waiting file items deleted (on_missing_row drop)
	Kept       int       `json:"kept"`    // vanished refs left in the registry
	Skipped    int       `json:"skipped"` // rows not used: id mismatch, no title, duplicate ref
	Error      string    `json:"error,omitempty"`
}

// pendingRefreshState is the refresh bookkeeping held by the Manager: a lock
// that serializes refreshes, the last result and the base directory a
// relative registry path resolves against.
type pendingRefreshState struct {
	run     sync.Mutex // one refresh at a time
	mu      sync.Mutex // guards last, has and baseDir
	last    PendingItemRefreshResult
	has     bool
	baseDir string
}

// SetPendingItemSourceBaseDir sets the directory a relative registry path
// resolves against (the daemon's default project directory). An absolute path
// ignores it.
func (m *Manager) SetPendingItemSourceBaseDir(dir string) {
	m.pendingRefresh.mu.Lock()
	m.pendingRefresh.baseDir = strings.TrimSpace(dir)
	m.pendingRefresh.mu.Unlock()
}

// LastPendingItemRefresh returns the result of the most recent refresh, and
// false when none has run since the daemon started.
func (m *Manager) LastPendingItemRefresh() (PendingItemRefreshResult, bool) {
	m.pendingRefresh.mu.Lock()
	defer m.pendingRefresh.mu.Unlock()
	return m.pendingRefresh.last, m.pendingRefresh.has
}

// remember keeps res as the last result and returns it.
func (s *pendingRefreshState) remember(res PendingItemRefreshResult) PendingItemRefreshResult {
	s.mu.Lock()
	s.last, s.has = res, true
	s.mu.Unlock()
	return res
}

// RefreshPendingItems reads the configured file registry and merges its rows
// into the pending item registry. With no file registry configured it changes
// nothing and returns Configured=false. An invalid id pattern, an unreadable
// file, a path that escapes the base directory or a file with no usable table
// records Error and leaves the registry untouched. Running it again on an
// unchanged file changes nothing.
func (m *Manager) RefreshPendingItems() PendingItemRefreshResult {
	st := &m.pendingRefresh
	st.run.Lock()
	defer st.run.Unlock()

	res := PendingItemRefreshResult{At: time.Now()}
	src := m.Config().Automation.EffectivePendingItemSource()
	if src.Path == "" {
		return st.remember(res)
	}
	res.Configured, res.Path = true, src.Path
	fail := func(format string, args ...any) PendingItemRefreshResult {
		res.Error = fmt.Sprintf(format, args...)
		return st.remember(res)
	}
	if m.pendingItems == nil {
		return fail("pending item registry is not available")
	}
	pattern, err := regexp.Compile(src.IDPattern)
	if err != nil {
		return fail("pending item source id pattern is not a valid regular expression: %v", err)
	}
	st.mu.Lock()
	baseDir := st.baseDir
	st.mu.Unlock()
	content, err := ReadPendingItemSourceFile(baseDir, src.Path)
	if err != nil {
		return fail("%v", err)
	}
	rows, skipped, err := ParsePendingItemTable(content, PendingItemSourceMapping{
		IDColumn:    src.IDColumn,
		TitleColumn: src.TitleColumn,
		StateColumn: src.StateColumn,
		IDPattern:   pattern,
		DoneValues:  src.DoneValues,
	})
	if err != nil {
		if errors.Is(err, ErrPendingItemNoTable) {
			return fail("%v", err)
		}
		return fail("read pending item registry: %v", err)
	}
	res.Rows, res.Skipped = len(rows), skipped

	var released []string // ids of items that moved to done
	if err := m.mergePendingRows(rows, src, &res, &released); err != nil {
		res.Error = err.Error()
	}
	for _, id := range released {
		m.releasePendingHolds(id)
	}
	if res.Added+res.Updated+res.Done+res.Dropped > 0 {
		m.controlAudit(controlPendingItemRefresh, map[string]any{
			"path": res.Path, "rows": res.Rows, "added": res.Added, "updated": res.Updated,
			"done": res.Done, "dropped": res.Dropped, "kept": res.Kept,
			"actor": pendingItemRefreshActor, "error": res.Error,
		})
	}
	return st.remember(res)
}

// mergePendingRows applies rows to the registry and fills the counters in
// res. The ids of items moved to done are appended to released. A store error
// stops the merge and is returned.
func (m *Manager) mergePendingRows(rows []PendingItemSourceRow, src config.PendingItemSourceConfig, res *PendingItemRefreshResult, released *[]string) error {
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		if seen[row.Ref] {
			res.Skipped++ // the first row of a ref wins
			continue
		}
		seen[row.Ref] = true
		item, found := m.pendingItems.FindByRef(PendingItemSourceFile, row.Ref)
		if !found {
			if err := m.addPendingRow(row, res, released); err != nil {
				return err
			}
			continue
		}
		if err := m.updatePendingRow(item, row, res, released); err != nil {
			return err
		}
	}
	return m.handleMissingPendingRows(seen, src.OnMissingRow, res)
}

// addPendingRow creates the waiting item for a new row, and moves it straight
// to done when the row is already done. A title the registry refuses (over the
// length limit) skips the row.
func (m *Manager) addPendingRow(row PendingItemSourceRow, res *PendingItemRefreshResult, released *[]string) error {
	item, err := m.pendingItems.Create(row.Title, row.Ref, PendingItemSourceFile)
	if err != nil {
		if strings.HasPrefix(err.Error(), "400:") {
			res.Skipped++
			return nil
		}
		return fmt.Errorf("add pending item %q: %w", row.Ref, err)
	}
	res.Added++
	if !row.Done {
		return nil
	}
	if _, err := m.pendingItems.Update(item.ID, func(cur *PendingItem) error {
		cur.State = PendingItemDone
		return nil
	}); err != nil {
		return fmt.Errorf("mark pending item %q done: %w", row.Ref, err)
	}
	res.Done++
	*released = append(*released, item.ID)
	return nil
}

// updatePendingRow brings an existing file item in line with its row: a
// changed title is updated, and a done row moves a waiting or attached item
// to done. A done item is final and keeps its state.
func (m *Manager) updatePendingRow(item *PendingItem, row PendingItemSourceRow, res *PendingItemRefreshResult, released *[]string) error {
	titleChanged := false
	if title, err := normalizePendingItemTitle(row.Title); err != nil {
		res.Skipped++ // a title the registry refuses leaves the item as it is
		return nil
	} else if title != item.Title {
		titleChanged = true
	}
	moveDone := row.Done && item.State != PendingItemDone
	if !titleChanged && !moveDone {
		return nil
	}
	if _, err := m.pendingItems.Update(item.ID, func(cur *PendingItem) error {
		if titleChanged {
			cur.Title = row.Title
		}
		if moveDone && cur.State != PendingItemDone {
			cur.State = PendingItemDone
		} else {
			moveDone = false
		}
		return nil
	}); err != nil {
		return fmt.Errorf("update pending item %q: %w", row.Ref, err)
	}
	if titleChanged {
		res.Updated++
	}
	if moveDone {
		res.Done++
		*released = append(*released, item.ID)
	}
	return nil
}

// handleMissingPendingRows applies on_missing_row to every file item whose
// ref is not in seen. keep leaves them alone. drop deletes only a waiting
// item that is not a member of any Automation; every other vanished item is
// left in place. Both count into Kept when the item stays.
func (m *Manager) handleMissingPendingRows(seen map[string]bool, onMissing string, res *PendingItemRefreshResult) error {
	var members map[string]bool
	for _, item := range m.pendingItems.List(PendingItemFilter{Source: PendingItemSourceFile}) {
		if item.Ref == "" || seen[item.Ref] {
			continue
		}
		if onMissing != config.PendingItemOnMissingRowDrop || item.State != PendingItemWaiting {
			res.Kept++
			continue
		}
		if members == nil {
			members = m.pendingItemMemberIDs()
		}
		if members[item.ID] {
			res.Kept++
			continue
		}
		if err := m.pendingItems.Delete(item.ID); err != nil {
			return fmt.Errorf("drop pending item %q: %w", item.Ref, err)
		}
		res.Dropped++
	}
	return nil
}

// pendingItemMemberIDs returns the ids of every pending item that is a member
// or a held insert of any Automation, whatever its status.
func (m *Manager) pendingItemMemberIDs() map[string]bool {
	ids := map[string]bool{}
	if m.automations == nil {
		return ids
	}
	for _, a := range m.automations.List() {
		for _, mem := range a.Members {
			if mem.PendingItemID != "" {
				ids[mem.PendingItemID] = true
			}
		}
		for _, ins := range a.PendingInserts {
			if ins.PendingItemID != "" {
				ids[ins.PendingItemID] = true
			}
		}
	}
	return ids
}

// releasePendingHolds re-runs the hold step for every Automation that is
// blocked with the member of pending item itemID at its cursor, through the
// same resume path an attach uses, so the done item releases the hold and the
// Automation moves on. A member already converted to an Automaton member is
// left to the attach path, and a budget stop is left to the budget rules.
func (m *Manager) releasePendingHolds(itemID string) {
	if m.automations == nil {
		return
	}
	for _, a := range m.automations.List() {
		if !blockedOnPendingMember(a, itemID) {
			continue
		}
		if err := m.resumeAfterAttach(a.ID, pendingItemRefreshActor); err != nil {
			log.Printf("[autonomous] pending item %s is done but automation %s was not resumed: %v", itemID, a.ID, err)
		}
	}
}

// blockedOnPendingMember reports whether a is blocked on a pending item member
// of itemID that still waits for its Automaton (not converted by an attach).
func blockedOnPendingMember(a *Automation, itemID string) bool {
	if a == nil || a.Status != AutomationBlocked || budgetEpisodeOpen(a) {
		return false
	}
	for _, id := range liveCursorIDs(a) {
		mem := automationMember(a, id)
		if mem != nil && mem.kind() == MemberKindPending && mem.PendingItemID == itemID && memberUnstarted(mem) {
			return true
		}
	}
	return false
}
