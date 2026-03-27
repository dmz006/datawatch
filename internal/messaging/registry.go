package messaging

import (
	"fmt"
	"sort"
)

var registry = map[string]Backend{}

// Register adds a Backend to the registry.
func Register(b Backend) { registry[b.Name()] = b }

// Get retrieves a registered backend by name.
func Get(name string) (Backend, error) {
	b, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown messaging backend %q; available: %v", name, Names())
	}
	return b, nil
}

// Names returns sorted list of registered backend names.
func Names() []string {
	names := make([]string, 0, len(registry))
	for k := range registry {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}
