// Command config-parity-keys prints every leaf configuration key of the
// Config struct (internal/config) as a dotted path, one per line, sorted
// and de-duplicated, using each field's yaml tag name (the same names that
// appear in config.yaml).
//
// Rules:
//   - fields tagged yaml:"-" are skipped
//   - fields tagged yaml:",inline" are flattened into the parent
//   - untagged embedded (anonymous) structs are flattened
//   - nested structs and pointers to structs are recursed into
//   - maps, slices and interfaces count as one leaf key (the field itself)
package main

import (
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// LeafKeys walks t (a struct type, or a pointer to one) and returns every
// leaf configuration key as a dotted path, sorted and de-duplicated.
// The rules are exactly the ones documented in the package comment above.
func LeafKeys(t reflect.Type) []string {
	seen := make(map[string]struct{})
	walk(t, "", &seen)
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// walk recursively collects leaf keys, prefixing every key under
// the dotted prefix.
func walk(t reflect.Type, prefix string, seen *map[string]struct{}) {
	t = deref(t)
	if t.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("yaml")
		name, inline := yamlName(tag, field)

		// Fields tagged yaml:"-" are ignored entirely (skipped above
		// already returns a special sentinel for that).
		ft := deref(field.Type)
		switch {
		case name == "-":
			continue
		case inline:
			// Inline fields (yaml:",inline" or an untagged embedded
			// struct) flatten into the parent: recurse with the same
			// prefix and contribute no key of their own.
			if ft.Kind() == reflect.Struct {
				walk(ft, prefix, seen)
				continue
			}
			// Inline non-struct is not a meaningful nesting target;
			// treat it as a single leaf under the parent prefix.
			addSeen(seen, prefix)
			continue
		}

		key := name
		if key == "" {
			key = strings.ToLower(field.Name) // yaml default for untagged fields
		}
		full := key
		if prefix != "" {
			full = prefix + "." + key
		}
		if ft.Kind() == reflect.Struct {
			walk(ft, full, seen)
			continue
		}
		// Scalars, maps, slices, arrays, interfaces, chans and funcs:
		// one leaf key (the field itself).
		addSeen(seen, full)
	}
}

// addSeen records path unless it is empty.
func addSeen(seen *map[string]struct{}, path string) {
	if path == "" {
		return
	}
	(*seen)[path] = struct{}{}
}

// deref follows pointer indirections to the underlying struct.
func deref(t reflect.Type) reflect.Type {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil {
		return reflect.TypeOf(0)
	}
	return t
}

// yamlName splits a yaml tag into its name and inline option.
// name is "-" for yaml:"-", and "" for untagged fields.
func yamlName(tag string, field reflect.StructField) (string, bool) {
	parts := strings.Split(tag, ",")
	name := parts[0]
	inline := false
	for _, opt := range parts[1:] {
		if opt == "inline" {
			inline = true
		}
	}
	// Untagged embedded (anonymous) structs flatten into the parent.
	if tag == "" && field.Anonymous {
		inline = true
	}
	return name, inline
}

func main() {
	keys := LeafKeys(reflect.TypeOf(config.Config{}))
	for _, k := range keys {
		fmt.Println(k)
	}
	os.Exit(0)
}
