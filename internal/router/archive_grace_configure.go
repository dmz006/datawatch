package router

import "github.com/dmz006/datawatch/internal/config"

// archiveGraceKey is the configure key for the minutes a cancelled Automaton
// is kept before the supervisor loop archives it.
const archiveGraceKey = "autonomous.archive_grace_minutes"

// checkArchiveGraceValue refuses a value that is not a whole number of
// minutes >= 0, using the same parser as the REST write.
func checkArchiveGraceValue(value string) error {
	_, err := config.ParseArchiveGraceMinutes(value)
	return err
}
