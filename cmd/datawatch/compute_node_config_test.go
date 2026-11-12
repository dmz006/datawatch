package main

import (
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestComputeNodeFromConfigCarriesTLSOptOut(t *testing.T) {
	if computeNodeFromConfig(config.ComputeNodeYAML{Name: "a"}).TLSInsecureSkipVerify {
		t.Fatal("verification must stay on by default")
	}
	n := computeNodeFromConfig(config.ComputeNodeYAML{Name: "a", TLSInsecureSkipVerify: true})
	if !n.TLSInsecureSkipVerify {
		t.Fatal("tls_insecure_skip_verify in config was not carried onto the node")
	}
}

func TestComputeNodeFromConfig(t *testing.T) {
	const fp = "ab:cd:ef:01:23:45:67:89:ab:cd:ef:01:23:45:67:89:ab:cd:ef:01:23:45:67:89:ab:cd:ef:01:23:45:67:89"
	cases := []struct {
		name     string
		in       config.ComputeNodeYAML
		wantCA   string
		wantFP   string
		wantSkip bool
	}{
		{name: "tls fields empty by default", in: config.ComputeNodeYAML{Name: "a"}},
		{name: "ca file mapped", in: config.ComputeNodeYAML{Name: "a", TLSCAFile: "/etc/datawatch/ca.pem"}, wantCA: "/etc/datawatch/ca.pem"},
		{name: "fingerprint mapped", in: config.ComputeNodeYAML{Name: "a", TLSCertFingerprint: fp}, wantFP: fp},
		{
			name:   "ca file and fingerprint together",
			in:     config.ComputeNodeYAML{Name: "a", TLSCAFile: "/etc/datawatch/ca.pem", TLSCertFingerprint: fp},
			wantCA: "/etc/datawatch/ca.pem", wantFP: fp,
		},
		{name: "skip verify mapped", in: config.ComputeNodeYAML{Name: "a", TLSInsecureSkipVerify: true}, wantSkip: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := computeNodeFromConfig(tc.in)
			if n.TLSCAFile != tc.wantCA {
				t.Errorf("TLSCAFile = %q, want %q", n.TLSCAFile, tc.wantCA)
			}
			if n.TLSCertFingerprint != tc.wantFP {
				t.Errorf("TLSCertFingerprint = %q, want %q", n.TLSCertFingerprint, tc.wantFP)
			}
			if n.TLSInsecureSkipVerify != tc.wantSkip {
				t.Errorf("TLSInsecureSkipVerify = %v, want %v", n.TLSInsecureSkipVerify, tc.wantSkip)
			}
		})
	}
}
