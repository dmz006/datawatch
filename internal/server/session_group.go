// Grouping read for the session list, GET /api/sessions?group_by=...
//
// The session list keeps returning the same rows it always has; grouping is
// a read over those rows. `group_by` picks the axis — a tag (narrowed by
// `tag_prefix`, e.g. `automation:` or `stage:`), a whole tag, the state, the
// node (hostname) or the project — and the rows are collected into groups
// carrying a key, a count and a per-state mix, while the response also
// carries the per-state totals. A session with several tags that match the
// axis lands in exactly one group (the first matching tag in sorted order
// wins) and keeps its other tags as chips; rows with no match go to the
// `ungrouped` group. On the automaton axis, sessions that carry an
// Automaton id nest under the Automaton and its story with a progress
// count, so one Automaton's workers, verifiers and planners form one block.
//
// The grouping itself is pure (GroupSessions takes only fields the list
// already returns, including the inherited Automaton tags), so it needs no
// extra store access. A request without `group_by` gets the unchanged
// response.

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/dmz006/datawatch/internal/session"
)

// The group_by axes handled by the grouping read.
const (
	groupAxisTag       = "tag"
	groupAxisState     = "state"
	groupAxisNode      = "node"
	groupAxisProject   = "project"
	groupAxisAutomaton = "automaton"
)

// groupUngroupedKey is the group rows with no match on the chosen axis go to.
const groupUngroupedKey = "ungrouped"

// groupRootStoryKey is the story block, on the automaton axis, that holds a
// session with an Automaton id but no story.
const groupRootStoryKey = "root"

// sessionGroupRow is one input row of the pure grouping: only fields the
// session list already returns, plus the Automaton ids on the session record
// where present.
type sessionGroupRow struct {
	ID            string
	FullID        string
	Name          string
	State         session.State
	Tags          []string
	InheritedTags []string
	Hostname      string
	ProjectDir    string
	// SystemTags are the placement system tags of the session (or, for an
	// Automaton task session, of its owning Automaton); the `project:` tag
	// names the datawatch project.
	SystemTags []string
	PRDID      string
	StoryID    string
	TaskID     string
}

// sessionGroupMember is one session inside a group or story block. Chips are
// the session's tags other than the one that placed it on the tag axis.
type sessionGroupMember struct {
	ID      string   `json:"id"`
	FullID  string   `json:"full_id"`
	Name    string   `json:"name,omitempty"`
	State   string   `json:"state"`
	StoryID string   `json:"story_id,omitempty"`
	TaskID  string   `json:"task_id,omitempty"`
	Chips   []string `json:"chips,omitempty"`
}

// sessionGroupStory is one story block inside an automaton group.
type sessionGroupStory struct {
	Key      string               `json:"key"`
	Count    int                  `json:"count"`
	StateMix map[string]int       `json:"state_mix"`
	Sessions []sessionGroupMember `json:"sessions"`
}

// sessionGroupProgress is the progress count of one automaton block: its
// finished sessions over its sessions.
type sessionGroupProgress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

// sessionGroup is one group of the grouped response.
type sessionGroup struct {
	Key      string                `json:"key"`
	Count    int                   `json:"count"`
	StateMix map[string]int        `json:"state_mix"`
	Sessions []sessionGroupMember  `json:"sessions,omitempty"` // non-automaton axes
	Stories  []sessionGroupStory   `json:"stories,omitempty"`  // automaton axis only
	Progress *sessionGroupProgress `json:"progress,omitempty"` // automaton axis only
}

// sessionGrouped is the grouped response for GET /api/sessions?group_by=...
type sessionGrouped struct {
	GroupBy     string         `json:"group_by"`
	TagPrefix   string         `json:"tag_prefix,omitempty"`
	Groups      []sessionGroup `json:"groups"`
	StateTotals map[string]int `json:"state_totals"`
	Total       int            `json:"total"`
}

// GroupSessions groups the rows the session list already returns on the
// chosen axis. axis is one of tag, state, node, project or automaton; a
// tagPrefix narrows the tag axis to matching tags. It is pure: it reads
// nothing beyond the rows and the two parameters.
func GroupSessions(rows []sessionGroupRow, axis, tagPrefix string) (sessionGrouped, error) {
	switch axis {
	case groupAxisTag, groupAxisState, groupAxisNode, groupAxisProject, groupAxisAutomaton:
	default:
		return sessionGrouped{}, fmt.Errorf("invalid group_by %q: want tag, state, node, project or automaton", axis)
	}
	tagPrefix = strings.TrimSpace(tagPrefix)
	out := sessionGrouped{GroupBy: axis, TagPrefix: tagPrefix, Groups: []sessionGroup{}, StateTotals: map[string]int{}}
	groups := map[string]*sessionGroup{}
	stories := map[string]map[string]*sessionGroupStory{} // group key -> story key
	progress := map[string]sessionGroupProgress{}

	for _, row := range rows {
		state := string(row.State)
		out.StateTotals[state]++
		out.Total++
		key, placement := groupAxisKey(axis, tagPrefix, row)
		grp := groups[key]
		if grp == nil {
			grp = &sessionGroup{Key: key, StateMix: map[string]int{}}
			groups[key] = grp
		}
		grp.Count++
		grp.StateMix[state]++
		member := sessionGroupMember{
			ID: row.ID, FullID: row.FullID, Name: row.Name, State: state,
			Chips: groupChips(row, placement),
		}
		if axis == groupAxisAutomaton {
			member.StoryID = row.StoryID
			member.TaskID = row.TaskID
			storyKey := row.StoryID
			if storyKey == "" {
				storyKey = groupRootStoryKey
			}
			block, ok := stories[key]
			if !ok {
				block = map[string]*sessionGroupStory{}
				stories[key] = block
			}
			story := block[storyKey]
			if story == nil {
				story = &sessionGroupStory{Key: storyKey, StateMix: map[string]int{}, Sessions: []sessionGroupMember{}}
				block[storyKey] = story
			}
			story.Count++
			story.StateMix[state]++
			story.Sessions = append(story.Sessions, member)
			p := progress[key]
			p.Total++
			if row.State.IsTerminal() {
				p.Done++
			}
			progress[key] = p
			continue
		}
		grp.Sessions = append(grp.Sessions, member)
	}

	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i] == groupUngroupedKey {
			return false
		}
		if keys[j] == groupUngroupedKey {
			return true
		}
		return keys[i] < keys[j]
	})
	for _, k := range keys {
		grp := *groups[k]
		if axis == groupAxisAutomaton {
			grp.Stories = sortedStories(stories[k])
			p := progress[k]
			grp.Progress = &p
		}
		out.Groups = append(out.Groups, grp)
	}
	return out, nil
}

