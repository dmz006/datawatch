// Package planrecords stores the plans-and-backlog records that used to live
// as numbered rows in docs/plans/README.md and as findings files: registry
// rows (planned or shipped work items) and findings (problems raised by an
// Automaton or an operator).
//
// The package is a pure file-backed store. It has no network, process or
// session logic: callers (REST, MCP, CLI, comm and the importer) sit on top.
//
// Identity. Every record has a short readable id, unique in the installation,
// and a uid (UUID) that never changes. New records never reuse a legacy row
// number as an id; the old number is kept in legacy_number so "row 12" still
// resolves. The title is the human handle and is shown first on every
// surface. A title is unique inside one project (case-insensitive, trimmed)
// and may repeat across projects. A rename keeps the old title as an alias
// that still resolves. Records are never deleted: closing is a state change.
//
// References. Resolve accepts, in this order: id, uid, "row <number>", exact
// title, an old alias, an unambiguous title prefix. An ambiguous reference
// returns *ErrAmbiguous carrying the candidate list so the caller can ask
// which one was meant.
//
// Storage. One JSON file per record under <data dir>/plan-records/<kind>/<uid>.json,
// written through a temp file and an atomic rename. The files are the source
// of truth; the in-memory index is rebuilt from them when the store opens.
//
// Plans. A plan is a registry record (title, goal, state, order) that links
// down plan -> Automation -> Automaton -> tasks. Kind "plan" carries Goal,
// Order (priority, lowest first) and the ordered lists Automations, Automata
// and Findings. A finding attaches to a plan or to an Automaton. The links
// change only through AttachAutomaton, AttachAutomation, AttachFinding, the
// Detach methods, ReorderPlans and ReorderAutomata. A link is never
// overwritten: attaching something already linked elsewhere returns
// *ErrLinkConflict. A summary index, kept in step on every write, answers
// PlanOf (Automaton or Automation to plan), Plans, Backlog (plans with no
// Automaton), ListSummaries and SearchTitles without reading a full record.
// The new fields are optional, so older records load unchanged.
//
// Secure mode. When a Sealer is supplied every record is sealed with the
// same XChaCha20-Poly1305 file sealing the other per-record data files use
// (internal/secfile). A sealed file keeps exactly these fields readable so
// records can be filtered and searched without the key: kind, project, state,
// id, uid, legacy_number, created_at, updated_at, closed_at. Title, body and
// every other field exist only inside the sealed payload.
package planrecords
