package autonomous

import "time"

// MergeValidatorCheck is one named check of the release-bump validator: what
// it looked at, whether it passed, and a short detail for the operator.
type MergeValidatorCheck struct {
	Name   string `json:"name"`
	Pass   bool   `json:"pass"`
	Detail string `json:"detail"`
}

// MergeValidatorResult is the outcome of validating a release bump before a
// merge ships. Pass is true only when every check passed. Checks always holds
// all of the validator's checks, in the order the validator runs them.
type MergeValidatorResult struct {
	Pass      bool                  `json:"pass"`
	Version   string                `json:"version"`
	CheckedAt time.Time             `json:"checked_at"`
	Checks    []MergeValidatorCheck `json:"checks"`
}