// sortedStories returns the story blocks of one automaton group in
// deterministic order: the root block first, then the story ids.
func sortedStories(block map[string]*sessionGroupStory) []sessionGroupStory {
	keys := make([]string, 0, len(block))
	for k := range block {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i] == groupRootStoryKey {
			return true
		}
		if keys[j] == groupRootStoryKey {
			return false
		}
		return keys[i] < keys[j]
	})
	out := make([]sessionGroupStory, 0, len(keys))
	for _, k := range keys {
		out = append(out, *block[k])
	}
	return out
}

// groupAxisKey returns the group key a row lands in on the axis, plus the
// tag that placed it (tag axis only, for chip exclusion). Rows the axis does
// not cover go to the ungrouped group.
func groupAxisKey(axis, tagPrefix string, row sessionGroupRow) (string, string) {
	switch axis {
	case groupAxisState:
		return string(row.State), ""
	case groupAxisNode:
		if row.Hostname == "" {
			return groupUngroupedKey, ""
		}
		return row.Hostname, ""
	case groupAxisProject:
		return resolveGroupProject(row), ""
	case groupAxisAutomaton:
		if row.PRDID == "" {
			return groupUngroupedKey, ""
		}
		return row.PRDID, ""
	default: // tag axis
		tags := sortedGroupTags(row)
		if len(tags) == 0 {
			return groupUngroupedKey, ""
		}
		if tagPrefix == "" {
			return tags[0], tags[0]
		}
		for _, t := range tags { // already sorted: first match wins
			if strings.HasPrefix(t, tagPrefix) {
				return t, t
			}
		}
		return groupUngroupedKey, ""
	}
}

// resolveGroupProject returns the project-axis key for a row: the datawatch
// project name from the `project:` placement system tag first, then the
// session working folder when no project is known, then ungrouped. An
// Automaton task session therefore groups under its project, never under its
// worktree path.
func resolveGroupProject(row sessionGroupRow) string {
	for _, t := range row.SystemTags {
		if name := strings.TrimSpace(strings.TrimPrefix(t, "project:")); strings.HasPrefix(t, "project:") && name != "" {
			return name
		}
	}
	if row.ProjectDir != "" {
		return row.ProjectDir
	}
	return groupUngroupedKey
}

// sortedGroupTags is the deduped, sorted union of the row's own tags and its
// inherited tags — the tag axis and the member chips both read from it.
func sortedGroupTags(row sessionGroupRow) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, t := range append(append([]string{}, row.Tags...), row.InheritedTags...) {
		t = strings.TrimSpace(t)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// groupChips are the tags a member keeps apart from the one that placed it.
func groupChips(row sessionGroupRow, placement string) []string {
	out := []string{}
	for _, t := range sortedGroupTags(row) {
		if t == placement {
			continue
		}
		out = append(out, t)
	}
	return out
}

// writeSessionGrouped serves the grouped read over the same list the flat and
// summary views would serve, with no pagination: one request sees the whole
// picture of the groups.
func (s *Server) writeSessionGrouped(w http.ResponseWriter, r *http.Request, sessions []*session.Session, o sessionGroupOptions) {
	sort.SliceStable(sessions, func(i, j int) bool { return sessionLess(sessions[i], sessions[j]) })
	rows := make([]sessionGroupRow, 0, len(sessions))
	for _, sess := range sessions {
		var inherited []string
		if s.manager != nil {
			inherited = s.manager.InheritedTags(sess)
		}
		sysTags := sess.SystemTags
		if s.manager != nil && sess.PRDID != "" {
			if owner := s.manager.SystemTagsByPRD(sess.PRDID); len(owner) > 0 {
				sysTags = append(append([]string(nil), sysTags...), owner...)
			}
		}
		rows = append(rows, sessionGroupRow{
			ID: sess.ID, FullID: sess.FullID, Name: sess.Name, State: sess.State,
			Tags: sess.Tags, InheritedTags: inherited,
			Hostname: sess.Hostname, ProjectDir: sess.ProjectDir, SystemTags: sysTags,
			PRDID: sess.PRDID, StoryID: sess.StoryID, TaskID: sess.TaskID,
		})
	}
	grouped, err := GroupSessions(rows, o.axis, o.tagPrefix)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_ = json.NewEncoder(w).Encode(grouped)
}
