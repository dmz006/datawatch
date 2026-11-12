package main

import (
	"strings"
	"testing"
)

// formatConfigGetOutput pins the `config get` display of the usage
// calculator's reset zone: when the operator left the zone unset the
// daemon's GET /api/config carries reset_zone_source "server" and the
// CLI marks the effective zone as following the server (the operator's
// own machine zone), never bare. An explicitly configured zone — or any
// other key — prints the value itself, unmarked.

func configGetBodyForZone(zone, source string, fellBack bool) map[string]interface{} {
	return map[string]interface{}{
		"usage": map[string]interface{}{
			"calculator": map[string]interface{}{
				"reset_zone":           zone,
				"reset_zone_source":    source,
				"reset_zone_fell_back": fellBack,
				"reset_weekday":        "sunday",
				"reset_time":           "08:00",
			},
		},
	}
}

func TestFormatConfigGetOutputServerZoneIsMarked(t *testing.T) {
	body := configGetBodyForZone("America/New_York", "server", false)
	out := formatConfigGetOutput(body, "usage.calculator.reset_zone", "America/New_York")
	if !strings.HasSuffix(out, "America/New_York (server zone)") {
		t.Fatalf("server zone must print with the (server zone) marker, got %q", out)
	}
}

func TestFormatConfigGetOutputConfiguredZoneIsNotMarked(t *testing.T) {
	body := configGetBodyForZone("Europe/Rome", "configured", false)
	// A configured zone prints through the same JSON formatting as every
	// other string key, but carries no (server zone) marker.
	if out := formatConfigGetOutput(body, "usage.calculator.reset_zone", "Europe/Rome"); out != `"Europe/Rome"` || strings.Contains(out, "server zone") {
		t.Fatalf("configured zone prints marked or unformatted, got %q", out)
	}
}

func TestFormatConfigGetOutputServerZoneFellBackStillMarked(t *testing.T) {
	// The daemon resolved no zone at all and UTC stood in: still a server
	// decision, still marked, so the operator knows to set usage.calculator.reset_zone.
	body := configGetBodyForZone("UTC", "server", true)
	if out := formatConfigGetOutput(body, "usage.calculator.reset_zone", "UTC"); out != "UTC (server zone)" {
		t.Fatalf("fallback server zone must also carry the marker, got %q", out)
	}
}

func TestFormatConfigGetOutputOtherKeysUnaffected(t *testing.T) {
	body := configGetBodyForZone("UTC", "server", false)
	if out := formatConfigGetOutput(body, "usage.calculator.reset_time", "08:00"); out != `"08:00"` {
		t.Fatalf("the zone marker must not leak onto other keys, got %q", out)
	}
	if out := formatConfigGetOutput(body, "usage.calculator.reset_zone", 3.5); out != "3.5" {
		t.Fatalf("a non-string zone value prints bare, got %q", out)
	}
}
