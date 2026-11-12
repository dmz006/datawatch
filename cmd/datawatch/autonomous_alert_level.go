package main

import alertspkg "github.com/dmz006/datawatch/internal/alerts"

// autonomousAlertLevel maps the autonomous manager's string alert level to
// an alerts-store level; unknown values fall back to warn.
func autonomousAlertLevel(level string) alertspkg.Level {
	switch level {
	case "info":
		return alertspkg.LevelInfo
	case "error":
		return alertspkg.LevelError
	default:
		return alertspkg.LevelWarn
	}
}
