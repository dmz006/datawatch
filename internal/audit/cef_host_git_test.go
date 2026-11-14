package audit

import "testing"

func TestCEFSignatureHostGitOperation(t *testing.T) {
	id, name, sev := cefSignature(ActionHostGitOperation)
	if id != 84 || name != "HostGitOperation" || sev != 3 {
		t.Fatalf("got (%d, %q, %d), want (84, HostGitOperation, 3)", id, name, sev)
	}
}

func TestCEFEscapeHostGitOperationValue(t *testing.T) {
	in := `a|b=c\d`
	if got, want := cefHeaderEscape(in), `a\|b=c\\d`; got != want {
		t.Errorf("header escape = %q, want %q", got, want)
	}
	if got, want := cefExtEscape(in), `a|b\=c\\d`; got != want {
		t.Errorf("ext escape = %q, want %q", got, want)
	}
}
