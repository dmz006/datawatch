// BL242 Phase 4 — ${secret:name} reference resolution.
//
// Any string field in the daemon config or in a project profile's env map
// may contain one or more ${secret:name} tokens. ResolveConfig walks the
// config struct via reflection and replaces every token in-place at daemon
// startup. ResolveMap does the same for a flat map[string]string (used for
// per-project env vars at spawn time).

package secrets

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
)

// secretRefRe matches ${secret:name} tokens. Name chars: alphanumeric, _, -, .
var secretRefRe = regexp.MustCompile(`\$\{secret:([a-zA-Z0-9_.\-]+)\}`)

// ResolveRef replaces all ${secret:name} tokens in s with values from store.
// Missing secrets are returned as errors; the original token is left in the
// output so the caller can decide whether to continue or abort.
func ResolveRef(s string, store Store) (string, error) {
	var firstErr error
	result := secretRefRe.ReplaceAllStringFunc(s, func(match string) string {
		if firstErr != nil {
			return match
		}
		name := secretRefRe.FindStringSubmatch(match)[1]
		sec, err := store.Get(name)
		if err != nil {
			firstErr = fmt.Errorf("%q: %w", name, err)
			return match
		}
		return sec.Value
	})
	return result, firstErr
}

// ResolveRefAs is like ResolveRef but enforces caller scope for each resolved
// secret. Returns ErrScopeDenied (wrapping the secret name) if any ref is
// denied for caller. Stops on the first denied or missing secret.
// Returns s unchanged when no ${secret:name} tokens are present.
func ResolveRefAs(s string, store Store, caller CallerCtx) (string, error) {
	if !secretRefRe.MatchString(s) {
		return s, nil
	}
	var resolveErr error
	result := secretRefRe.ReplaceAllStringFunc(s, func(match string) string {
		if resolveErr != nil {
			return match
		}
		name := secretRefRe.FindStringSubmatch(match)[1]
		sec, err := store.Get(name)
		if err != nil {
			resolveErr = fmt.Errorf("%q: %w", name, err)
			return match
		}
		if err := CheckScope(sec, caller); err != nil {
			resolveErr = fmt.Errorf("%q: %w", name, err)
			return match
		}
		return sec.Value
	})
	if resolveErr != nil {
		return s, resolveErr
	}
	return result, nil
}

// ResolveMapRefs resolves ${secret:name} tokens in every value of m and
// returns a new map (m is not mutated). Resolution continues past individual
// errors; all errors are joined and returned together.
func ResolveMapRefs(m map[string]string, store Store) (map[string]string, error) {
	out := make(map[string]string, len(m))
	var errs []string
	for k, v := range m {
		resolved, err := ResolveRef(v, store)
		if err != nil {
			errs = append(errs, fmt.Sprintf("env[%s]: %v", k, err))
			resolved = v
		}
		out[k] = resolved
	}
	if len(errs) > 0 {
		return out, fmt.Errorf("resolve map: %s", strings.Join(errs, "; "))
	}
	return out, nil
}

// ResolveConfig walks all exported string fields of v recursively (structs,
// pointers, slices, and map[string]string values) and resolves
// ${secret:name} tokens in-place. v must be a non-nil pointer to a struct.
// Errors from missing secrets are accumulated and returned joined.
func ResolveConfig(v any, store Store) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return fmt.Errorf("ResolveConfig: requires a non-nil pointer to struct")
	}
	var errs []string
	walkValue(rv.Elem(), store, &errs)
	if len(errs) > 0 {
		return fmt.Errorf("config secret refs: %s", strings.Join(errs, "; "))
	}
	return nil
}

func walkValue(v reflect.Value, store Store, errs *[]string) {
	switch v.Kind() {
	case reflect.String:
		if !v.CanSet() || !secretRefRe.MatchString(v.String()) {
			return
		}
		resolved, err := ResolveRef(v.String(), store)
		if err != nil {
			*errs = append(*errs, err.Error())
			return
		}
		v.SetString(resolved)

	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			walkValue(v.Field(i), store, errs)
		}

	case reflect.Ptr:
		if !v.IsNil() {
			walkValue(v.Elem(), store, errs)
		}

	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			walkValue(v.Index(i), store, errs)
		}

	case reflect.Map:
		// Only handle map[string]string — other map types are skipped.
		if v.Type().Key().Kind() != reflect.String || v.Type().Elem().Kind() != reflect.String {
			return
		}
		for _, key := range v.MapKeys() {
			val := v.MapIndex(key).String()
			if !secretRefRe.MatchString(val) {
				continue
			}
			resolved, err := ResolveRef(val, store)
			if err != nil {
				*errs = append(*errs, fmt.Sprintf("map[%s]: %v", key.String(), err))
				continue
			}
			v.SetMapIndex(key, reflect.ValueOf(resolved))
		}
	}
}
