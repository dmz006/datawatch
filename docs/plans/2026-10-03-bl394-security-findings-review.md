# BL394 — Dependabot + Code Scanning findings review

**Date:** 2026-10-03
**Status:** Started as analysis-only, per explicit instruction mid-review
("don't just fix things or change code, this is meant to be an analysis
and review and I guide all decisions") — every item needed an operator
go-ahead before any action. The operator has since authorized specific
items individually as the review progressed: all 74 confirmed false
positives were dismissed on CodeQL with documented reasons (§3, no code
changed for these); the `push.go` SSRF (§3e, v8.39.3), the council/
skills path-traversal pair (§3b, v8.39.4), the webhook arbitrary
local-file-read (§3b #4, v8.39.5), and the `cliPrompt` secret echo
(§3d, v8.39.6) are fixed. The email-injection finding (§3f, v8.39.7)
turned out, on live verification while fixing it, to have been
mischaracterized — not a live gap, see §3f for the full correction;
hardened anyway as defense in depth. The client-side prototype-
pollution finding (§3h, v8.39.8) is also fixed, with a from-scratch
Node-based regression test since no JS test framework existed for
this PWA. The reflected XSS in the docs viewer (§3h, v8.39.9) is also
fixed — and turned out to have 3 more unescaped sites beyond the 2
originally identified, found while implementing the fix, not before.
A follow-up pass (v8.39.10) then: fixed the two §3b items this doc had
previously left as "not independently re-verified" (both turned out to
be real, query-param-reachable traversal bugs — see the §3b addendum);
fixed a second, more subtle round of app.js escaping bugs found while
re-reading §3h (see the §3h addendum); fixed the two
`missing-workflow-permissions` hygiene items (§3i); hardened
`channel/index.ts`'s error-message exposure; dismissed 10 more CodeQL
alerts as false-positive-after-fix or accepted-risk; migrated the JS
regression tests to `node --test`; and documented, but deliberately did
not fix, the `proxy.go` same-origin federation-peer-proxy finding as
architectural (see the new §3h addendum). A second follow-up (v8.39.11)
then: fixed the `api_smoke_progress.go` capability-model decision this
doc had deliberately deferred (new `CapAnalyticsWrite` + a narrow
`smoke-reporter` preset, chosen specifically because the write path also
serves federation cross-instance forwarding — see the §3b addendum);
fixed the §2 Dependabot recommendation (bumped all 4 stale override
floors); and fixed a `js/double-escaping` bug CodeQL found in v8.39.10's
own new test helper (see the §3h addendum). A third follow-up (v8.39.12)
then implemented the `proxy.go` fix (§6): a second, independent origin
(`server.proxy_sandbox_port`) for `/remote/`+`/api/proxy/`, decided
after discussion to start with a port (not a per-peer subdomain) but
designed so subdomains are a drop-in upgrade later, not a rewrite. Live
testing that fix (not just reading the diff) found and fixed a second,
independent bug: `handleRemotePWA` was forwarding the proxied remote
peer's own security headers verbatim, landing a duplicate, conflicting
`Content-Security-Policy` on top of the new one. A fourth follow-up
(v8.39.13) then built the iframe-embed UX §6 had deliberately deferred
(§6a) — live-browser-testing it (not just reading the diff) found and
fixed four more bugs: the main origin's own CSP had no `frame-src` and
would have blocked the iframe outright; the sandbox origin's now-empty
localStorage left the embedded dashboard's own API calls with no token
(fixed with a new short-lived, single-peer-scoped proxy token system);
static asset tag-loads under `/remote/` needed the same `?token=`
treatment as the top-level navigation; and a pre-existing, unrelated bug
— `handleRemotePWA` forwarding the browser's own `Accept-Encoding`
header upstream — silently corrupted every gzip-compressed script/
stylesheet it proxied, for any real browser, since before this review
began. Everything else in this doc remains exactly what it was — a
recommendation awaiting its own explicit go-ahead, not yet acted on.

## 1. Why this looked noisier than it is

Two systemic, non-obvious reasons the raw alert counts (100 open on
`datawatch`, 11 on `datawatch-app`, 16+5 Dependabot) overstate the real
signal:

1. **`#nosec` (gosec's suppression comment) does nothing for CodeQL.**
   They are two independent scanners with two independent suppression
   mechanisms. Several flagged lines already carry a `#nosec G402`/`G702`
   comment from a prior, documented gosec review — CodeQL has no way to
   know that and flags the same line fresh, with no process today to
   cross-reference a prior review.
2. **For dataflow rules, the alert's `location` is the sink, not the
   source.** The reported line/column can be hundreds of lines (or several
   files) away from where the actual tainted value originates — it's in
   `message.markdown`'s linked locations, not `location`. Reading only the
   reported line produces false "this looks fine" or false "this looks bad"
   judgments. (Concrete example in §3.)

Neither of these is a reason to blanket-dismiss anything — they're why a
line-by-line read was necessary before judging, and why the verdicts below
differ from what a glance at the alert list alone would suggest.

## 2. Dependabot — `datawatch` (16 open, all real, same root cause)

All 16 are in `channel/package-lock.json`, all transitive (`hono`,
`fast-uri`, `ip-address`, `qs` — pulled in via `@modelcontextprotocol/sdk`
→ express/ajv/express-rate-limit/body-parser). **None are false positives.**

`channel/package.json` already has an `overrides` block pinning minimum
versions for exactly these packages — someone already fixed this class of
finding once. But the floors are stale: new CVEs were disclosed in the same
packages *after* those floors were set, and the lockfile was never bumped
to track them.

| Package | Override floor | Installed | Needed to clear every open alert |
|---|---|---|---|
| hono | `>=4.12.34` | 4.13.1 | `>=4.13.7` |
| fast-uri | `>=3.1.5` (wrong major) | 4.1.2 | `>=4.1.5` |
| ip-address | `>=10.3.1` | 10.5.0 | `>=10.7.1` |
| qs | `>=6.15.2` | 6.15.3 | `>=6.16.0` |

**Fixed — v8.39.11.** Bumped all four override floors (re-checked against
the live advisories at fix time, which had moved slightly past the table
above: `fast-uri>=4.1.5` not `4.1.3`, `ip-address>=10.7.1` not `10.5.1`,
`qs>=6.16.0` unchanged, `hono>=4.13.7` unchanged), ran `npm install` to
regenerate the lockfile, confirmed `npm audit` reports 0 vulnerabilities
and `npm ls hono fast-uri ip-address qs` resolves every one above its
patched floor, and re-ran `make channel-build` (no diff in the tracked
embed copy, since only transitive versions changed, not `channel/
index.ts` itself).

## 3. `datawatch` code scanning — by rule, with verdicts

100 open alerts, all CodeQL. Grouped by rule; verdict legend:
**CONFIRMED REAL** (recommend fixing) · **FALSE POSITIVE** (recommend
dismissing, with the specific reason) · **NEEDS REVIEW** (sampled, not
exhaustively verified, or genuinely ambiguous).

### 3a. `go/disabled-certificate-check` (16) — FALSE POSITIVE, all 16

Every single instance already carries a `#nosec G402` comment with a
specific, sound rationale: operator-registered compute nodes / k8s clusters
(self-signed certs by nature of being infra the operator themselves stood
up), or loopback-only connections to the daemon's own self-signed cert
(127.0.0.1). Checked all 16 locations individually — none lack a rationale,
none connect to arbitrary external/attacker-influenced hosts.
**Recommendation:** dismiss all 16 on the CodeQL side as "won't fix,"
referencing the existing gosec rationale in the dismissal comment. No code
change needed — this is a scanner-sync problem (§1), not a vulnerability.

### 3b. `go/path-injection` (54, the largest bucket) — mixed, 4 CONFIRMED REAL found

Sampled all 15 files in this bucket (not every one of the 54 individual
alert IDs was traced independently — several files have 4-7 duplicate
alerts for the same underlying pattern). Found three real, consistent false-
positive explanations covering most of the bucket, **and four genuinely
exploitable findings** — three sharing one root cause the false-positive
patterns don't cover, plus one standalone (the webhook finding, below —
this one was somehow never actually written up here despite being
confirmed real and discussed with the operator in chat; recorded now
rather than left as a gap in this doc).

**False-positive patterns found (covers the majority):**

- **Explicit traversal guard CodeQL doesn't recognize as a sanitizer.**
  `internal/server/bl333_file_service.go` (7 alerts) and `internal/server/
  api.go` (6 alerts) all route through `checkPathTraversal(root, target)` or
  an identical inline `filepath.Clean` + separator-suffixed `HasPrefix`
  check — a textbook-correct traversal guard (verified it handles the
  classic `/root-evil` sibling-directory bypass correctly via the trailing
  separator). CodeQL's Go path-injection query doesn't recognize this
  custom idiom as a barrier.
- **Go's `http.ServeMux` auto-cleans `..` from URL paths before dispatch.**
  `internal/server/bl332_discussion_scope.go` (6 alerts), `internal/
  council/council.go`'s `LoadRun` (1), `internal/evals/evals.go`'s
  `LoadRun` (1) all derive their path-id from a URL *path segment*
  (`r.URL.Path`), which Go's stdlib `ServeMux` (confirmed: `apiMux :=
  http.NewServeMux()` in `server.go`) cleans and 301-redirects on `..`
  *before* the handler ever runs — confirmed this is genuinely the stdlib
  mux, not gorilla/mux with `SkipClean`. A literal `..` segment can't reach
  these handlers at all.
- **The "path" is a fixed, construction-time value, not per-request
  input.** `internal/secfile/secfile.go` (7 alerts) is a generic encrypted
  file I/O helper; grepped all 20 call sites — every single one passes a
  `path` field set once when the owning store is constructed (e.g.
  `filepath.Join(dataDir, "fixed-filename.jsonl")`), never a per-request
  value. `internal/tooling/artifacts.go` (4), `internal/session/tracker.go`
  (3 of its 4) similarly use the session's own already-validated
  `ProjectDir`/`sessionDir`.

**CONFIRMED REAL (3 instances, one shared root cause):**

All three take a `name`/`id` field from a **JSON request body** (not a URL
path segment, so ServeMux's auto-clean never applies) and build a
filesystem path via `filepath.Join(fixedDir, value+".ext")` with **no
traversal check at all** — no `checkPathTraversal`, no `..` rejection,
nothing:

1. **`internal/server/api_smoke_progress.go`** — `POST /api/smoke/progress`
   takes `run_id` from the JSON body (`body["run_id"]`), builds
   `filepath.Join(runsDir, id+".json")`, and writes the full request body
   there with zero validation. Gated by `federation.CapAnalyticsRead` —
   which is a **read**-only-sounding capability included in the built-in
   `monitor` and `read-only` capability-group presets. Any identity holding
   one of those presets (explicitly described in the code as low-privilege,
   broadly-handed-out roles) can write (and, via the `DELETE`/`GET` cases on
   the same handler, delete/read) an arbitrary `*.json`-suffixed path
   anywhere the daemon process can write, e.g. `{"run_id":
   "../../../../some/path/name"}`. This is both a path-traversal bug *and*
   a capability-scoping bug (a write path gated by a capability named
   `*Read`).
2. **`internal/skills/manager.go`'s `Sync`** — a skill registry's advertised
   skill `Name` comes directly from parsing that skill's `SKILL.md`
   frontmatter (`manifest.Name`, in `internal/skills/git_registry.go`'s
   `Browse`), not from its real on-disk directory name (`Path` is derived
   safely from the directory; `Name` is not). `Sync` then does
   `dst := filepath.Join(m.SyncedRoot, registry, av.Name)` and
   `copyDir(src, dst)` — which starts with `os.RemoveAll(dst)`. A malicious
   or compromised skill registry (any git URL an operator adds via
   `skills_registry_add`/`connect` — not just `datawatch-community`) could
   publish a `SKILL.md` with `name: "../../../../home/user/.ssh"` in its
   frontmatter and, the moment an operator syncs it, have an arbitrary
   directory `RemoveAll`'d and overwritten. (Note: the CI validator built
   earlier this session for `datawatch-community` — BL392 — checks
   "frontmatter name matches directory name" as a *merge gate* for that one
   repo's PRs; it provides zero protection for any other registry the
   daemon might connect to, and isn't a substitute for the daemon
   validating this itself.)
3. **`internal/council/council.go`'s `AddPersona`** — `POST
   /api/council/personas` takes `Name` from the JSON body, gated by
   `federation.CapCouncilRun` (part of the `council-operator` preset — a
   narrower-than-admin role explicitly meant for delegating "run councils"
   without granting config/secrets/autonomous-write access). `AddPersona`
   does `os.WriteFile(filepath.Join(dir, p.Name+".yaml"), b, 0o644)` with no
   check. (`UpdatePersona`/`RemovePersona` take `name` from the URL path and
   *are* protected by ServeMux's cleaning — only the POST/create path is
   exposed.)
4. **`internal/messaging/backends/webhook/backend.go`'s `decodeImageURL`**
   (alert #554) — standalone, different shape from the three above: `POST
   /task`'s optional `image_url` field, when not a `data:` URI, is passed
   **directly** to `os.ReadFile(imageURL)` with no `filepath.Join` and no
   scoping check of any kind — a bare arbitrary-path read, not even the
   "append an extension" pattern the other three share. Worse starting
   point than the others on two counts: this listener's bearer token is
   itself *optional* (`webhook.token` unset means no auth at all, and it's
   commonly left unset), and the file's content doesn't just get
   deleted/overwritten — it gets read and forwarded into the task/session
   pipeline as an attachment, a real local-file-disclosure primitive
   reachable pre-auth in the common configuration.

**Pattern:** all three are "add new record, name field arrives via POST
body" shapes — a `name`/`id` field is never checked for `..`/path
separators before being used to build a destination path. This looks like
a *class* of bug, not three unrelated ones — worth a shared fix (one small
`isSafeRecordName` helper reused in all three, and grepped for elsewhere
with the same shape) rather than three one-off patches, if and when fixing
is authorized.

**Fixed (council + skills) — v8.39.4.** New shared `internal/pathsafe`
package (`ValidateRecordName`), applied in `council.AddPersona`/
`UpdatePersona` and `skills.Manager.Sync` before either reaches disk. The
third instance (`api_smoke_progress.go`'s `run_id`) is **not yet fixed** —
it also needs the separate capability-model decision (what gates the
write/delete paths, since `CapAnalyticsWrite` doesn't exist) flagged
earlier, so it's being done as its own pass rather than folded in here.

**Fixed (`api_smoke_progress.go`, both halves) — v8.39.11.** The
path-traversal half (left unfixed as of v8.39.10, since it needed this
same capability decision settled first): `id`/`runID` now go through
`pathsafe.ValidateRecordName` on every write path
(POST/PUT body-or-path `id`, DELETE's path `runID`), plus the path-sourced
value defense-in-depth even though ServeMux already cleans it. The
capability half, deferred at the time for its own decision: this
handler's write methods were discovered, while fixing the traversal, to
also need splitting off of `CapAnalyticsRead` — that capability is handed
to `monitor`/`analytics-viewer`/`read-only`, three presets explicitly
documented as read-only, which could write/delete arbitrary `*.json`
paths here with the traversal bug alone closed but the capability
mismatch untouched. Traced who actually calls the write path before
deciding how to split it: not just the local operator's own smoke runner
(`scripts/release-smoke.sh`), but also the `#54` cross-instance forwarder
— **one federation peer instance POSTing its own smoke results to
another instance's dashboard over a bearer token checked against this
same capability system.** That's the reason the fix isn't simply "add
`CapAnalyticsWrite` to `full-control`": doing only that would silently
403 any operator's already-working forwarding setup if the forwarding
peer's identity was granted one of the three broad read presets (which
happened to work only *because of* this bug). New `CapAnalyticsWrite` +
a new, narrow `smoke-reporter` builtin preset (`{CapAnalyticsRead,
CapAnalyticsWrite}`) — matching this codebase's existing pattern of
purpose-built delegation presets (`comms-channel-agent`,
`council-operator`) rather than a one-size-fits-all grant — now gates
POST/PUT/DELETE on `/api/smoke/progress` and PUT on
`/api/smoke/forward-url`; GET stays on `CapAnalyticsRead`. **Documented
as an operator-action-required change** in the v8.39.11 CHANGELOG entry:
anyone with existing cross-instance forwarding needs to re-grant the
forwarding peer `smoke-reporter` (or `full-control`) after upgrading.
New tests (`internal/server/api_smoke_progress_test.go`) cover both
halves: the traversal rejection (body-sourced and path-sourced `id`,
plus a legitimate id still round-tripping through write/read/delete), and
the capability split (a `monitor`-capability peer can still GET but gets
403 on every write method; a `smoke-reporter`-capability peer can write).
Both validated by temporarily removing the respective guards and
confirming the new tests fail first.

**CodeQL still flagged `skills/manager.go`'s `copyDir`/`copyFile` sink
lines after this fix shipped (5 alerts) — dismissed, v8.39.10.** These
generic helpers sit one call frame below `Sync`, where the new
`pathsafe.ValidateRecordName(av.Name)` guard actually runs; CodeQL's
interprocedural taint tracking doesn't connect the validation one frame
up the call stack to the sink inside the shared helper. Same pattern
recurs for `push.go`/`email/backend.go` below (§3e, §3f) — confirmed by
re-scanning after the fixes landed, not assumed.

Verified: `go build ./...` + full `go test ./...` (2909 tests, 82
packages) clean; new tests confirm both the rejection (no filesystem
trace left behind) and that every one of the 12 real default persona
names, plus a normal `datawatch-community`-shaped skill name, still
work unchanged after the fix.

**Fixed (webhook, #4) — v8.39.5.** Different shape needed a different
fix than `pathsafe.ValidateRecordName` (that package validates a single
*name* field destined to become one path segment; this one takes an
arbitrary caller-supplied *path* that was never meant to be a single
segment). Added `WebhookConfig.ImageDir` (empty by default — the
local-file-path feature is now **disabled** unless the operator opts in
by setting it) and a `filepath.Clean` + separator-suffixed `HasPrefix`
scoping check in `decodeImageURL`, the same correct idiom already
established elsewhere in this codebase (§3b's first false-positive
pattern, above) and verified again here to handle the sibling-directory
bypass (`image_dir=/x/allowed` vs. a path under `/x/allowed-evil`)
correctly. `data:` URIs are unaffected regardless of the new setting.
Verified: full `go test ./...` (2923 tests, 82 packages) clean; 14 new
tests split between unit-level `decodeImageURL` scoping cases
(in-dir, relative-path-under-dir, traversal, sibling-bypass) and the real
HTTP handler end-to-end via `httptest` (auth behavior unchanged; the
actual traversal attack, run through the real handler against a real
file that exists outside the configured directory, confirmed to produce
no attachment rather than erroring the whole request — pre-existing
"swallow the decode error" behavior, confirmed unchanged by this fix).

**Not independently re-verified at this depth (at the time):** `internal/
evals/evals.go`'s write-side (`SaveRun`, uses a server-generated
`uuid.NewString()`, so already safe), and `internal/memory/
layers_recursive.go`'s `L0ForAgent` (single internal caller, read-only,
lower confidence — not traced to `agentID`'s ultimate origin).

**Addendum (v8.39.10) — both of the above turned out to be real, on the
read side, reachable via a query-string parameter:**

1. **`internal/evals.LoadSuite(name)`** — `filepath.Join(r.SuitesDir(),
   name+".yaml")` with no check. Traced `name`'s actual origin, which the
   original pass above didn't do: `GET /api/evals/run?suite=` (`internal/
   server/evals.go`'s `handleEvalsRun`) and the "measure" algorithm
   action (`internal/server/algorithm.go:176`) both read `name` straight
   from `r.URL.Query()`. Query parameters are **not** touched by
   `http.ServeMux`'s path-cleaning — the false-positive reasoning that
   correctly closed out `LoadRun` (same file, a path-segment-sourced
   sibling function) does not transfer to `LoadSuite`, which is why this
   one needed its own look rather than being bucketed with the rest.
2. **`internal/memory/layers_recursive.L0ForAgent(agentID)`** — same
   shape: `GET /api/memory/wakeup?agent_id=` (`internal/server/
   api.go:2818-2836`, gated by `CapConfigRead`) reads `agentID` from the
   query string, then `filepath.Join(l.dataDir, "agents", agentID,
   "identity.txt")` with no check — an arbitrary-file-read of a
   fixed filename (`identity.txt`) anywhere the daemon can read.

**Fixed — v8.39.10.** Both now call the existing `internal/pathsafe.
ValidateRecordName` before the `filepath.Join`. `LoadSuite` returns an
error on rejection; `L0ForAgent` falls back to the host identity
(matching its pre-existing "any failure falls back to host L0" shape)
rather than erroring. New tests for each: `LoadSuite`'s plants a real
file one directory above `SuitesDir()` and confirms both a relative and
an absolute traversal payload error out; `L0ForAgent`'s plants a real
`secret-agents/identity.txt` containing a literal `"SECRET"` one level
outside the retriever's own data dir and confirms traversal falls back
to the host identity rather than leaking it. Both validated by
temporarily removing the new guard and confirming the test fails first
— the `L0ForAgent` test initially used only one `".."` segment (only
cancels the `agents/` segment, landing back at `dataDir` rather than
escaping it), which passed even without the fix in place; caught by that
same "did removing the fix actually break the test" check and corrected
to two `".."` segments.

### 3c. `go/command-injection` (5) — 1 by design, rest false positive/low-risk

- `internal/session/manager.go`'s `runSubprocess`: `exec.Command("bash",
  "-c", sess.Task)` — this is the "subprocess" backend type, whose entire
  *purpose* is running an operator-specified shell command as the task. Not
  a vulnerability in the normal sense (same category as "a shell lets you
  run shell commands") — but worth a sentence of operator attention on
  **who can set `sess.Task` for a subprocess-backend session**, since the
  blast radius of this one is "arbitrary shell execution," and the question
  worth settling is only "is session creation properly capability-gated to
  the actual operator," not whether the call itself is wrong.
  **Settled and dismissed on CodeQL — v8.39.10.** Traced the capability:
  session creation is gated by `CapSessionsWrite`, which is only ever
  granted via the `session-operator` ("full session + agent lifecycle
  control") and `comms-channel-agent` ("starts sessions on the operator's
  behalf") presets — never a broadly-distributed read-only tier. Both
  presets already mean "can run arbitrary tasks" by design, so `bash -c`
  for the subprocess backend specifically isn't a new escalation for a
  capability that already grants that.
- `internal/session/tracker.go`, `internal/server/project_summary.go`:
  argv-list `exec.Command("git", args...)` calls, already carrying a
  `#nosec G702 "argv-list invocation, not shell"` comment — same
  scanner-sync issue as §3a, not a new finding.
- `internal/session/git.go`: builds git URLs with an injected access
  token, then runs `git` via argv-list. **Not fully resolved** — the
  theoretical risk is git's own known argument-injection class (a URL
  starting with `-` being interpreted as a git flag, e.g. during clone).
  Traced `injectTokenIntoHTTPS`'s `rawURL` back to `originURL` but did not
  trace that further to its ultimate source before being asked to stop
  investigating. **Flagging as open, not as confirmed either way.**
  **Traced further and dismissed — v8.39.10.** `originURL` traces only to
  `proj.Git.URL`, set via `profile_create`/`profile_update` — the
  operator's own project-profile config, not external/request input. Same
  accepted-risk class as the compute-node and federation-peer URLs
  dismissed elsewhere in this review (§3e, §3a).
