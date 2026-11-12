// Package genartifacts renders the documents an Automaton generates from its
// change record (internal/changerecord.ChangeRecord): a gate report of counts,
// a changelog draft, a parity issue draft, testing tracker rows and a plan
// registry row.
//
// Each Kind has a text/template stored as an embedded default under
// templates/. A project may replace one by placing its own template at
// .datawatch/templates/<kind>.tmpl under the project root; a template that
// does not parse or run falls back to the default and the result carries a
// warning.
//
// Formats:
//
//   - GateReport: counts only (commands run, passed, failed, files changed,
//     packages touched, tests added, settings, routes, locale keys). No
//     prose and no command output.
//   - ChangelogDraft: an entry in the style of the top entries of
//     CHANGELOG.md. The heading carries the placeholder vX.Y.Z, never a fixed
//     version: the release version is taken at merge time as the next patch
//     above main.
//   - ParityDraft: markdown with the headings of docs/plans/parity-issues/*.md
//     (What changed, Settings to add to the mobile app, REST contract from
//     routes added, New locale keys with English values, Screens).
//   - TrackerRows: rows of the table in docs/testing-tracker.md, one per
//     surface touched.
//   - RegistryRow: a draft row for the table in docs/plans/README.md.
//
// Write puts a rendered document at its tracked path with a first-line
// marker. A person's edit is kept: Write skips a file that lacks the marker,
// carries the "<!-- edited -->" line, or whose body no longer matches the
// hash recorded in its sidecar when it was generated.
package genartifacts
