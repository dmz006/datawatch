package router

import (
	"strings"
	"sync"
	"testing"
)

func TestCheckArchiveGraceValue(t *testing.T) {
	for _, bad := range []string{"-1", "-30", "1.5", "abc", ""} {
		if err := checkArchiveGraceValue(bad); err == nil {
			t.Errorf("%q accepted, want refusal", bad)
		}
	}
	for _, ok := range []string{"0", "90"} {
		if err := checkArchiveGraceValue(ok); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
}

// configureWrite refuses a bad value before the write function is called and
// passes 0 and 90 through.
func TestConfigureWriteArchiveGrace(t *testing.T) {
	var wrote []string
	r := &Router{hostname: "h"}
	r.SetConfigureFunc(func(key, value string) error {
		wrote = append(wrote, key+"="+value)
		return nil
	})
	if err := r.configureWrite(archiveGraceKey, "-1"); err == nil || !strings.Contains(err.Error(), "archive_grace_minutes") {
		t.Fatalf("negative: err = %v", err)
	}
	if len(wrote) != 0 {
		t.Fatalf("refused value was written: %v", wrote)
	}
	for _, v := range []string{"0", "90"} {
		if err := r.configureWrite(archiveGraceKey, v); err != nil {
			t.Fatalf("%s: %v", v, err)
		}
	}
	if len(wrote) != 2 {
		t.Fatalf("wrote %v, want two writes", wrote)
	}
}

func TestConfigureHelpMentionsArchiveGrace(t *testing.T) {
	var got string
	var wg sync.WaitGroup
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: func(s string) { got = s; wg.Done() }}}
	r.SetConfigureFunc(func(string, string) error { return nil })
	wg.Add(1)
	r.handleConfigure(Command{Type: CmdConfigure, Text: "list"})
	wg.Wait()
	if !strings.Contains(got, "autonomous.archive_grace_minutes") {
		t.Errorf("configure list lacks the key: %s", got)
	}
}
