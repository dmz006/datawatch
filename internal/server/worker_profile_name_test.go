package server

import (
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/workerprofile"
)

// TestValidateWorkerProfileName: an Automaton's worker profile name is
// accepted when it is empty (clears the override), built in, or user defined,
// and refused otherwise.
func TestValidateWorkerProfileName(t *testing.T) {
	cfg := &config.Config{}
	cfg.Autonomous.WorkerProfiles = map[string]workerprofile.WorkerProfile{"mine": {Name: "mine"}}
	cases := []struct {
		name    string
		wantErr bool
	}{
		{"", false},
		{"digest", false},
		{"datawatch-tools", false},
		{"mine", false},
		{"nope", true},
	}
	for _, c := range cases {
		err := validateWorkerProfileName(cfg, c.name)
		if (err != nil) != c.wantErr {
			t.Errorf("validateWorkerProfileName(%q) err=%v, wantErr=%v", c.name, err, c.wantErr)
		}
	}
	if err := validateWorkerProfileName(nil, "mine"); err == nil {
		t.Errorf("a user profile must not validate without a config")
	}
}
