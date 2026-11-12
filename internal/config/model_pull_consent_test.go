package config

import (
	"strings"
	"testing"
)

func TestValidateModelPullConsent(t *testing.T) {
	rows := []struct {
		name    string
		c       ModelPullConsent
		wantErr string // substring of the error; empty = must pass
	}{
		{
			name: "unset consent is valid",
		},
		{
			name: "one allowed model",
			c:    ModelPullConsent{Models: []ModelConsentEntry{{Name: "gpt-oss", Allowed: true}}},
		},
		{
			name: "refused entry with an operator note is valid",
			c:    ModelPullConsent{Models: []ModelConsentEntry{{Name: "phi3", Allowed: false, Note: "licensed only"}}},
		},
		{
			name: "model publisher hint is valid",
			c:    ModelPullConsent{Models: []ModelConsentEntry{{Name: "gpt-oss", Publisher: "openai", Allowed: true}}},
		},
		{
			name: "publishers only are valid",
			c:    ModelPullConsent{Publishers: []string{"org"}},
		},
		{
			name:    "empty model name is refused",
			c:       ModelPullConsent{Models: []ModelConsentEntry{{Name: "   ", Allowed: true}}},
			wantErr: "name must not be empty",
		},
		{
			name:    "exact duplicate model is refused",
			c:       ModelPullConsent{Models: []ModelConsentEntry{{Name: "gpt-oss", Allowed: true}, {Name: "gpt-oss", Allowed: false}}},
			wantErr: "duplicate entry",
		},
		{
			name:    "duplicate across case and tag is refused",
			c:       ModelPullConsent{Models: []ModelConsentEntry{{Name: "GPT-OSS:20b", Allowed: true}, {Name: "gpt-oss", Allowed: true}}},
			wantErr: "duplicate entry",
		},
		{
			name:    "empty publisher entry is refused",
			c:       ModelPullConsent{Publishers: []string{"org", " "}},
			wantErr: "publishers[1]",
		},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			err := ValidateModelPullConsent(row.c)
			if row.wantErr == "" {
				if err != nil {
					t.Fatalf("want valid, got: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), row.wantErr) {
				t.Fatalf("want error containing %q, got: %v", row.wantErr, err)
			}
		})
	}
}
