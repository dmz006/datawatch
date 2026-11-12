// Server side of the usage.billing config keys: PUT /api/config validation
// and the applyConfigPatch fallback for
// usage.billing.<provider>.<field>. The rules live in
// internal/config/billing_profile.go (validateBillingProfiles); this file
// only checks one key's patch value at a time and stores it in
// cfg.Usage.Billing, so a change is live the moment it is written — the
// cost report and the usage calculator read the map directly, which is
// why this surface is a config patch rather than a dedicated endpoint.
// The provider map stays the only storage of the profile: nothing is
// copied into plan_tier_allocation_usd or any other block.

package server

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

const billingProfileKeyPrefix = "usage.billing."

// billingProfilesForDisplay renders the provider map for GET /api/config's
// usage.billing block: the same content, but always a non-nil map so a
// daemon with no profile set shows {} instead of null.
func billingProfilesForDisplay(m map[string]config.BillingProfile) map[string]interface{} {
	out := map[string]interface{}{}
	for provider, p := range m {
		out[provider] = p
	}
	return out
}

// billingProfileFields is every settable usage.billing.<provider>.<field> field.
var billingProfileFields = []string{
	"mode", "plan", "monthly_price", "currency", "reset_weekday", "reset_time", "allowance_unit",
}

// billingModes is the allowed mode vocabulary, in the order the error
// message spells it out (mirrors validateBillingProfiles).
var billingModes = []string{config.BillingModeBundledPlan, config.BillingModeAPIMetered, config.BillingModeFreeLocal}

// billingUnits is the allowed allowance_unit vocabulary (same order).
var billingUnits = []string{config.BillingUnitPercent, config.BillingUnitRequests, config.BillingUnitTokens}

// billingProfileSplit splits usage.billing.<provider>.<field> into its
// provider (the key of the cfg.Usage.Billing map — open vocabulary, only
// required to be non-empty and free of dots) and field (one of
// billingProfileFields).
func billingProfileSplit(k string) (provider, field string, ok bool) {
	rest := strings.TrimPrefix(k, billingProfileKeyPrefix)
	if rest == k || strings.Count(rest, ".") != 1 {
		return "", "", false
	}
	provider, field, _ = strings.Cut(rest, ".")
	if strings.TrimSpace(provider) == "" {
		return "", "", false
	}
	for _, f := range billingProfileFields {
		if f == field {
			return provider, field, true
		}
	}
	return "", "", false
}

// billingProfilePatchString coerces a patch value to the trimmed string it
// stores. An explicit JSON null clears the field (empty string); numbers,
// booleans and objects are refused rather than converted, so a typo'd
// monthly_price stays loud instead of silently becoming "12".
func billingProfilePatchString(field string, v interface{}) (string, error) {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x), nil
	case nil:
		return "", nil
	}
	return "", fmt.Errorf("%s: must be a string (or null to clear)", field)
}

// billingProfilePatchNumber coerces a patch value to a finite float >= 0
// (the same tolerance as burnGuardPatchNumber: a number, or a numeric
// string). 0 stays representable: it means "free plan", which is the only
// way to clear the price back down without a whole-file rewrite.
func billingProfilePatchNumber(field string, v interface{}) (float64, error) {
	msg := field + ": must be a finite number >= 0"
	err := errors.New(msg)
	var f float64
	switch x := v.(type) {
	case float64:
		f = x
	case int:
		f = float64(x)
	case int64:
		f = float64(x)
	case nil:
		return 0, nil
	case string:
		t := strings.TrimSpace(x)
		if t == "" {
			return 0, nil
		}
		if _, scanErr := fmt.Sscanf(t, "%g", &f); scanErr != nil {
			return 0, err
		}
	default:
		return 0, err
	}
	if math.IsNaN(f) || math.IsInf(f, 0) || f < 0 {
		return 0, err
	}
	return f, nil
}

