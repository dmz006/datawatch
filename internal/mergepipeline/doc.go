// Package mergepipeline describes the ordered steps that take a set of
// finished branches from "ready" to "shipped": checks, merge, version bump,
// changelog, tag, push, deploy and any project-specific hooks.
//
// It holds the pipeline description and the Runner that runs it (run.go):
//
//   - Definition and Step (definition.go): the typed pipeline and its
//     validation rules.
//   - DefaultDefinition (default.go): the built-in pipeline whose steps
//     reproduce the current merge flow, in the same order.
//   - LoadFile (file.go): reads a project's own pipeline from a YAML file in
//     its repository, with a typed error for a missing file versus a
//     malformed one and a cap on file size.
//
// Runner.Run merges branches through the pipeline the Resolver picks, applies
// the conflict policy when a merge step conflicts, and reports every step
// (step, start, end, result, source) to an autonomous.MergeRunRecorder, which
// the merge status shows as pipeline_runs. The tag, deploy and post-ship steps
// run only through a ShipRunner, so a test uses a fake and never a real remote.
//
// Nothing here is tied to a repository name, path or person. A project that
// has no pipeline file simply uses the default.
package mergepipeline
