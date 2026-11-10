package autonomous

import (
	"strings"
	"testing"
)

func TestReleasePushRefusal(t *testing.T) {
	const defaultBranch = "main"
	tests := []struct {
		name          string
		secretScan    string
		branch        string
		unattended    bool
		wantRefused   bool
		wantInMessage string
	}{
		{
			name:          "default branch unattended with shadow is refused",
			secretScan:    "shadow",
			branch:        defaultBranch,
			unattended:    true,
			wantRefused:   true,
			wantInMessage: "git.secret_scan.mode",
		},
		{
			name:          "default branch unattended with off is refused",
			secretScan:    "off",
			branch:        defaultBranch,
			unattended:    true,
			wantRefused:   true,
			wantInMessage: "git.secret_scan.mode",
		},
		{
			name:        "default branch unattended with enforce is allowed",
			secretScan:  "enforce",
			branch:      defaultBranch,
			unattended:  true,
			wantRefused: false,
		},
		{
			name:        "feature branch unattended is allowed",
			secretScan:  "off",
			branch:      "feature/x",
			unattended:  true,
			wantRefused: false,
		},
		{
			name:        "default branch attended is allowed",
			secretScan:  "off",
			branch:      defaultBranch,
			unattended:  false,
			wantRefused: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := releasePushRefusal(tc.secretScan, tc.branch, defaultBranch, tc.unattended)
			if tc.wantRefused {
				if err == nil {
					t.Fatalf("expected refusal, got nil error")
				}
				if !strings.Contains(err.Error(), tc.wantInMessage) {
					t.Fatalf("error %q does not name %q", err.Error(), tc.wantInMessage)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected allowed, got error: %v", err)
			}
		})
	}
}
