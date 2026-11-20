// Package packs embeds the default build supervisor pack so the daemon can
// serve it without the repository on disk.
package packs

import "embed"

// BuildSupervisor holds packs/build-supervisor.
//
//go:embed build-supervisor
var BuildSupervisor embed.FS
