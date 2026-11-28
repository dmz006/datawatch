package server

// A tag filter takes a tag's name the same as its id. The names are turned into
// ids here, before any filter reads the query, so every read answers a name
// exactly as it answers the id. A tag nothing knows is reported instead of
// answering with an empty result.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
)

type tagFilterScope int

const (
	// tagFilterManaged reads match managed tags only (timeline, progress):
	// every value must be one.
	tagFilterManaged tagFilterScope = iota
	// tagFilterObject reads also match derived axis:value tags, which may
	// rightly match nothing.
	tagFilterObject
	// tagFilterTemplates is tagFilterObject plus the templates' own free tags.
	tagFilterTemplates
)

// builtinTagAxes are the axes datawatch derives axis:value tags on.
var builtinTagAxes = map[string]bool{"project": true, "automation": true, "automaton": true, "stage": true,
	"kind": true, "role": true, "lane": true, "compute": true, "peer": true, "backend": true}

// carriedTags is what the stored records carry.
type carriedTags struct {
	ids      map[string]bool // managed tag ids assigned to Automata
	system   map[string]bool // derived tags
	axes     map[string]bool // the axes of those derived tags (custom ones too)
	template map[string]bool // templates' own tags
}

func (c *carriedTags) addSystem(tags []string) {
	for _, t := range tags {
		c.system[t] = true
		if axis, _, ok := strings.Cut(t, ":"); ok {
			c.axes[axis] = true
		}
	}
}

func (s *Server) carriedTags() *carriedTags {
	c := &carriedTags{ids: map[string]bool{}, system: map[string]bool{}, axes: map[string]bool{}, template: map[string]bool{}}
	add := func(ids, system []string) {
		for _, id := range ids {
			c.ids[id] = true
		}
		c.addSystem(system)
	}
	if idx, ok := s.autonomousMgr.(prdSummaryIndex); ok {
		for _, e := range idx.SummaryIndexEntries() {
			add(e.TagIDs, e.SystemTags)
		}
	} else {
		for _, raw := range s.autonomousMgr.ListPRDs() {
			if p, err := toPRD(raw); err == nil && p != nil {
				add(p.TagIDs, p.SystemTags)
			}
		}
	}
	for _, raw := range s.autonomousMgr.ListTemplates() {
		if t, ok := raw.(*autonomous.Template); ok {
			c.addSystem(t.SystemTags)
			for _, tag := range t.Tags {
				c.template[tag] = true
			}
		}
	}
	for _, raw := range s.autonomousMgr.ListAutomations() {
		if a := rawAutomationOf(raw); a != nil {
			c.addSystem(a.SystemTags)
		}
	}
	return c
}

// registryTags is the managed tags by id, and the ids sharing each name.
func registryTags(api tagAPI) (byID map[string]*autonomous.Tag, byName map[string][]string) {
	byID, byName = map[string]*autonomous.Tag{}, map[string][]string{}
	for _, raw := range api.ListTags(false) {
		var t autonomous.Tag
		b, err := json.Marshal(raw)
		if err != nil || json.Unmarshal(b, &t) != nil || t.ID == "" {
			continue
		}
		byID[t.ID] = &t
		byName[t.Name] = append(byName[t.Name], t.ID)
	}
	return byID, byName
}

// resolveTagFilter rewrites the tag and tag_id values of r so each tag name
// reads as its id. It answers the request itself and reports false for a name
// several tags share, and for an unknown tag. On a managed-tag read any unknown
// value is refused. An object read refuses only when the unknown values leave
// the filter unable to match: with tag_match=all any of them, else all of them.
func (s *Server) resolveTagFilter(w http.ResponseWriter, r *http.Request, scope tagFilterScope) bool {
	q := r.URL.Query()
	if len(q["tag"]) == 0 && len(q["tag_id"]) == 0 {
		return true
	}
	api, ok := s.tagAPI()
	if !ok {
		return true
	}
	byID, byName := registryTags(api)
	var carried *carriedTags
	load := func() *carriedTags {
		if carried == nil {
			carried = s.carriedTags()
		}
		return carried
	}
	var unknown []string
	known, changed := 0, false
	// resolve returns the value the filter should read for ref.
	resolve := func(ref string) (string, error) {
		if byID[ref] != nil {
			known++
			return ref, nil
		}
		named := byName[ref]
		axis, value, isAxisForm := strings.Cut(ref, ":")
		isAxisForm = isAxisForm && value != ""
		if len(named) == 0 {
			if scope != tagFilterManaged && isAxisForm && builtinTagAxes[axis] {
				known++
				return ref, nil
			}
			c := load()
			if c.ids[ref] || (scope != tagFilterManaged && (c.system[ref] || (isAxisForm && c.axes[axis]))) ||
				(scope == tagFilterTemplates && c.template[ref]) {
				known++
				return ref, nil
			}
			unknown = append(unknown, strconv.Quote(ref))
			return ref, nil
		}
		// A name that records also carry as their own tag stays as written.
		if scope != tagFilterManaged && (load().system[ref] || (scope == tagFilterTemplates && load().template[ref])) {
			known++
			return ref, nil
		}
		if len(named) > 1 {
			sort.Strings(named)
			return "", fmt.Errorf("%w: %d tags are named %q; use the tag id: %s",
				autonomous.ErrTagInvalid, len(named), ref, strings.Join(named, ", "))
		}
		known++
		changed = true
		return named[0], nil
	}
	for _, key := range []string{"tag", "tag_id"} {
		for i, v := range q[key] {
			parts := strings.Split(v, ",")
			for j, part := range parts {
				ref := strings.TrimSpace(part)
				if ref == "" {
					continue
				}
				out, err := resolve(ref)
				if err != nil {
					writeTagError(w, err)
					return false
				}
				parts[j] = out
			}
			q[key][i] = strings.Join(parts, ",")
		}
	}
	// The axis shortcuts name derived tags too, so they count as known.
	if scope != tagFilterManaged {
		for _, name := range []string{"project", "automation", "automaton"} {
			if q.Get(name) != "" {
				known++
			}
		}
	}
	refuse := len(unknown) > 0
	if scope != tagFilterManaged && q.Get("tag_match") != autonomous.TagMatchAll {
		refuse = refuse && known == 0
	}
	if refuse {
		writeTagError(w, fmt.Errorf("%w: %s", autonomous.ErrTagNotFound, strings.Join(unknown, ", ")))
		return false
	}
	if changed {
		u := *r.URL
		u.RawQuery = q.Encode()
		r.URL = &u
	}
	return true
}
