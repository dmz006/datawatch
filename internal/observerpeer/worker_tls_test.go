package observerpeer

import (
	"strings"
	"testing"
)

const (
	testPin    = "a1b2c3d4e5f6a7b8c9d0a1b2c3d4e5f6a7b8c9d0a1b2c3d4e5f6a7b8c9d0a1b2"
	testParent = "https://primary:8443"
)

func envGetenv(env map[string]string) func(string) string {
	return func(k string) string { return env[k] }
}

func TestWorkerTLSSettings(t *testing.T) {
	cases := []struct {
		name         string
		env          map[string]string
		parent       string
		wantPin      string
		wantInsecure bool
		wantSkipWarn bool
		wantNotice   bool
	}{
		{
			name:       "no env: https parent verifies via system store",
			env:        map[string]string{},
			parent:     testParent,
			wantNotice: true,
		},
		{
			name:    "pin set: pin returned, no warnings",
			env:     map[string]string{EnvParentCertFingerprint: testPin},
			parent:  testParent,
			wantPin: testPin,
		},
		{
			name:    "pin set with opt-out: pin wins, insecure off, no warnings",
			env:     map[string]string{EnvParentCertFingerprint: testPin, EnvPushInsecure: "true"},
			parent:  testParent,
			wantPin: testPin,
		},
		{
			name:         "insecure=1",
			env:          map[string]string{EnvPushInsecure: "1"},
			parent:       testParent,
			wantInsecure: true,
			wantSkipWarn: true,
		},
		{
			name:         "insecure=true",
			env:          map[string]string{EnvPushInsecure: "true"},
			parent:       testParent,
			wantInsecure: true,
			wantSkipWarn: true,
		},
		{
			name:         "insecure=yes",
			env:          map[string]string{EnvPushInsecure: "yes"},
			parent:       testParent,
			wantInsecure: true,
			wantSkipWarn: true,
		},
		{
			name:         "insecure=on",
			env:          map[string]string{EnvPushInsecure: "on"},
			parent:       testParent,
			wantInsecure: true,
			wantSkipWarn: true,
		},
		{
			name:         "insecure=TRUE (case-insensitive)",
			env:          map[string]string{EnvPushInsecure: "TRUE"},
			parent:       testParent,
			wantInsecure: true,
			wantSkipWarn: true,
		},
		{
			name:       "insecure=0: not truthy, falls back to system store",
			env:        map[string]string{EnvPushInsecure: "0"},
			parent:     testParent,
			wantNotice: true,
		},
		{
			name:       "insecure=false: not truthy",
			env:        map[string]string{EnvPushInsecure: "false"},
			parent:     testParent,
			wantNotice: true,
		},
		{
			name:       "insecure=no: not truthy",
			env:        map[string]string{EnvPushInsecure: "no"},
			parent:     testParent,
			wantNotice: true,
		},
		{
			name:       "insecure=off: not truthy",
			env:        map[string]string{EnvPushInsecure: "off"},
			parent:     testParent,
			wantNotice: true,
		},
		{
			name:   "http parent: no TLS notices",
			env:    map[string]string{},
			parent: "http://primary:8442",
		},
		{
			name:         "http parent with opt-out: insecure on, skip warning",
			env:          map[string]string{EnvPushInsecure: "on"},
			parent:       "http://primary:8442",
			wantInsecure: true,
			wantSkipWarn: true,
		},
		{
			name:    "pin over http parent: pin only",
			env:     map[string]string{EnvParentCertFingerprint: "  " + testPin + "  "},
			parent:  "http://primary:8442",
			wantPin: testPin,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pin, insecure, warnings := WorkerTLSSettings(envGetenv(tc.env), tc.parent)
			if pin != tc.wantPin {
				t.Errorf("pin = %q want %q", pin, tc.wantPin)
			}
			if insecure != tc.wantInsecure {
				t.Errorf("insecure = %v want %v", insecure, tc.wantInsecure)
			}
			joined := strings.Join(warnings, "\n")
			if tc.wantSkipWarn && !strings.Contains(joined, "SKIPPED") {
				t.Errorf("expected a visible skip-verification warning, got: %q", joined)
			}
			if tc.wantSkipWarn && !strings.Contains(joined, EnvParentCertFingerprint) {
				t.Errorf("skip warning should say how to pin instead, got: %q", joined)
			}
			if tc.wantNotice && !strings.Contains(joined, "system trust store") {
				t.Errorf("expected a system-store notice, got: %q", joined)
			}
			if tc.wantNotice && !strings.Contains(joined, EnvParentCertFingerprint) {
				t.Errorf("store notice should name the pin env var, got: %q", joined)
			}
			if tc.wantNotice && !strings.Contains(joined, EnvPushInsecure) {
				t.Errorf("store notice should name the opt-out env var, got: %q", joined)
			}
			if (tc.wantSkipWarn || tc.wantNotice) && !strings.Contains(joined, "primary") {
				t.Errorf("warning should name the parent host, got: %q", joined)
			}
			if !tc.wantSkipWarn && !tc.wantNotice && len(warnings) != 0 {
				t.Errorf("expected no warnings, got: %q", joined)
			}
		})
	}
}

func TestWorkerTLSSettings_WarningsNeverContainToken(t *testing.T) {
	const secret = "super-secret-token"
	cases := []struct {
		name   string
		env    map[string]string
		parent string
	}{
		{
			name:   "credentials in URL: skip warning",
			env:    map[string]string{EnvPushInsecure: "on"},
			parent: "https://user:" + secret + "@primary:8443",
		},
		{
			name:   "credentials in URL: store notice",
			env:    map[string]string{},
			parent: "https://user:" + secret + "@primary:8443",
		},
		{
			name:   "pin value that looks like a token is not echoed into warnings",
			env:    map[string]string{EnvParentCertFingerprint: secret, EnvPushInsecure: "on"},
			parent: testParent,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, warnings := WorkerTLSSettings(envGetenv(tc.env), tc.parent)
			for _, w := range warnings {
				if strings.Contains(w, secret) {
					t.Errorf("warning must not contain the token, got: %q", w)
				}
			}
		})
	}
}

func TestTruthiness(t *testing.T) {
	for _, v := range []string{"1", "true", "yes", "on", "TRUE", " On ", "YES"} {
		if !truthyEnv(v) {
			t.Errorf("truthyEnv(%q) = false, want true", v)
		}
	}
	for _, v := range []string{"", "0", "false", "no", "off", "2", "truly", "t"} {
		if truthyEnv(v) {
			t.Errorf("truthyEnv(%q) = true, want false", v)
		}
	}
}
