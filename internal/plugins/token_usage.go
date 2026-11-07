// Token usage (GET /api/plugins/{name}/tokens) — pure scan of a
// manifest's ${secret:...} references. Names and the manifest fields
// that carry the refs only; never a secret's value.

package plugins

import (
	"sort"

	"github.com/dmz006/datawatch/internal/secrets"
)

// SecretRefUsage is one secret reference a plugin manifest uses, plus
// every manifest field (dot-path) that references it.
type SecretRefUsage struct {
	Name   string   `json:"name"`
	Fields []string `json:"fields"`
}

// SecretRefs scans a manifest for ${secret:...} references in the fields
// the daemon resolves: connection.auth.token and each entry of the env
// map (BL242). Returns one entry per distinct secret name, sorted by
// name; each entry's fields are the dot-paths ("connection.auth.token",
// "env.<KEY>"), sorted. Empty when the manifest references no secrets.
// It never returns a secret's value.
func SecretRefs(m Manifest) []SecretRefUsage {
	byName := map[string][]string{}
	add := func(name, field string) {
		if name == "" || field == "" {
			return
		}
		for _, f := range byName[name] {
			if f == field {
				return
			}
		}
		byName[name] = append(byName[name], field)
	}

	if m.Connection != nil && m.Connection.Auth != nil {
		for _, n := range secrets.RefNames(m.Connection.Auth.Token) {
			add(n, "connection.auth.token")
		}
	}
	for k, v := range m.Env {
		for _, n := range secrets.RefNames(v) {
			add(n, "env."+k)
		}
	}

	out := make([]SecretRefUsage, 0, len(byName))
	for name, fields := range byName {
		sort.Strings(fields)
		out = append(out, SecretRefUsage{Name: name, Fields: fields})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
