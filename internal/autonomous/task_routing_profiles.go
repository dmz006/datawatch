package autonomous

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// TaskRoutingExportFormat is the version of the exported profile document.
const TaskRoutingExportFormat = 1

// TaskRoutingDocument is the exported form of one profile.
type TaskRoutingDocument struct {
	FormatVersion int                       `json:"format_version"`
	Name          string                    `json:"name"`
	Rows          config.TaskRoutingProfile `json:"rows"`
}

// ValidateModelBackendPair is the model and backend pairing check the
// routing table validation uses (the same rule the task and story LLM
// setters apply).
func ValidateModelBackendPair(backend, model string) error {
	return validateModelForBackend(backend, model)
}

// TaskRoutingChecksFor builds the validation lookups from a backend
// lookup (for example the LLM registry's Get).
func TaskRoutingChecksFor(known func(backend string) bool) config.TaskRoutingChecks {
	return config.TaskRoutingChecks{BackendKnown: known, ModelPairs: ValidateModelBackendPair}
}

func joinTaskRoutingErrors(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	parts := make([]string, len(errs))
	for i, e := range errs {
		parts[i] = e.Error()
	}
	return errors.New(strings.Join(parts, "; "))
}

// SaveProfile stores a profile under name and returns the new config. A
// nil rows copies the active profile's current table. The input config
// is never modified.
func SaveProfile(cfg config.TaskRoutingConfig, name string, rows config.TaskRoutingProfile) (config.TaskRoutingConfig, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return cfg, errors.New("profile name is required")
	}
	out := cfg.Clone()
	if rows == nil {
		rows = out.Profiles[out.ActiveProfile]
	}
	if out.Profiles == nil {
		out.Profiles = map[string]config.TaskRoutingProfile{}
	}
	out.Profiles[name] = rows.Clone()
	return out, nil
}

// SetActiveProfile switches the active profile. It changes routing only
// for tasks created afterwards; no existing task or session is touched.
func SetActiveProfile(cfg config.TaskRoutingConfig, name string) (config.TaskRoutingConfig, error) {
	if _, ok := cfg.Profiles[name]; !ok {
		return cfg, fmt.Errorf("profile %q does not exist", name)
	}
	out := cfg.Clone()
	out.ActiveProfile = name
	return out, nil
}

// DeleteProfile removes a profile. The active profile cannot be deleted.
func DeleteProfile(cfg config.TaskRoutingConfig, name string) (config.TaskRoutingConfig, error) {
	if _, ok := cfg.Profiles[name]; !ok {
		return cfg, fmt.Errorf("profile %q does not exist", name)
	}
	if name == cfg.ActiveProfile {
		return cfg, fmt.Errorf("profile %q is active and cannot be deleted", name)
	}
	out := cfg.Clone()
	delete(out.Profiles, name)
	return out, nil
}

// ListProfiles returns the profile names, sorted.
func ListProfiles(cfg config.TaskRoutingConfig) []string {
	names := make([]string, 0, len(cfg.Profiles))
	for n := range cfg.Profiles {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// ExportProfile returns a deterministic JSON document for a profile
// (map keys are sorted by encoding/json).
func ExportProfile(cfg config.TaskRoutingConfig, name string) ([]byte, error) {
	p, ok := cfg.Profiles[name]
	if !ok {
		return nil, fmt.Errorf("profile %q does not exist", name)
	}
	return json.MarshalIndent(TaskRoutingDocument{FormatVersion: TaskRoutingExportFormat, Name: name, Rows: p}, "", "  ")
}

// ImportProfile validates doc and stores it under name (the document's own
// name when name is empty). It refuses on any problem and returns the
// config unchanged. An existing profile is replaced only when overwrite
// is true.
func ImportProfile(cfg config.TaskRoutingConfig, doc []byte, name string, overwrite bool, chk config.TaskRoutingChecks) (config.TaskRoutingConfig, error) {
	var d TaskRoutingDocument
	if err := json.Unmarshal(doc, &d); err != nil {
		return cfg, fmt.Errorf("profile document is not valid: %w", err)
	}
	if d.FormatVersion != TaskRoutingExportFormat {
		return cfg, fmt.Errorf("profile document format version %d is not supported", d.FormatVersion)
	}
	if name == "" {
		name = d.Name
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return cfg, errors.New("profile name is required")
	}
	if _, exists := cfg.Profiles[name]; exists && !overwrite {
		return cfg, fmt.Errorf("profile %q already exists; import with overwrite to replace it", name)
	}
	out := cfg.Clone()
	if out.Profiles == nil {
		out.Profiles = map[string]config.TaskRoutingProfile{}
	}
	out.Profiles[name] = d.Rows.Clone()
	if err := joinTaskRoutingErrors(config.ValidateTaskRouting(out, chk)); err != nil {
		return cfg, err
	}
	return out, nil
}