- `internal/compute/probe.go`'s SSH probe: `target` built from
  operator-registered compute-node config (`n.SSH.Host`/`User`), not
  external input. Low risk even in the worst case (operator attacking their
  own config).

### 3d. `go/clear-text-logging` (4 alerts) — 1 CONFIRMED REAL, 3 FALSE POSITIVE

**Correction to this doc's own earlier count:** this section originally
said "2 CONFIRMED REAL, 2 FALSE POSITIVE" — wrong. Alert #619 is the one
real alert, and its own `message.markdown` reports *two source flows*
converging on the same sink (both described below) — that's one alert
with two taint paths, not two separate alerts. Alerts #618, #620, #621
are the three false positives. Caught and fixed while writing up this
alert's resolution below; flagging the correction explicitly rather than
quietly editing a wrong number without saying so.

Importantly these are *not* the same despite sharing a rule — read each
alert's actual source line (§1's lesson), not just the sink:

- **CONFIRMED REAL (alert #619):** `cmd/datawatch/main.go`'s
  `cliPrompt(reader, label, defaultVal)` helper — the interactive
  `datawatch setup` wizard shows an *existing* value as the visible
  default when re-running setup: `fmt.Printf("%s [%s]: ", label,
  defaultVal)`. Traced the two source lines CodeQL's dataflow actually
  points to (`message.markdown`, not `location`):
  `cliPrompt(reader, "SMTP password", cfg.Email.Password)` and
  `cliPrompt(reader, "API key...", cfg.OpenWebUI.APIKey)` — both pass the
  *actual secret value* as `defaultVal`, so re-running setup prints e.g.
  `SMTP password [the-real-password]: ` to the terminal in plain text. This
  matters more than usual for this product specifically, given its own
  tmux screen-capture/session-logging features would persist that into log
  files if setup is ever run inside a captured session. **Likely more call
  sites share this bug** — `cliPrompt` is generic and reused; worth grepping
  all its call sites for other secret fields (haven't enumerated them all).
- **FALSE POSITIVE (alert #618):** `internal/config/template.go`'s
  `GenerateAnnotatedConfig` (lines 122/162 there, a *different* function
  from `cliPrompt` despite CodeQL's `location` pointing at `main.go:8493`
  — that's the sink inside `newConfigGenerateCmd`, not the actual source).
  Its only caller always passes `config.DefaultConfig()` — a fresh
  zero-value config, never the operator's real loaded secrets — so
  `cfg.Email.Password`/`cfg.OpenWebUI.APIKey` are always empty strings on
  this path.
- **FALSE POSITIVE (alerts #620, #621):** `internal/agents/spawn.go`
  (both alerts, same location) — flagged because `as.ClaudeAuthKeySecret`
  flows to a log call, but that field is the secret's *reference name*
  (e.g. `"my-anthropic-key"`), not its value — the actual value
  (`sec.Value`) never appears in the logged line. CodeQL's naming-based
  heuristic over-fired here.

**Fixed (alert #619) — v8.39.6.** The "likely more call sites" guess
above turned out right: grepping `cliPrompt`'s ~60 call sites found 13
secret fields doing the exact same thing, not just the 2 CodeQL happened
to flag (every bot token, bearer token, API key, and shared secret in
the wizard — Telegram, Discord, Slack, Matrix, Twilio, Ntfy, email/SMTP,
generic webhook, GitHub webhook, the PWA server, MCP, DNS channel,
OpenWebUI). New `cliPromptSecret(reader, label, existing)` shows a
neutral "unchanged, press Enter to keep" hint instead of the value for
all 13; the return-value semantics (Enter keeps `existing`, typing
replaces it) are identical to `cliPrompt`, only the printed prompt
differs. The ~45 other, genuinely non-secret fields (hostnames,
addresses, ports, binary paths) correctly keep using the original,
unchanged `cliPrompt`. Verified: full `go test ./...` (2929 tests, 82
packages) clean. New tests confirm the secret is never printed, the
round-trip (Enter → unchanged value) still works, `cliPrompt` itself is
provably unchanged for non-secret fields, and a source-scanning
regression guard that fails if any of the 13 call sites ever reverts to
plain `cliPrompt`.

### 3e. `go/request-forgery` (4) — 1 CONFIRMED REAL, 3 FALSE POSITIVE

- **CONFIRMED REAL:** `internal/server/push.go` — `POST
  /api/push/register` accepts an arbitrary `endpoint` URL from the request
  body with zero validation (no scheme check, no private/loopback-IP block,
  no allowlist), gated by `CapCommWrite`. The daemon later does `http.
  NewRequest(POST, r.Endpoint, body)` directly. Classic SSRF — the daemon
  can be made to POST to any address reachable from it (internal services,
  cloud metadata endpoints, etc.) by anyone able to register a push
  endpoint.
- **FALSE POSITIVE:** `internal/server/proxy.go` (×2 lines) and
  `internal/server/api.go`'s federation-proxy handler — both resolve the
  target through `s.findServer(serverName)`, an admin-registered allowlist
  (the same `remote.URL` pattern used throughout the federation code), not
  an arbitrary caller-supplied URL. CodeQL's dataflow doesn't recognize the
  map-lookup-by-name as breaking attacker control of the destination.
- **FALSE POSITIVE:** `internal/compute/probe.go`'s HTTP probe — `n.
  Address` is operator-registered compute-node config, same reasoning.

**Fixed — v8.39.3.** Coordinated with the `datawatch-app` agent (same node,
`memory_discussion_write` discussion_id `push-endpoint-ssrf-review`) before
writing anything, because a naive "block all private IPs" fix would have
broken a real, currently-working feature: self-hosted ntfy/Gotify push
commonly runs on RFC1918/Tailscale-CGNAT addresses, sometimes over plain
http. Their reply identified that `/api/push/register` actually serves two
different shapes — an SSE self-registration (`client_id` set, endpoint is
the server's own base URL, never dialed) and a real outbound WebPush/
distributor registration (no `client_id`, the one actually dialed) — and
gave the real endpoint shapes currently in use.

Implementation: `internal/server/push_ssrf.go` (new) + `internal/server/
push.go` (modified) + a new `PushConfig` (`internal/config/config.go`) with
two operator-facing flags (`push.allow_insecure_endpoints`,
`push.block_private_endpoints`, both default `false`), full config parity
(REST GET/PUT `/api/config`, YAML template + `docs/config-reference.yaml`,
CLI/MCP/comm channel — all generic proxies to the same REST path, so no
extra wiring needed there). Loopback/link-local/cloud-metadata are rejected
unconditionally at both registration time and — via a `net.Dialer.Control`
hook — at every actual dial, closing the DNS-rebinding gap a
registration-time-only check would leave open. SSE-marker registrations
are now excluded from the dial loop entirely rather than validated, since
the real fix per datawatch-app's info is "never reach it," not "validate
it more carefully."

Verification: full `go test ./...` (2905 tests, all packages) run after
the change, not just the touched package — caught one real regression
before it shipped (`NewServer` is called with a `nil` cfg in an existing
lightweight test helper; the new `setPushConfig(cfg.Push)` call panicked on
that nil pointer — fixed with a nil-guard). A second pre-existing test
(`TestPublishToTopics_RegisteredEndpointGetsOneDelivery`) used
`httptest.NewServer` (loopback) as its mock target, which the new
dial-time guard now correctly refuses — rather than weaken the guard for a
test, made the HTTP-client constructor a swappable package var
(`newPushHTTPClient`) so tests can substitute a plain client while
production code always goes through the real SSRF-guarded one. New
`push_ssrf_test.go` covers: loopback/metadata always rejected, http
rejected unless opted in, private-range/Tailscale-CGNAT allowed by default
and blockable when opted in, SSE markers never reach the dial loop, and
the dial-time guard independently (not just the registration-time check).

Both the code (`push_ssrf.go`'s header comment) and the operator docs
(`docs/operations.md` § Network Security → "Outbound Mobile Push Endpoint
Validation") document the design and what to check first if push breaks
after an upgrade, per the operator's explicit ask to keep this
debuggable.

**CodeQL still flagged `push.go`'s send-line after this fix — dismissed,
v8.39.10.** Same interprocedural-tracking gap as §3b's `skills/
manager.go` dismissal above: the dial-time guard lives inside the
`Transport`'s dialer (`newPushHTTPClient`), one call frame below the
flagged line, which CodeQL's static analysis doesn't trace through.

**Still pending, not done by this fix:** real on-device verification.
Nothing here can exercise actual SSE delivery or a real WebPush/ntfy round
trip from an Android/iOS client — that needs `datawatch-app`'s own E2E/
release testing to specifically watch for push regressions on the next
verification pass against this version. Flagged to them directly (see the
discussion WAL and the follow-up message sent after this landed).

### 3f. `go/email-injection` (1) — originally called CONFIRMED REAL; corrected to NOT A LIVE GAP, hardened anyway — v8.39.7

**Two mistakes in the original write-up, corrected here rather than quietly
edited:**

1. It named `message` (the notification body) as the vulnerable field. It
   isn't — a value placed *after* the header/blank-line separator in a
   hand-built message string can't inject new headers; it's just body
   content. The actual risk, if there were one, would be in `to`/`b.from`
   (the header *values*), not `message`.
2. It called this "CONFIRMED REAL" without actually checking whether
   `smtp.SendMail` itself does anything about it. It does: verified live
   (timed a real call against a non-routable test address, `203.0.113.1`)
   that `smtp.SendMail` calls `validateLine` on the envelope `from` and
   every envelope `to`, and returns `smtp: A line must not contain CR or
   LF` in ~1 *microsecond* — not a multi-second dial timeout, proving it
   rejects before attempting any network connection at all. `internal/
   messaging/backends/email/backend.go`'s `Send` passes the exact same
   `to`/`b.from` strings as **both** the SMTP envelope parameters (which
   stdlib validates) **and** the hand-built header block (which it
   doesn't) — so in this specific code, a CRLF-laced `to`/`from` never
   reaches the wire; `SendMail` errors out before the tainted header
   string is ever sent anywhere. This was not a live, exploitable gap as
   originally characterized.

**Hardened anyway (v8.39.7), as real but narrower defense in depth:** a
new `hasCRLF` check rejects `to`/`b.from` in `Send` before the header
string is even constructed. Two reasons this is still worth having
despite not closing a live gap today: it fires strictly before the
tainted string exists in memory at all (stdlib's check fires slightly
later, after already being handed the pre-built message), and — the part
that actually matters for the future — it protects against a plausible
later change where the header value diverges from the envelope value
(e.g. adding a display name like `"Alice <a@example.com>"` to the header
while the envelope keeps the bare address for SMTP purposes). At that
point stdlib's envelope-only validation would stop covering the header
string, and this explicit check would be the only thing still protecting
it. Verified: full `go test ./...` (2935 tests, 82 packages) clean. 6 new
tests, including a real fake-SMTP-server end-to-end test (hand-written,
speaks just enough SMTP for `net/smtp.SendMail` + `PlainAuth` to
complete) proving a legitimate multi-line notification body still
delivers byte-for-byte unchanged, and a direct test of stdlib's own
`validateLine` behavior (not just asserted from reading its source).

**CodeQL still flagged the header-building line after this fix shipped —
dismissed, v8.39.10.** Same shape as the other post-fix dismissals in
this doc: the new `hasCRLF` check runs immediately above the flagged
line (plus `net/smtp.SendMail`'s own `validateLine`, verified live
earlier in this section) — not a new gap, a scanner re-flagging a
sink CodeQL already had before, now covered.

### 3g. `go/uncontrolled-allocation-size` (2) — FALSE POSITIVE, with a caveat

`internal/memory/store.go` and `pg_store.go`: both flag `results := make
([]Memory, topK)`. Traced it: `topK` comes from `atoiDefault(q.Get
("top_k"), 10)`, which already rejects `<= 0` values, and — critically —
the code clamps `if topK > len(candidates) { topK = len(candidates) }`
*immediately before* the flagged `make()` call, bounding the allocation to
the real (DB-content-bounded) candidate count. CodeQL isn't recognizing
that two-line-earlier clamp as neutralizing. **Caveat, not part of this
alert:** the SQL scan that builds `candidates` in the first place has no
`LIMIT` visible in what was read — if the embeddings table ever grows very
large, that's a separate, legitimate memory-growth question CodeQL isn't
even flagging, worth a look independently of this specific finding.

### 3h. JS/PWA findings (~16, mixed) — 2 CONFIRMED REAL (both fixed), several false positive, some not fully checked

- **CONFIRMED REAL — reflected/DOM XSS:** `internal/server/web/
  diagrams.js`'s `openDoc(path)`. Traced the actual external trigger:
  `const h = decodeURIComponent((location.hash||'').slice(1)); ...
  openDoc(pathPart)` — `path` is attacker-controlled via the URL hash
  fragment (a crafted link like `.../docs#<img src=x onerror=...>`), and
  two lines interpolate it into `innerHTML` **unescaped**:
  `mainEl.innerHTML = '<div class="loading">Loading ' + path + '…</div>'`
  and the matching error-path line. **Correction, found while
  implementing the fix:** this bullet originally said "everywhere else
  in the same file (renderDoc's title/h.path/h.excerpt) is properly
  escaped" — wrong, a conflation. `renderIndex`'s `h.path`/`titleText`/
  `h.excerpt` (a *different* function, rendering search results) genuinely
  are escaped. `renderDoc`'s *own* `title`/`path` header (`<h2>${title}
  </h2>`, `<span class="path">${path} ...`) were not, and neither was a
  third spot, a "View on GitHub" `href="...${path}"` link — 3 more
  unescaped sites sharing the exact same tainted value, missed on the
  first pass. Since `openDoc`/`renderDoc` runs in the operator's own
  authenticated PWA session, a successful hit gives script execution
  with the operator's own session/cookies — this is the standout finding
  of the whole review in terms of exploit simplicity (no capability
  token needed at all, just getting the operator to click a link).

**Fixed — v8.39.9.** All 5 sites (2 directly reachable via the hash
alone — the "Loading"/"Failed to load" messages in `openDoc`, the more
realistically-triggered one being the error path, since the fetch for a
nonexistent crafted path simply 404s; 3 requiring a real file to exist
at the crafted path for `renderDoc` to even run, so lower-reachability
but fixed anyway for defense in depth) now go through a new `escHtml`
helper (`&`, `<`, `>`, `"`) rather than this file's pre-existing
`.replace(/</g,'&lt;')`-only convention used elsewhere — one of the 3
secondary sites sits inside an `href="..."` attribute, where the
exploitable character is a literal `"`, not `<`, so the file's existing
narrower convention wouldn't have closed that one even if applied.
Another standalone Node test (`internal/server/web/
diagrams_security_test.js`, same approach as the prototype-pollution
test's): loads the real `diagrams.js`, sets a crafted `location.hash`
*before* loading (the file calls `openFromHash()` at its own top level,
so loading it is the trigger, no extra step needed), and confirms the
rendered `#main.innerHTML` contains the escaped form, not the raw
payload. Validated the same way as the other new test this session:
reverted the fix temporarily and confirmed the test fails first.
Verified: `node -c` syntax check, full `go test ./...` (2935 tests, 82
packages, unaffected since this is JS-only) clean.
- **CONFIRMED REAL — client-side prototype pollution:** `internal/server/
  web/app.js`'s WS `hook_update` handler. The sink CodeQL flags
  (`n.hookHealth = ...`, `n.state = ...`, etc.) looks like an innocuous
  static-key assignment, but `n = _dash.nodes[hSid]` one line earlier is
  the real problem: `_dash.nodes` is a plain `{}` object, and
  `_dash.nodes["__proto__"]` on a plain object doesn't return `undefined`
  for a missing key — it returns the real `Object.prototype`. A WS message
  with `data.session_id === "__proto__"` makes `n` resolve to
  `Object.prototype` itself, and the subsequent property writes pollute it
  for the entire page's JS runtime. **Reachability, traced:** the server
  only ever broadcasts `hook_update` from `BroadcastHookUpdate`
  (`internal/server/ws.go`), called from `POST
  /api/sessions/{sid}/hook-event` (`internal/server/hook_events.go`) —
  gated by `CapConfigWrite` (a meaningfully elevated capability, not one
  of the broadly-distributed read-only presets like the `api_smoke_
  progress.go` finding earlier in this doc), and `sid` is taken from the
  URL path with **no check that it corresponds to a real, existing
  session** before being recorded and broadcast. So exploitability
  requires already holding `CapConfigWrite` — but for anything at that
  trust tier, this is a genuine privilege *escalation*: from "can write
  daemon config" to "can run arbitrary JS in the operator's own
  authenticated browser tab," a meaningfully worse outcome than
  `CapConfigWrite` alone implies.

**Fixed — v8.39.8.** Added a guard rejecting `session_id` values of
`"__proto__"`, `"constructor"`, or `"prototype"` before `hSid` is used as
a key anywhere in this block — placed before the *first* such use
(`_dash._boards[hSid] = ...`, one line before the flagged `_dash.
nodes[hSid]` lookup), since both go through the identical class of risk
and one guard covers both for free. There is no existing JS test
framework for this PWA (no `package.json`, no bundler — `app.js` is a
single classic `<script>` file), so this needed a standalone, dependency-
free Node script rather than slotting into an existing suite: `internal/
server/web/app_security_test.js` loads the real, unmodified `app.js`
into a Node `vm` context with a minimally-stubbed browser environment
(enough `document`/`window`/`localStorage`/`Notification`/
`MutationObserver`/etc. that the file's real top-level code — including
`state`/`_dash`'s actual initialization — runs to completion without
throwing, same as in a real browser), then calls the real, hoisted
`handleMessage` function with a crafted message and inspects whether
`Object.prototype` got polluted in that same realm.

Two real mistakes surfaced and fixed while building this test, both
worth naming since they'd have silently produced a false "it's fine"
result otherwise: (1) first attempt copied `Object`/`Array`/etc. from
the Node host process into the sandbox object, which shadows the
*identifier* `Object` for code resolving it by name, while object/array
*literals* (`{}`/`[]`) created by code running in the vm context still
bind to the context's own, separate native intrinsics regardless —
the two silently diverge, which is exactly the kind of mismatch this
test exists to catch, so injecting them was removed; `vm.createContext()`
already provisions a complete, correct set on its own. (2) validated the
test itself is actually meaningful, not just passing by coincidence, by
temporarily reverting the fix and re-running it — confirmed it then
correctly reports `POLLUTION_DETECTED: true` on all three checks (a
fresh `{}` inheriting the polluted property, `Object.prototype` showing
it directly, and `_dash._boards` having been reparented via the
`__proto__` setter) before trusting it as a real regression guard.
Final run: clean pass against the actual fixed file, including the
positive/regression case (a normal session's status update still
applies correctly).
  `applyI18nDOM`'s `el.innerHTML = val` for `data-i18n-html` keys — the
  code comment explicitly documents this is first-party-translation-only,
  never user input. Verified no obvious injection point feeds user text
  into the translation dictionary itself.
- **FALSE POSITIVE (self-XSS only):** the chat-markdown renderer
  (`renderChatMarkdown`) calls `escHtml(text)` *first*, before any of its
  markdown-to-HTML regex substitutions run on the already-escaped string —
  a correct escape-then-template pattern CodeQL doesn't recognize as a
  sanitizer. Also the compute-node/LLM "YAML↔Form" editor's `fBody.
  innerHTML = html` — built from the operator's own freely-edited textarea
  content in their own browser tab; can only inject into your own session.
- **Originally called LOW SEVERITY/cosmetic; corrected and fixed — v8.39.10.**
  The compute-kind-migration `name.replace(/"/g, '\\"')` and schedule-entry
  `escHtml(sc.command).replace(/'/g, "\\'")` were both characterized above
  as merely "escape a quote without also escaping a pre-existing backslash
  first." On closer reading while actually fixing these, that
  characterization understated the schedule-entry one specifically:
  `escHtml` has *already* converted every `'` to `&#39;` by the time that
  trailing `.replace(/'/g,...)` runs, so the replace is a no-op regardless
  of any backslash — meaning the *real* bug isn't a narrow backslash-
  collision edge case, it's that HTML-entity-encoding a quote does nothing
  at all to prevent JS-string breakout inside an inline `onclick='...'`
  attribute, because the browser HTML-decodes the attribute value (undoing
  `escHtml`'s own encoding, back to a literal `'`) **before** compiling it
  as JS — so even the simplest quote-breakout payload, no backslash
  involved, worked against the pre-existing code, not just a crafted
  backslash-ending value. Confirmed this precisely by swapping in the old
  pattern and checking both a simple-breakout test and a backslash-
  collision test fail. Found two real call sites with this shape: a
  channel-stats row's expand/collapse toggle (`renderChanRow`), and the
  schedule-entry edit/delete buttons. Fixed both with a new `escJsAttr`
  helper (placed next to `escHtml`), which escapes a literal backslash
  *first*, then the quote — the order matters: escaping the quote first
  lets a value already ending in `\` combine with the newly-added
  backslash into an unescaped quote once decoded, a real bypass of a
  naive single-pass fix. The compute-kind-migration CSS-selector lines
  got the same backslash-before-quote fix for correctness, though they
  were never an XSS vector (the string feeds `querySelector`, not
  markup). New `app-escaping.test.js` round-trips both call sites through
  a full simulation (escape → HTML-attribute-decode → compile as real JS
  via `new Function`) covering a plain quote-breakout payload and a
  trailing-backslash payload, plus a normal-value regression check.
- **Two more unescaped `innerHTML` sites found and fixed — v8.39.10:**
  `app.js`'s status-board renderer interpolated `board.tests.pass/fail/
  skip` (from a hook-event payload, `POST /api/sessions/{sid}/hook-event`,
  `CapConfigWrite`) into two separate `innerHTML` templates with no
  escaping at all — these were the "NOT independently re-verified" lines
  this doc previously flagged near 4208/20752 (line numbers shifted since;
  same underlying renderers). Normally numbers, never enforced as such.
  Both now wrapped in the existing `escHtml`.
- **`js/stack-trace-exposure` (`internal/channel/embed/channel.js`,
  `channel/index.ts`) — fixed, v8.39.10.** Was previously characterized as
  "low severity, info-leak only, not independently deep-dived." Fixed
  anyway as cheap defense-in-depth: the handler now logs the real error
  server-side (`process.stderr.write`) and returns a generic `{error:
  "bad request"}` to the caller. Regenerated the tracked embed copy via
  `make channel-build` rather than hand-editing it. The listener binds
  loopback-only (`127.0.0.1`, confirmed in `channel/index.ts`), so this
  was never remotely reachable — the fix closes the hygiene gap, not an
  actual remote-exposure risk.
- **CONFIRMED REAL, architectural, deliberately NOT fixed — alert #545,
  `go/reflected-xss`, `internal/server/proxy.go:323`.** `handleRemotePWA`
  proxies a remote, admin-added federation peer's own PWA content
  (fetched via `remote.URL`, an operator-registered allowlist entry —
  same trust tier as the `go/request-forgery` false positives dismissed
  in §3e) and serves it back to the browser **under the local daemon's
  own origin**, at `/api/proxy/{serverName}/...`. `rewritePWAContent`
  does rewrite the remote content's API/WS/asset URLs to route back
  through the proxy (correct and necessary for the proxy to work at
  all), but there is no CSP, no iframe sandboxing, and no separate
  serving origin for the proxied content. If a federation peer is
  malicious or its own daemon is compromised, its JS executes with the
  *local* daemon's own cookies/session — a real privilege elevation
  beyond what visiting that peer directly, in its own separate tab/
  origin, would grant. This is a genuine, confirmed finding, but not a
  quick validation-guard fix like the rest of this review — closing it
  properly needs an architectural decision (a CSP header scoped to the
  proxy route, iframe sandboxing instead of direct serving, or moving
  proxied peer content to its own serving origin entirely), not a
  one-line patch. Recorded here as open and deliberately deferred rather
  than rushed; the alert itself remains open on CodeQL (not dismissed —
  it's real).

### 3i. `actions/missing-workflow-permissions` (2) — real, trivial hygiene gap

`docs-sync.yaml` and `ebpf-gen-drift.yaml` have no explicit `permissions:`
block, inheriting the repo-level default (currently `read`, confirmed
during this session's earlier GitHub-hardening work). Not a live
vulnerability today given that default, but the explicit-block convention
this repo already follows elsewhere (per CodeQL's own recommendation,
`{contents: read}`) makes the no-write intent durable even if the repo
default ever changes. Cheapest possible fix in this whole review, whenever
authorized.

**Fixed — v8.39.10.** Added `permissions: {contents: read}` to both.
Validated both files with `python3 -c "import yaml; yaml.safe_load(...)"`
and `actionlint`.

### JS test harness — DRY-up and `node --test` migration (v8.39.10)

Two hand-rolled, nearly-identical Node/`vm` stub-browser-environment
scripts existed from v8.39.8/v8.39.9 (`app_security_test.js`,
`diagrams_security_test.js`), each using `console.error`/
`process.exitCode` for reporting. Before adding a third test file
(`app_escaping_test.js`, for the escJsAttr fixes above), did two things:

1. **Factored the duplicated stub-environment setup** into one shared
   module, `internal/server/web/testutil_browser_stub.js` (exports
   `makeStubElement`, `buildSandbox`, `loadScript`, `flushAsync`, `vm`).
2. **Compared Node's built-in test runner (`node:test`, zero
   dependencies, available since Node 18+) against the hand-rolled
   approach directly**, with a throwaway prototype test, rather than
   assuming either is better. `node:test` won clearly: named tests,
   file:line on failure, structured actual/expected diffs, vs. a bare
   `console.error('FAIL: ...')`. Migrated all three files, renamed to
   `node --test`'s own naming convention (`app-prototype-pollution.
   test.js`, `diagrams-xss.test.js`, `app-escaping.test.js`). One real
   quirk found during the migration: `node --test <directory>`'s own
   auto-discovery was unreliable in this setup (treated a whole
   directory as one opaque failing test even with a single correctly-
   named file inside) — the reliable invocation is explicit shell glob
   expansion, `node --test internal/server/web/*.test.js`, documented as
   such in `CONTRIBUTING.md`'s new Testing section.

All 11 tests across the three files pass under the new runner.

**Addendum (v8.39.11) — the new `app-escaping.test.js` introduced a real
alert of its own (CodeQL #629, `js/double-escaping`), caught while
re-checking this review's current alert state, not from any original
pass.** Its own `htmlAttrDecode` test helper decoded `&amp;` *before* the
other four entities — order-dependent chained `.replace()` calls can
double-unescape when the earlier replacement's output happens to look
like a later entity, exactly as flagged. Concretely: a payload containing
literal `&amp;lt;` text would decode, under the old order, all the way to
`<` — a real browser's single-pass decoder stops at `&lt;`, since it
never re-scans its own output. None of this file's existing test
payloads happened to contain that shape, so it hadn't produced a false
pass; fixed anyway (decode `&amp;` last, mirroring `escHtml`'s own
`&`-first encode order) before it could become a silent footgun in a
security test's own helper. New unit test of the decoder directly, plus
a round-trip regression test through the real `escJsAttr`, validated by
temporarily reverting the decode order and confirming the new test fails
first.

## 4. `datawatch-app` — 11 code scanning + 5 Dependabot (reviewed, less deeply)

Reviewed at a lighter touch than `datawatch` — this is Kotlin/Android code
I have far less session-accumulated context on than the Go daemon, and the
instruction to stop and compile arrived before a `#nosec`-depth pass here.
Treat the verdicts below as directional, not as thoroughly earned as §3's.

- **`java/android/implicit-pendingintents` (3, high)** —
  `WearAlertListenerService.kt` (×2), `NotificationPoster.kt` (×1). This is
  a well-documented, official Android anti-pattern (Google's own security
  guidance calls it out by name): an implicit `PendingIntent` can be
  intercepted/redirected by another app on the same device. Given CodeQL's
  rule here maps directly to published Android platform guidance rather
  than a generic taint heuristic, I'd lean toward **likely real** rather
  than false positive, but have not read the actual `Intent` construction
  at each site to confirm.
- **`java/insecure-trustmanager` (3, high)** — `AndroidHttpClient.kt`,
  `AndroidWsHttpClient.kt`, `DocsViewerSheet.kt`. Same shape as `datawatch`
  server's `InsecureSkipVerify` pattern, but mobile TrustManager
  implementations are a notoriously easy place to get wrong (a common
  anti-pattern is trusting *all* certs rather than pinning to a specific
  fingerprint). `datawatch` itself has a `PinnedTLSConfig`/
  `VerifyPeerCertificate` pattern for exactly this problem
  (`internal/agents/tls.go`, `client.go`) — worth checking whether the
  Android client uses an equivalent pinning approach or a blanket trust-all.
  **Not verified either way** — flagging as the single highest-value next
  check in this repo if this review continues.
- **`java/android/insecure-local-authentication` (1, medium)** —
  `BiometricGate.kt`. Commonly means the biometric check result isn't tied
  to a cryptographic key (just a boolean callback), which can be bypassed
  on an instrumented/rooted device. Narrower threat model (requires device
  compromise) — not independently verified.
- **`actions/missing-workflow-permissions` (4)** — same hygiene gap as
  `datawatch`'s 2, same trivial fix.

**Dependabot (5, all in `docs/testing/v1.0.0/package-lock.json`)** — `js-
yaml` (×4) and `ws` (×1). This path reads like a docs-testing fixture, not
the shipped app's own dependency tree — worth confirming whether this
harness still runs at all before deciding these matter; not confirmed
either way.

## 5. What this doc is *not*

No alert has been dismissed, no code has been changed, no dependency has
been bumped. Every "CONFIRMED REAL" above is a recommendation to fix, every
"FALSE POSITIVE" is a recommendation to dismiss (with the stated reason),
and every "NEEDS REVIEW" is exactly that — not yet resolved. All of it
awaits an explicit operator decision, item by item or in whatever grouping
the operator prefers, before any action is taken.

## 6. The proxy fix (implemented — v8.39.12)

§3h's addendum confirmed `proxy.go`'s `handleRemotePWA` as real and
deliberately deferred. Re-examining it for this discussion sharpened the
severity: the PWA authenticates every API call via a Bearer token read
straight out of `localStorage.getItem('cs_token')` (`app.js`'s
`tokenHeader()`, ~14 call sites), not a cookie. A malicious or
compromised federation peer's JS, proxied under the local daemon's own
origin, doesn't need to "ride" an ambient session — it can call
`localStorage.getItem('cs_token')` directly and exfiltrate the real
local admin bearer token outright, since `localStorage` is shared
per-origin with no further barrier once something runs under that
origin. That's a full local-daemon API takeover, not just the
proxy-channel abuse the original write-up implied.

**This matters for which of the three original options (CSP / iframe+
sandbox / separate origin) actually closes the gap, and the operator's
own instinct to combine CSP with iframe+sandbox exposed a real gap in
the single-option framing:**

- An iframe `sandbox` attribute only blocks `localStorage`/cookie access
  from inside it if `allow-same-origin` is *omitted*. But the proxied
  PWA's own, unmodified code already depends on reading `cs_token` from
  `localStorage` to make its *own* legitimate rewritten API calls work —
  so omitting `allow-same-origin` while still serving proxied content
  from the *same* origin breaks the feature outright (every call 401s),
  while including it just reinstates the original vulnerability (full
  read access to the real token) that sandboxing was supposed to remove.
  **Sandbox alone, on the same origin, cannot both protect the token and
  keep the feature working — that was a real gap in recommending it on
  its own last time.**
- A genuinely **separate serving origin** for proxied content resolves
  this cleanly: a different origin gets its own, naturally separate
  `localStorage`, so there is no real `cs_token` to read from inside it
  regardless of sandbox flags. `allow-same-origin` then becomes safe to
  grant (it only grants access to the *proxy* origin's own, harmless
  storage), which is what actually lets the proxied PWA keep functioning
  normally while denying it anything sensitive.
- **Revised recommendation: all three together, layered by what each
  one actually buys**, not three independent options to pick from:
  1. **Separate origin** (new port/subdomain the daemon also listens
     on) does the real credential-isolation work — structural, not
     policy-based, so it can't be subtly misconfigured the way a CSP
     rule can.
  2. **Iframe + `sandbox`** (with `allow-same-origin` + `allow-scripts`,
     *without* `allow-top-navigation`/unrestricted `allow-popups`)
     embeds the now-harmless-if-compromised content in the dashboard
     while still containing navigation-hijacking/popup abuse against
     the *outer* page.
  3. **CSP** on that separate origin's own responses as the cheap,
     easy-to-keep-current layer — the operator's "tight and updated as
     we add routes" concern is solved by scoping it coarsely
     (`connect-src 'self'`, `frame-ancestors <main-daemon-origin>`,
     `object-src 'none'`) rather than enumerating individual API paths:
     a *new* route under the same proxy origin needs no CSP change at
     all; only adding a genuinely new cross-origin destination would.

**Does the move toward more container/sandbox instances change this?**
Yes, in a way that favors this design rather than complicating it: the
fix above needs exactly **one** extra origin total, shared by every
proxied peer — it is not "one origin per peer," so it doesn't need to
scale with how many sandboxed/containerized instances get federated.
Separately, if that direction means containerized peers increasingly
get their own real, independently-addressable network identity (a
per-container hostname from an ingress/orchestrator) rather than being
reached only through this daemon's own proxy, that opens a cleaner
longer-term alternative worth keeping in view: stop proxying/rewriting
peer content at all, and have the dashboard link directly to each
peer's own real origin instead. That sidesteps `rewritePWAContent`'s
regex-based URL rewriting entirely (already a fragile mechanism on its
own terms) in favor of the browser's native origin model doing all the
isolation work — a bigger redesign than what's being scoped here, not
something to commit to without further discussion, but worth revisiting
if containerized peers become the common case rather than the
exception.

**Decision (operator, 2026-10-04): plan on peers eventually having their
own real addressable name (the longer-term subdomain/per-peer-origin
direction above); for now, start with the port-based origin, designed so
subdomains are a drop-in upgrade later, not a rewrite.** The iframe-
embedding half of the original three-layer plan was deliberately NOT
built in this pass — the existing UI already opens the remote PWA link
in a new browser tab (`target="_blank"`, `app.js`'s server-list `pwaLink`
near the "PWA" link text), and a new tab on a genuinely separate origin
is already fully isolated by the browser's own same-origin policy with
no iframe/sandbox attribute needed at all for that specific UX. Iframe
embedding (if the dashboard later wants to show a peer's PWA inline
instead of a new tab) remains a real, separate follow-up — the sandbox
origin's CSP already allows it (`frame-ancestors` names the main origin,
not `'self'`), so adding an iframe later needs no further origin/CSP
work, just the embedding UI itself.

**Implemented — v8.39.12.** `internal/server/proxy_sandbox.go` (new):
a second, independent `*http.Server` + TCP listener, bound to
`server.proxy_sandbox_port` (new config field, default `8444`) on the
same host(s) and sharing the same TLS cert as the main listener (a cert
is bound to a hostname, not a port). It serves a deliberately narrow mux
— ONLY `/remote/` (`handleRemotePWA`/`handleRemotePWARedirect`, same
handlers as before, unmodified) and `/api/proxy/` (`handleProxy`, same
handler, covers its WS relay too since that's dispatched internally by
path suffix) — never the full API surface. `/remote/` is removed from
the **main** mux entirely; it now only 301-redirects
(`redirectToProxySandbox`) to the sandbox origin, preserving the
original path and query string, so an old bookmark still lands in the
isolated place rather than 404ing.

**The seam for a future subdomain scheme:** every caller that needs "the
origin a proxied peer's content should use" goes through
`proxySandboxPortFor(peerName string) int` — today a one-line lookup
that ignores `peerName` (every peer shares the one configured port), but
the single function a later per-peer-subdomain scheme would need to
change. Nothing else references the port directly.

**The sandbox origin's own CSP** (`buildSandboxCSP`) mirrors the main
origin's `buildCSP` directives (including the same `'unsafe-inline'`
trade-off for `script-src`/`style-src` — the proxied content is
literally another datawatch instance's `app.js`, with the identical
inline-event-handler shape as this one, so it needs the same
accommodation, not a weaker policy) with one deliberate difference:
`frame-ancestors` names the real main origin instead of `'self'`.
Computed **per request** from the incoming `Host` header
(`counterpartOrigin`), not a single fixed string — so it's correct
however the operator happens to reach the daemon (`localhost`, a LAN IP,
a Tailscale name), since whichever hostname reached the sandbox port,
the main dashboard was almost certainly reached via that same hostname's
other port. `X-Frame-Options` is deliberately omitted on the sandbox
origin (its same-origin-or-deny vocabulary can't express "embeddable by
this other specific origin," and setting it would make a legacy browser
without CSP3 support wrongly block the main dashboard's own legitimate
framing — modern browsers already enforce `frame-ancestors` from CSP
alone).

**Config field, full accessibility per `AGENT.md`'s Configuration
Accessibility Rule:** `server.proxy_sandbox_port` — YAML
(`config-reference.yaml`), `GET`/`PUT /api/config` (`handleGetConfig`'s
map, `handlePutConfig`'s switch), which automatically covers the CLI
(`datawatch config set`), the MCP `config_set` tool, and the comm-channel
`configure key=value` command, since all three are thin wrappers over
the same REST endpoint — confirmed by reading each one's implementation,
not assumed. Added to the Settings → Comms → Web Server fields array in
`app.js` (`COMMS_CONFIG_FIELDS`) for the Web UI. `app.js`'s own
`loadServers()` now also fetches `/api/config` to read the port and
builds the "PWA" link against `${location.protocol}//${location.hostname}:
${port}` — if config fails to load, the link is omitted entirely rather
than falling back to the vulnerable same-origin path.

**A second, independent bug found while live-testing this fix, not
from reading the code in isolation:** `handleRemotePWA`'s header-copy
loop (forwarding the proxied remote's response headers onto the local
response) was blindly forwarding the remote's own security headers too
— `curl` against a real running daemon showed **two**
`Content-Security-Policy` headers on one response: the correct one (this
fix's, naming the main origin) and the remote peer's own (`frame-
ancestors 'self'`, copied verbatim). Multiple CSP headers combine as an
AND across directives per the CSP spec, so this could silently make the
*correct* policy more restrictive than intended, or wrong outright,
depending on what a given remote peer's own CSP happens to say — a bug
that would never have been caught by only reading the diff, only by
actually running it. Fixed by skipping every known security-header name
(`Content-Security-Policy`, `X-Frame-Options`, `X-Content-Type-Options`,
`Referrer-Policy`, the three `Cross-Origin-*-Policy` headers,
`Permissions-Policy`) when copying the upstream response, alongside the
pre-existing `Content-Length`/`Content-Encoding` skip.

**Verification — live, not just `go test`.** Built the real binary,
ran it with the new listener against a throwaway config, registered a
real (self-referential, pointed at the daemon's own main port) federation
peer, and confirmed with `curl`: (1) `GET /remote/{name}/...` on the
main origin 301s to the sandbox origin with the path and query preserved
exactly; (2) the sandbox origin serves the real proxied PWA content
(`rewritePWAContent`'s rewriting visibly present in the returned HTML);
(3) exactly one `Content-Security-Policy` header on that response,
correctly naming the main origin in `frame-ancestors`; (4) the sandbox
origin 404s `/api/sessions` and every other main-API route — confirming
the mux's surface is actually as narrow as intended, not just as
documented; (5) `PUT`/`GET /api/config` round-trips
`server.proxy_sandbox_port` correctly. The duplicate-CSP bug above was
found at step 3 of this very pass, before being fixed — this review's
own "verify the test actually catches the bug" discipline extended here
to "verify the live behavior actually matches the design," which is what
surfaced a bug neither `go build` nor the unit tests below would have
caught on their own, since both only exercise this daemon's own
responses, never a round trip through a second, real HTTP server.

New tests (`internal/server/proxy_sandbox_test.go`): the redirect
(target URL, and the disabled-feature 503), `counterpartOrigin`'s
TLS-dual-mode port selection, `buildSandboxCSP`'s frame-ancestors value,
the sandbox mux's route surface (serves `/remote/`+`/api/proxy/`, 404s
everything else), the sandbox mux's CSP header end-to-end, and the
header-stripping fix (`TestHandleRemotePWA_StripsUpstreamSecurityHeaders`,
using a fake upstream `httptest.Server` that sets its own CSP/X-Frame-
Options/Permissions-Policy — confirmed to fail without the fix before
being trusted). Full `go test ./...` (2952 tests, 82 packages) and
`node --test internal/server/web/*.test.js` (13 tests) both clean.

## 6a. Iframe embed (implemented — v8.39.13)

§6's own UX decision was to keep the existing new-tab `<a href target=
"_blank">` link (origin separation alone fully isolates that, no iframe
needed) and defer actually embedding the remote PWA inline. The operator
then asked for the embed to be built. It surfaced four more real bugs —
**every one of them found by actually driving a real headless Chrome
against the feature, not from reading the diff** — none of which showed
up in `go build`, `go test`, or a `curl`-based check, because `curl`
doesn't send `Accept-Encoding` by default and can't evaluate CSP, iframe
sandboxing, or JS execution at all.

**UI:** the "PWA" link in Settings → Servers now opens an embedded
viewer modal (`_showRemotePWAViewer` in `app.js`, following the exact
same `.response-modal`/expand-toggle/backdrop-close pattern already
established by the file viewer, `_showFileViewer`) on a plain left
click; `href` still points at the real sandbox-origin URL so
middle-click/ctrl-click/right-click "open in new tab" keep working
natively, and the modal itself has its own "↗ New tab" link. The iframe
uses `sandbox="allow-scripts allow-same-origin allow-forms
allow-popups"` — `allow-same-origin` is safe to grant here specifically
*because* "same-origin" now means the harmless sandbox origin, not the
real one; `allow-top-navigation` is deliberately never granted, so the
embedded page can never navigate the outer page.

**Bug 1 — the main origin's own CSP would have silently blocked the
iframe outright.** `buildCSP` had no `frame-src` at all, so it fell back
to `default-src 'self'` — meaning the browser would refuse to even
create the iframe, regardless of what the sandbox origin's own
`frame-ancestors` said. CSP requires **both** sides to agree: the
embedder's `frame-src` and the embedded page's `frame-ancestors`. Fixed
by extending `buildCSP` with an optional extra `frame-src` origin,
applied only to `/` and `/index.html` (the one page the whole SPA, and
so any iframe, actually lives on), computed per request the same way
the sandbox origin's own CSP is (off the incoming `Host` header, so it's
correct regardless of which hostname the operator uses to reach the
daemon).

**Bug 2 — the sandbox origin's own fix (empty localStorage) left the
embedded dashboard non-functional.** This is the direct, structural
cost of fixing the real vulnerability: the proxied `app.js` authenticates
via `localStorage.getItem('cs_token')`, and the sandbox origin's
localStorage is now genuinely empty (that's v8.39.12 working as
intended) — so every one of the proxied dashboard's own API calls had
no token to send and 401'd. Putting the *real* admin token there to fix
it would have undone the entire point of the origin split (a compromised
peer's JS could read it right back out). **Decision point raised with
the operator before building anything** (see the three options
presented): build a short-lived, peer-scoped proxy token system (chosen)
vs. ship degraded/read-only vs. only support unsecured daemons. Built as
`internal/server/proxy_token.go`: `handleRemotePWA` mints a random
24-byte, hex-encoded token bound to one `serverName`, valid for
`proxyTokenTTL` (1 hour — long enough for a normal viewing session,
far short of "forever"), on every successful page load (so it also
naturally refreshes on reload, no explicit renewal flow needed). The
token is injected into the proxied page's own `localStorage` via a tiny
inline bootstrap `<script>` placed immediately after `<head>` —
guaranteed to run before the real `app.js` tag later in the document.
`fedAuthMiddleware` gained one new, narrow fallback branch: a token that
doesn't match the real admin or a registered federation peer is checked
against the scoped-token store, but **only** for requests under
`/api/proxy/` or `/remote/`, and **only** if the token's bound peer name
(extracted from that same request's own path) matches exactly — a token
minted for peer A is never valid for peer B, and never valid for any
other route at all (verified directly:
`TestFedAuthMiddleware_ScopedTokenRejectedForOtherPeer`,
`...RejectedForUnrelatedRoute`). `handleProxy`/`handleProxyWS`/
`handleRemotePWA` all now go through one shared `checkProxyAuth` helper
that checks for this scoped-token context first, falling back to the
original, completely unchanged admin/federation-capability check
otherwise.

**Bug 3 — a plain tag load can't carry a header either.** Not just the
top-level `<iframe src>` navigation (same limitation as the pre-existing
`<a href>` link, already gated by `fedAuthMiddleware` and needing the
real admin token) — every rewritten static asset reference
(`<link href="...style.css">`, `<script src="...app.js">`, etc.) under
`/remote/{name}/` is *also* gated by `fedAuthMiddleware`, and a
browser's own tag-driven resource load can no more carry a custom
`Authorization` header than a navigation can. `rewritePWAContent` now
appends `?token=`/`&token=` (`appendProxyToken`, handling both "no
existing query string" and "already has one, e.g. the `?v=<version>`
cache-buster" cases) to every asset URL it rewrites, using the same
scoped token minted for the page itself. Before this was fixed, these
asset requests 401'd and came back as a `text/plain` "unauthorized"
error body — which a real browser, with strict MIME-type checking,
correctly refused to execute/apply as JS/CSS, producing a wall of
"Refused to execute script... MIME type ('text/plain')" console errors
that `curl` (which doesn't enforce MIME-type-vs-execution policy at
all) would never have shown.

**Bug 4 — found at the very next layer down, entirely pre-existing,
unrelated to anything else in this list: `handleRemotePWA` forwarded the
browser's own `Accept-Encoding` header upstream.** Per `net/http`'s own
documented behavior, `Transport` only requests gzip itself and
transparently decompresses the response when the **caller** never set
`Accept-Encoding` explicitly — forwarding the browser's real one (every
real browser sends `Accept-Encoding: gzip, deflate, br` by default; `curl`
does not, unless `--compressed` is passed, which is exactly why this
never surfaced in any of this review's many `curl`-based checks) disables
that transparent mode. `resp.Body` here was the **raw, still-gzipped
bytes**, which `rewritePWAContent` then string-rewrote as if it were
plain UTF-8 text (silently producing garbage — no error, no panic) and
served back with **no** `Content-Encoding` header (already stripped by
the pre-existing header-copy skip-list) — so the browser received raw
gzip binary labeled `text/javascript` and
failed outright: `Uncaught SyntaxError: Invalid or unexpected token`, at
line 1 column 1 of `app.js`, `xterm.min.js`, every proxied script,
confirmed via `window.onerror` instrumentation in a real headless
Chrome. **This bug predates everything else in this entire review** — it
would have broken the *original*, same-origin `/remote/` feature for any
real browser too, for as long as that feature has existed, for anyone
whose static-file layer happens to gzip-compress JS/CSS (this daemon's
own `gzipFileServer` does). It was simply never exercised by a real
browser during development until this exact pass. Fixed by dropping
`Accept-Encoding` before forwarding in `handleRemotePWA`; the same fix
was applied to `handleProxy`'s generic REST-forwarding path too, for
consistency, even though that path is a raw `io.Copy` passthrough today
and so isn't actually corrupted by this (it forwards the real
`Content-Encoding` right alongside the real compressed bytes, staying
internally consistent) — closing it there too is cheap insurance against
the same bug reappearing the instant anyone adds text-rewriting to that
path.

**Bug 5 (cosmetic, fixed anyway) — `/locales/{lang}.json` was never
reachable from the sandbox origin at all.** `app.js`'s own locale fetch
(`fetch('/locales/' + lang + '.json')`) is built from a runtime string
concatenation, not a static `href=`/`src=` attribute — the one shape
`rewritePWAContent`'s regex rewriting actually matches — so it was never
rewritten to carry a peer name, and the sandbox mux never registered
`/locales/` at all, so it 401'd via `fedAuthMiddleware`'s blanket gate
before even reaching a 404. (In the *original* same-origin version, this
same unrewritten fetch would have silently hit the **local** daemon's
own locale file instead of the remote peer's — translations would have
been correct-looking by coincidence, not because the mechanism was
actually proxying anything; now it fails honestly instead of silently
serving the wrong content.) Fixed by registering `/locales/` on the
sandbox mux directly, unauthenticated, serving straight from the same
embedded filesystem the main daemon uses (`HTTPServer.webSub`, a new
field) — translations aren't peer-specific, so there's no reason to
proxy them and no peer name to proxy them *by* even if there were.

**Known, deliberately not fixed: `/api/health`'s staleness-check 401s
once proxied.** `index.html`'s own version-check script calls
`fetch('/api/health')` with no auth at all, by design — `/api/health` is
registered directly on the main mux, outside `fedAuthMiddleware`
entirely, specifically so monitoring tools don't need a token. Once
rewritten to `/api/proxy/{name}/api/health`, it's now going through
`handleProxy`, which (correctly) requires real or scoped auth for the
entire `/api/proxy/*` surface — this specific pre-existing design (one
blanket capability check for the whole proxy surface, not per-sub-path)
predates this review and isn't something today's fixes changed; it just
means one version-mismatch-triggers-reload guard silently doesn't fire
for a proxied view. Not fixed here — narrow, cosmetic, pre-existing, and
out of scope for what was asked.

**Verification — live, with a real browser, at every stage.** Used
`puppeteer-core` driving the system's actual Chrome (no system-wide
Playwright/chromium-cli available in this environment) against a real
running daemon with a real registered federation peer (self-referential
— pointed at the daemon's own main port, with its own matching
`remote.Token` configured, the same as any real two-instance federation
pair would need). Confirmed, in order, as each bug was found and fixed:
zero CSP violations; the sandbox mux's narrow route surface holds; the
bootstrap script correctly sets the scoped token; asset requests
succeed; `window.onerror` is empty (no JS execution errors); every real
API call the dashboard makes on load (`config`, `alerts`,
`autonomous/config`, `observer/peers`, `migration/status`) returns 200
with real data; and, as the final check, a full-page **screenshot**
showing the actual rendered remote dashboard — title, correctly
translated nav labels, "No active sessions" empty state — inside the
modal, next to the main dashboard's own nav bar, visually confirming the
feature works end to end rather than inferring it from logs and status
codes alone.

New tests: `internal/server/proxy_token_test.go` (the token store's
mint/validate/expiry-and-sweep behavior; `fedAuthMiddleware`'s new
branch, including the two rejection cases above; `appendProxyToken`'s
query-string handling; `rewritePWAContent`'s bootstrap injection and
per-asset token appending, including the empty-token no-op case; the
`Accept-Encoding`-stripping fix, confirmed to fail without it; the
unauthenticated `/locales/` route). Full `go test ./...` (2965 tests, 82
packages) clean.