// validateBillingProfileField checks one recognized field's patch value.
// An empty string (or null) is accepted as "clear this field back to the
// default" — matching how the profile's empty fields resolve (Mode "" →
// api_metered, AllowanceUnit "" → percent, ResetWeekday/ResetTime "" →
// none) — so clearing and setting use the same PUT shape.
func validateBillingProfileField(field string, v interface{}) error {
	switch field {
	case "mode":
		s, err := billingProfilePatchString(field, v)
		if err != nil {
			return err
		}
		if s == "" {
			return nil
		}
		s = strings.ToLower(s)
		for _, m := range billingModes {
			if m == s {
				return nil
			}
		}
		return fmt.Errorf("mode: unknown billing mode %q (allowed: %s, %s, %s)",
			s, billingModes[0], billingModes[1], billingModes[2])
	case "plan", "currency":
		if _, err := billingProfilePatchString(field, v); err != nil {
			return err
		}
		return nil
	case "monthly_price":
		if _, err := billingProfilePatchNumber(field, v); err != nil {
			return err
		}
		return nil
	case "reset_weekday":
		s, err := billingProfilePatchString(field, v)
		if err != nil {
			return err
		}
		if s == "" {
			return nil
		}
		return config.ValidateUsageCalculatorResetWeekday(strings.ToLower(s))
	case "reset_time":
		s, err := billingProfilePatchString(field, v)
		if err != nil {
			return err
		}
		if s == "" {
			return nil
		}
		return config.ValidateUsageCalculatorResetTime(s)
	case "allowance_unit":
		s, err := billingProfilePatchString(field, v)
		if err != nil {
			return err
		}
		if s == "" {
			return nil
		}
		s = strings.ToLower(s)
		for _, u := range billingUnits {
			if u == s {
				return nil
			}
		}
		return fmt.Errorf("allowance_unit: unknown allowance unit %q (allowed: %s, %s, %s)",
			s, billingUnits[0], billingUnits[1], billingUnits[2])
	}
	return fmt.Errorf("unknown field %q (allowed: %s)", field, strings.Join(billingProfileFields, ", "))
}

// validateBillingPatch checks every usage.billing.* key in a PUT /api/config
// body before anything is applied. A key that is not exactly
// usage.billing.<provider>.<field> — unknown field, an extra dot, an empty
// provider — is rejected, the same way every other usage.* write surface
// names the offending key in a 400.
func validateBillingPatch(patch map[string]interface{}) error {
	for k, v := range patch {
		if !strings.HasPrefix(k, billingProfileKeyPrefix) {
			continue
		}
		_, field, ok := billingProfileSplit(k)
		if !ok {
			return fmt.Errorf("%s: key must be usage.billing.<provider>.<field> with a non-empty provider and a known field (allowed fields: %s)",
				k, strings.Join(billingProfileFields, ", "))
		}
		if err := validateBillingProfileField(field, v); err != nil {
			return fmt.Errorf("%s: %v", k, err)
		}
	}
	return nil
}

// applyBillingPatchKey stores one usage.billing.<provider>.<field> patch value in
// cfg.Usage.Billing. It re-validates the value so a direct applyConfigPatch
// caller (not just handlePutConfig) cannot store a malformed one — the same
// refuse-quietly-here, refuse-loudly-in-PUT discipline as the sibling
// usage.* apply paths.
//
// Returns true when the key belongs to the usage.billing.* surface, so the
// applyConfigPatch default arm stops after this one call (including
// recognized-but-invalid values, which handlePutConfig has already rejected
// as a 400 before apply runs); a key that fails its split is not a billing
// key and is left to the rest of the default arm.
func applyBillingPatchKey(cfg *config.Config, k string, v interface{}) bool {
	provider, field, ok := billingProfileSplit(k)
	if !ok {
		return false
	}
	if err := validateBillingProfileField(field, v); err != nil {
		return true
	}
	if cfg.Usage.Billing == nil {
		cfg.Usage.Billing = map[string]config.BillingProfile{}
	}
	p := cfg.Usage.Billing[provider]
	switch field {
	case "mode":
		s, _ := billingProfilePatchString(field, v)
		p.Mode = strings.ToLower(s)
	case "plan":
		s, _ := billingProfilePatchString(field, v)
		p.Plan = s
	case "currency":
		s, _ := billingProfilePatchString(field, v)
		p.Currency = strings.ToUpper(s)
	case "monthly_price":
		f, _ := billingProfilePatchNumber(field, v)
		p.MonthlyPrice = f
	case "reset_weekday":
		s, _ := billingProfilePatchString(field, v)
		p.ResetWeekday = strings.ToLower(s)
	case "reset_time":
		s, _ := billingProfilePatchString(field, v)
		p.ResetTime = s
	case "allowance_unit":
		s, _ := billingProfilePatchString(field, v)
		p.AllowanceUnit = strings.ToLower(s)
	}
	cfg.Usage.Billing[provider] = p
	return true
}
