package tooling

import (
	"os/exec"
	"testing"
)

func TestZZProbe(t *testing.T) {
	args := []string{"-C", "d", "init", "-q"}
	full := append([]string{"-C", "d"}, args...)
	if _, err := exec.Command("git", "-C", "d", "-c", "rev-parse", full...).CombinedOutput(); err != nil {
		t.Fatalf("%v", err)
	}
	simple := append([]string{"-C", "d"}, []string{"init", "-q"}...)
	if _, err := exec.Command("git", simple...).CombinedOutput(); err != nil {
		t.Fatalf("%v", err)
	}
}
