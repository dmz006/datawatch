// Command config-parity-keys prints every leaf configuration key of the
// Config struct in internal/config, one dotted path per line, sorted and
// de-duplicated. The names are the yaml tag names — the same keys that
// appear in config.yaml — so the output can be diffed against the keys
// reachable through the other config channels (REST, CLI, PWA, MCP) to
// prove parity.
//
// Run with: go run ./scripts/config-parity-keys
package main

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// ConfigKeys walks the fields of type t and returns the dotted
// yaml-key path of every leaf configuration field. The result is not
// sorted or de-duplicated here; callers do that (main and the tests).
//
// Rules:
//   - A field's key segment is the first comma-separated piece of its
//     yaml tag (options like "omitempty" are ignored).
//   - Fields tagged `yaml:"-"` do not exist in the configuration and
//     are skipped, together with their subtrees.
//   - Fields tagged `yaml:",inline"` and anonymous (embedded) struct
//     fields are flattened into the parent: their children inherit the
//     parent's key prefix.
//   - Struct values and pointers to structs are recursed into.
//   - Everything else — maps, slices, arrays, interfaces and scalars —
//     is one leaf key: the field's own path.
//   - Untagged fields that are neither embedded nor marked inline do
//     not appear in the configuration (yaml.v3 would serialize them by
//     Go field name, which is not a documented config key), and are
//     skipped; untagged fields of struct kind that do need a key
//     instead panic with a clear message, because that is a programming
//     error rather than a configuration decision.
func ConfigKeys(t reflect.Type, prefix string) []string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		// A leaf reached through a pointer: its key is the prefix.
		if prefix == "" {
			return nil
		}
		return []string{prefix}
	}

	keys := make([]string, 0, 8)
	for i := 0; i < t.NumField(); i++ {
		child := t.Field(i)
		if !child.IsExported() {
			continue
		}

		tag, _ := child.Tag.Lookup("yaml")
		parts := strings.Split(tag, ",")
		name := parts[0]
		inline := len(parts) > 1 && anyIn(parts[1:], "inline")

		if name == "-" {
			continue // yaml:"-" — not in the configuration
		}
		if name == "" && !inline && !child.Anonymous {
			// No tag at all on a named field: not a documented
			// config key.
			continue
		}

		ft := child.Type
		for ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}

		// Flatten conditions: anonymous embedded struct, or an
		// explicit inline tag on a struct (or pointer to struct).
		// Maps and other kinds stay one leaf even when inline: their
		// keys are not statically known.
		if ft.Kind() == reflect.Struct && (child.Anonymous || inline) {
			keys = append(keys, ConfigKeys(child.Type, prefix)...)
			continue
		}
		if ft.Kind() == reflect.Struct && name == "" {
			panic("config-parity-keys: untagged struct field " + child.Name + " without yaml:\",inline\" cannot be named")
		}

		path := name
		if prefix != "" {
			path = prefix + "." + name
		}
		if ft.Kind() == reflect.Struct {
			keys = append(keys, ConfigKeys(child.Type, path)...)
		} else {
			keys = append(keys, path)
		}
	}
	return keys
}

func anyIn(parts []string, want string) bool {
	for _, p := range parts {
		if p == want {
			return true
		}
	}
	return false
}

func main() {
	seen := make(map[string]bool)
	sorted := make([]string, 0)
	for _, k := range ConfigKeys(reflect.TypeOf(config.Config{}), "") {
		if !seen[k] {
			seen[k] = true
			sorted = append(sorted, k)
		}
	}
	sort.Strings(sorted)
	for _, k := range sorted {
		fmt.Println(k)
	}
}
