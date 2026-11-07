package config

import (
	"fmt"
	"strconv"
	"strings"
)

// MemoryLimitNone is the word that opts a project or task kind out of a
// memory ceiling.
const MemoryLimitNone = "none"

// ValidateMemoryLimit checks an optional memory_limit value: empty (use the
// default), the word "none", or a positive MB number. Zero, negative and
// any other text are rejected.
func ValidateMemoryLimit(v string) error {
	v = strings.TrimSpace(v)
	if v == "" || strings.EqualFold(v, MemoryLimitNone) {
		return nil
	}
	if n, err := strconv.Atoi(v); err != nil || n <= 0 {
		return fmt.Errorf("invalid memory_limit %q: want a positive MB number or %q", v, MemoryLimitNone)
	}
	return nil
}

// ValidateMemoryLimitByKind checks every value in a per-task-kind
// memory_limit table (autonomous.memory_limit_by_kind).
func ValidateMemoryLimitByKind(m map[string]string) error {
	for k, v := range m {
		if err := ValidateMemoryLimit(v); err != nil {
			return fmt.Errorf("autonomous.memory_limit_by_kind[%s]: %w", k, err)
		}
	}
	return nil
}
