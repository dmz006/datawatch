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
