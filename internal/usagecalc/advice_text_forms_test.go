package usagecalc

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"
)

// The text forms travel in the advice body, so the PWA preview, the CLI and the comm channel
// draw the same words from one result.

func TestServiceAdviceCarriesTextForms(t *testing.T) {
	f := newServiceFixture(t, Settings{Reset: serviceReset}, nil)
	f.seedReading(t)
	res, err := f.svc.Advice(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Advice: %v", err)
	}
	if res.Text.CLI == "" || res.Text.Comm == "" {
		t.Fatalf("text forms are empty: %+v", res.Text)
	}
	if want := FormatCLI(*res); res.Text.CLI != want {
		t.Errorf("CLI form in the body differs from FormatCLI:\n%s\n--- want\n%s", res.Text.CLI, want)
	}
	if want := FormatComm(*res); res.Text.Comm != want {
		t.Errorf("comm form in the body differs from FormatComm:\n%s\n--- want\n%s", res.Text.Comm, want)
	}
	for _, line := range strings.Split(res.Text.Comm, "\n") {
		if n := utf8.RuneCountInString(line); n > CommWidth {
			t.Errorf("comm line is %d columns, over %d: %q", n, CommWidth, line)
		}
	}
}

func TestServiceAdviceTextFormsWithoutReading(t *testing.T) {
	f := newServiceFixture(t, Settings{Reset: serviceReset}, nil)
	res, err := f.svc.Advice(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Advice: %v", err)
	}
	if !strings.Contains(res.Text.CLI, "no weekly readings yet") {
		t.Errorf("CLI form without a reading: %q", res.Text.CLI)
	}
}
