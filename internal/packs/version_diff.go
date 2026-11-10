package packs

import (
	"sort"
	"time"
)

// VersionDiff lists what a candidate pack version adds over the version the
// operator already accepted. Each list is sorted and holds only additions.
type VersionDiff struct {
	NewTasks   []string `json:"new_tasks"`   // task titles
	NewScripts []string `json:"new_scripts"` // verification script paths
	NewTools   []string `json:"new_tools"`
	NewScopes  []string `json:"new_file_scopes"`
}

// Empty reports whether the candidate adds nothing.
func (d VersionDiff) Empty() bool {
	return len(d.NewTasks)+len(d.NewScripts)+len(d.NewTools)+len(d.NewScopes) == 0
}

// WidensAccess reports whether the diff adds a tool, script or file scope.
// New tasks alone do not widen access.
func (d VersionDiff) WidensAccess() bool {
	return len(d.NewScripts)+len(d.NewTools)+len(d.NewScopes) > 0
}

// DiffVersions compares candidate against accepted. A nil accepted pack means
// nothing was accepted before, so everything in candidate is new.
func DiffVersions(accepted, candidate *Pack) VersionDiff {
	var d VersionDiff
	if candidate == nil {
		return d
	}
	titles, scripts, tools, scopes := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	if accepted != nil {
		for _, t := range accepted.Tasks {
			titles[t.Title] = true
			if t.VerificationScript != "" {
				scripts[t.VerificationScript] = true
			}
			for _, x := range t.Tools {
				tools[x] = true
			}
			for _, x := range t.FileScopes {
				scopes[x] = true
			}
		}
	}
	add := func(seen map[string]bool, out *[]string, v string) {
		if v == "" || seen[v] {
			return
		}
		seen[v] = true
		*out = append(*out, v)
	}
	for _, t := range candidate.Tasks {
		add(titles, &d.NewTasks, t.Title)
		add(scripts, &d.NewScripts, t.VerificationScript)
		for _, x := range t.Tools {
			add(tools, &d.NewTools, x)
		}
		for _, x := range t.FileScopes {
			add(scopes, &d.NewScopes, x)
		}
	}
	sort.Strings(d.NewTasks)
	sort.Strings(d.NewScripts)
	sort.Strings(d.NewTools)
	sort.Strings(d.NewScopes)
	return d
}

// AutoAcceptUpdate records candidate as accepted when the source's
// AutoAcceptPacks flag is on and the diff against the accepted version adds no
// tools, scripts or file scopes. accepted_by is "auto:<source name>". It
// returns false with no error when auto-acceptance does not apply.
func AutoAcceptUpdate(store *TrustStore, accepted, candidate *Pack, sourceAutoAccept bool, sourceName string, at time.Time) (TrustRecord, bool, error) {
	if !sourceAutoAccept || sourceName == "" || accepted == nil || candidate == nil {
		return TrustRecord{}, false, nil
	}
	if DiffVersions(accepted, candidate).WidensAccess() {
		return TrustRecord{}, false, nil
	}
	rec, err := store.Accept(candidate, "auto:"+sourceName, at)
	if err != nil {
		return TrustRecord{}, false, err
	}
	return rec, true, nil
}
