// Package builtinpacks embeds the built-in standard-tasks pack folder. The
// folder under builtin/ holds the pack's manifest, prompts, verification
// scripts and Automaton templates; FS exposes it as an embed.FS so packages
// that import this one never need the folder on disk at build time.
package builtinpacks

import "embed"

// FS is the embedded built-in pack folder. Its root is "builtin/":
// builtin/pack.yaml, builtin/prompts/, builtin/verify/ and
// builtin/templates/. Use fs.Sub(FS, "builtin") to treat the pack folder
// itself as the FS root.
//
//go:embed builtin
var FS embed.FS
