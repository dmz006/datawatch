// Package deploy runs the operator's configured post-ship deploy. It names no
// repository and no command: every command line comes from the operator's
// settings, and empty settings mean nothing runs.
//
// This part of the package covers the sequence up to the restart: snapshot the
// installed binary, build, drain running final-gate tasks, record the
// pre-restart state, persist it, install, and run the restart command.
package deploy
