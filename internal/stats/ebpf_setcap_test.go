//go:build linux

package stats

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/ebpfsetup"
)

// TestSetCapCommand_MatchesSudoersRule: the command `setup ebpf` runs and the
// command the printed sudoers rule allows come from one place; if one
// changes the other must too, or the passwordless re-apply stops working.
func TestSetCapCommand_MatchesSudoersRule(t *testing.T) {
	if _, err := ebpfsetup.FindSetcap(); err != nil {
		t.Skip("setcap not installed")
	}
	argv, err := setCapCommand("/opt/dw/bin/datawatch")
	if err != nil {
		t.Fatal(err)
	}
	if argv[0] != "sudo" || argv[1] != "-S" {
		t.Fatalf("argv %v", argv)
	}
	p, err := ebpfsetup.NewPlan("alice", argv[2], argv[4], "")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(argv[2:], " "), strings.Join(p.SetcapArgv(), " "); got != want {
		t.Errorf("applied %q, rule pins %q", got, want)
	}
	if argv[3] != "cap_bpf,cap_perfmon,cap_sys_resource=ep" {
		t.Errorf("capability argument %q", argv[3])
	}
}
