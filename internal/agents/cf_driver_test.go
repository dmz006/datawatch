// F10 sprint 8 (S8.5) — CF driver stub tests.

package agents

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCFDriver_KindIsCF(t *testing.T) {
	if got := NewCFDriver().Kind(); got != "cf" {
		t.Errorf("Kind=%q want cf", got)
	}
}

func TestCFDriver_AllMethodsReturnNotImplemented(t *testing.T) {
	d := NewCFDriver()
	if err := d.Spawn(context.Background(), &Agent{}); !errors.Is(err, ErrCFNotImplemented) {
		t.Errorf("Spawn err=%v want ErrCFNotImplemented", err)
	}
	if _, err := d.Status(context.Background(), &Agent{}); !errors.Is(err, ErrCFNotImplemented) {
		t.Errorf("Status err=%v want ErrCFNotImplemented", err)
	}
	if _, err := d.Logs(context.Background(), &Agent{}, 10); !errors.Is(err, ErrCFNotImplemented) {
		t.Errorf("Logs err=%v want ErrCFNotImplemented", err)
	}
	if err := d.Terminate(context.Background(), &Agent{}); !errors.Is(err, ErrCFNotImplemented) {
		t.Errorf("Terminate err=%v want ErrCFNotImplemented", err)
	}
}

// ErrCFNotImplemented message names the deferred-work backlog item
// so operators have a breadcrumb to follow.
func TestCFDriver_ErrorMessageNamesPlanItem(t *testing.T) {
	got := ErrCFNotImplemented.Error()
	for _, want := range []string{"S8.5", "ephemeral-agents.md", "kind=cf"} {
		if !strings.Contains(got, want) {
			t.Errorf("error message missing %q:\n%s", want, got)
		}
	}
}
