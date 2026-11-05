package config

import (
	"fmt"
	"strings"
)

// Model-consent (autonomous.model_pull_consent) — the operator's
// allow-list that gates every model pull. A compute-node model pull or a
// marketplace task pull is refused unless this list explicitly allows the
// model, and the gate that reads it lives in internal/autonomous
// (autonomous.ModelPullConsentCheck), so config only owns the shape and
// the write-path validation here.

// ModelConsentEntry is one model the operator lists in the consent: its
// name, whose model it is, and whether it may be pulled.
type ModelConsentEntry struct {
	// Name is the model name as the registry addresses it, with or without
	// its ":tag" suffix (matching at the gate ignores case and any tag).
	Name string `yaml:"name" json:"name"`
	// Publisher names the model's publisher when the name alone doesn't
	// carry it. Optional; the gate prefers the publisher named on the
	// model itself over this hint.
	Publisher string `yaml:"publisher,omitempty" json:"publisher,omitempty"`
	// Allowed decides whether the pull is permitted. An entry with
	// Allowed false is an explicit refusal; it still counts as the model
	// being "listed" in the setup report.
	Allowed bool `yaml:"allowed" json:"allowed"`
	// Note is an operator free-text reason for the entry. Never validated,
	// surfaced as-is in reports.
	Note string `yaml:"note,omitempty" json:"note,omitempty"`
}

// ModelPullConsent is the operator's model-pull consent (the
// autonomous.model_pull_consent block). Two kinds of allow entries,
// either of which permits a pull:
//
//   - Models: model-specific entries — a listed model with Allowed true
//     may be pulled; a listed model with Allowed false is refused.
//   - Publishers: the publisher allow-list — one entry allows every model
//     of that publisher. A pull's publisher is the entry's own Publisher
//     when the model is listed, or the name's leading segment for
//     "publisher/model"-style names; plain names carry no publisher.
//
// Nothing is pulled without a matching allow entry: an unlisted model
// (and no matching publisher) is refused, and the refusal message tells
// the operator exactly which key to set.
type ModelPullConsent struct {
	// Models lists the models this setup needs, each with its allowed flag.
	// Unset (nil) means the operator has listed nothing — every pull is
	// refused until they do.
	Models []ModelConsentEntry `yaml:"models,omitempty" json:"models,omitempty"`
	// Publishers is the publisher allow-list; each entry allows every
	// model of that publisher. Unset (nil) allows no publisher.
	Publishers []string `yaml:"publishers,omitempty" json:"publishers,omitempty"`
}

// normModelConsentName lowers a model name and strips any ":tag" suffix,
// so matching at the gate is case-insensitive and tag-agnostic.
func normModelConsentName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if i := strings.IndexByte(name, ':'); i >= 0 {
		name = name[:i]
	}
	return name
}

// ValidateModelPullConsent rejects entries the gate couldn't interpret:
// empty model names, duplicate model names (case-insensitive, ignoring
// tags), and empty publisher entries. It runs on config load (see
// validateLoaded) and on every operator write path.
func ValidateModelPullConsent(c ModelPullConsent) error {
	seen := map[string]string{} // normalized name -> first-seen raw name
	for i, e := range c.Models {
		name := strings.TrimSpace(e.Name)
		if name == "" {
			return fmt.Errorf("autonomous.model_pull_consent.models[%d]: name must not be empty", i)
		}
		n := normModelConsentName(name)
		if first, dup := seen[n]; dup {
			return fmt.Errorf("autonomous.model_pull_consent.models: duplicate entry for model %q (already listed as %q)", name, first)
		}
		seen[n] = name
	}
	for i, p := range c.Publishers {
		if strings.TrimSpace(p) == "" {
			return fmt.Errorf("autonomous.model_pull_consent.publishers[%d]: must not be empty", i)
		}
	}
	return nil
}
