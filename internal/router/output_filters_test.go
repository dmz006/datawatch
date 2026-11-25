package router

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/session"
)

func TestParseAndFormatOutputFilters(t *testing.T) {
	for in, want := range map[string]string{"filter": "list", "filter list": "list", "Filter Disable ab12": "disable ab12"} {
		c := Parse(in)
		if c.Type != CmdOutputFilter || c.Text != want {
			t.Errorf("%q -> %s %q", in, c.Type, c.Text)
		}
	}
	out := formatOutputFilters([]session.FilterPattern{
		{ID: "ab12", Action: session.FilterActionSendInput, Pattern: "secret-pattern", DisabledReason: "why"},
		{ID: "cd34", Action: session.FilterActionAlert, Enabled: true},
	})
	if !strings.Contains(out, "ab12") || !strings.Contains(out, "TYPES INPUT") || !strings.Contains(out, "(why)") || strings.Contains(out, "secret-pattern") {
		t.Errorf("list:\n%s", out)
	}
	if strings.Count(out, "TYPES INPUT") != 1 {
		t.Errorf("only typing filters are marked:\n%s", out)
	}
}
