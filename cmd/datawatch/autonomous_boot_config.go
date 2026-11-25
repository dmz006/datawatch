package main

// The engine's boot config is built field by field in main.go. A setting that
// list leaves out would read as zero after every restart, and the next
// autonomous config write would save that zero over the stored value. This file
// closes the gap for every setting at once, including ones added later.

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// carryAutonomousSettings fills every engine setting that is still unset in dst
// from the daemon setting of the same name in src, nested blocks included.
// Values dst already holds (boot defaults, normalized values) are kept. Filled
// values are fresh copies, so dst shares no map, slice or pointer with src. A
// setting that cannot be copied is reported in the error; the others are still
// filled.
func carryAutonomousSettings(dst *autonomouspkg.Config, src config.AutonomousConfig) error {
	raw, err := json.Marshal(src)
	if err != nil {
		return fmt.Errorf("autonomous settings: %w", err)
	}
	var stored map[string]json.RawMessage
	if err := json.Unmarshal(raw, &stored); err != nil {
		return fmt.Errorf("autonomous settings: %w", err)
	}
	return errors.Join(fillUnsetFields(reflect.ValueOf(dst).Elem(), stored, "autonomous")...)
}

func fillUnsetFields(v reflect.Value, stored map[string]json.RawMessage, path string) []error {
	var errs []error
	for i := 0; i < v.NumField(); i++ {
		field := v.Type().Field(i)
		key, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		raw, ok := stored[key]
		if !field.IsExported() || key == "" || key == "-" || !ok {
			continue
		}
		f := v.Field(i)
		if f.IsZero() {
			p := reflect.New(f.Type())
			if err := json.Unmarshal(raw, p.Interface()); err != nil {
				errs = append(errs, fmt.Errorf("%s.%s: %w", path, key, err))
				continue
			}
			f.Set(p.Elem())
			continue
		}
		if f.Kind() == reflect.Struct {
			var sub map[string]json.RawMessage
			if json.Unmarshal(raw, &sub) == nil {
				errs = append(errs, fillUnsetFields(f, sub, path+"."+key)...)
			}
		}
	}
	return errs
}
