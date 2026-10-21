package main

import (
	"testing"

	alertspkg "github.com/dmz006/datawatch/internal/alerts"
)

func TestAutonomousAlertLevel(t *testing.T) {
	cases := map[string]alertspkg.Level{
		"warn": alertspkg.LevelWarn, "info": alertspkg.LevelInfo,
		"error": alertspkg.LevelError, "": alertspkg.LevelWarn, "x": alertspkg.LevelWarn,
	}
	for in, want := range cases {
		if got := autonomousAlertLevel(in); got != want {
			t.Errorf("%q: got %v want %v", in, got, want)
		}
	}
}
