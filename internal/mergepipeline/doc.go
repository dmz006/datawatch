// Package mergepipeline describes the ordered steps that take a set of
// finished branches from "ready" to "shipped": checks, merge, version bump,
// changelog, tag, push, deploy and any project-specific hooks.
//
// It holds three things and runs none of them:
//
//   - Definition and Step (definition.go): the typed pipeline and its
//     validation rules.
//   - DefaultDefinition (default.go): the built-in pipeline whose steps
//     reproduce the current merge flow, in the same order.
//   - LoadFile (file.go): reads a project's own pipeline from a YAML file in
//     its repository, with a typed error for a missing file versus a
//     malformed one and a cap on file size.
//
// Nothing here is tied to a repository name, path or person. A project that
// has no pipeline file simply uses the default.
package mergepipeline
