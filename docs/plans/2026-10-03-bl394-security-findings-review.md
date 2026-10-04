# BL394 — Dependabot + Code Scanning findings review

**Date:** 2026-10-03
**Status:** Started as analysis-only, per explicit instruction mid-review
("don't just fix things or change code, this is meant to be an analysis
and review and I guide all decisions") — every item needed an operator
go-ahead before any action. The operator has since authorized specific
items individually as the review progressed: all 74 confirmed false
positives were dismissed on CodeQL with documented reasons (§3, no code
changed for these); the `push.go` SSRF (§3e, v8.39.3) and the council/
skills path-traversal pair (§3b, v8.39.4) are fixed. Everything else in
this doc remains exactly what it was — a recommendation awaiting its own
explicit go-ahead, not yet acted on.

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

**Recommendation (not yet done, awaiting go-ahead):** bump the four
override floors to the versions above, run `npm install` in `channel/` to
regenerate the lockfile, verify with `npm ls <pkg>` that each resolves to
the patched version, then let Dependabot re-scan.

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

### 3b. `go/path-injection` (54, the largest bucket) — mixed, 3 CONFIRMED REAL found

Sampled all 15 files in this bucket (not every one of the 54 individual
alert IDs was traced independently — several files have 4-7 duplicate
alerts for the same underlying pattern). Found three real, consistent false-
positive explanations covering most of the bucket, **and three genuinely
exploitable findings sharing one root cause the false-positive patterns
don't cover.**

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
Verified: `go build ./...` + full `go test ./...` (2909 tests, 82
packages) clean; new tests confirm both the rejection (no filesystem
trace left behind) and that every one of the 12 real default persona
names, plus a normal `datawatch-community`-shaped skill name, still
work unchanged after the fix.

**Not independently re-verified at this depth:** `internal/evals/evals.go`'s
write-side (`SaveRun`, uses a server-generated `uuid.NewString()`, so
already safe), and `internal/memory/layers_recursive.go`'s `L0ForAgent`
(single internal caller, read-only, lower confidence — not traced to
`agentID`'s ultimate origin).

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
- `internal/compute/probe.go`'s SSH probe: `target` built from
  operator-registered compute-node config (`n.SSH.Host`/`User`), not
  external input. Low risk even in the worst case (operator attacking their
  own config).

### 3d. `go/clear-text-logging` (4) — 2 CONFIRMED REAL, 2 FALSE POSITIVE

Importantly these are *not* the same despite sharing a rule — read each
alert's actual source line (§1's lesson), not just the sink:

- **CONFIRMED REAL:** `cmd/datawatch/main.go`'s `cliPrompt(reader, label,
  defaultVal)` helper — the interactive `datawatch setup` wizard shows an
  *existing* value as the visible default when re-running setup:
  `fmt.Printf("%s [%s]: ", label, defaultVal)`. Traced the two source lines
  CodeQL's dataflow actually points to (`message.markdown`, not
  `location`): `cliPrompt(reader, "SMTP password", cfg.Email.Password)` and
  `cliPrompt(reader, "API key...", cfg.OpenWebUI.APIKey)` — both pass the
  *actual secret value* as `defaultVal`, so re-running setup prints e.g.
  `SMTP password [the-real-password]: ` to the terminal in plain text. This
  matters more than usual for this product specifically, given its own
  tmux screen-capture/session-logging features would persist that into log
  files if setup is ever run inside a captured session. **Likely more call
  sites share this bug** — `cliPrompt` is generic and reused; worth grepping
  all its call sites for other secret fields (haven't enumerated them all).
- **FALSE POSITIVE:** `internal/agents/spawn.go` (both alerts, same
  location) — flagged because `as.ClaudeAuthKeySecret` flows to a log call,
  but that field is the secret's *reference name* (e.g. `"my-anthropic-
  key"`), not its value — the actual value (`sec.Value`) never appears in
  the logged line. CodeQL's naming-based heuristic over-fired here.

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

**Still pending, not done by this fix:** real on-device verification.
Nothing here can exercise actual SSE delivery or a real WebPush/ntfy round
trip from an Android/iOS client — that needs `datawatch-app`'s own E2E/
release testing to specifically watch for push regressions on the next
verification pass against this version. Flagged to them directly (see the
discussion WAL and the follow-up message sent after this landed).

### 3f. `go/email-injection` (1) — CONFIRMED REAL

`internal/messaging/backends/email/backend.go`'s `Send` hand-builds the
raw RFC822 message: `fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: ...\r\n\r\n
%s", b.from, to, message)` with **no CRLF stripping/validation** on
`message` (the notification body) before handing it to `smtp.SendMail`.
Textbook email-header-injection (CWE-93): a `\r\n` sequence inside the
notification text injects arbitrary extra SMTP headers (e.g. a `Bcc:`).
Given this product routes LLM output / webhook-relayed / comm-channel text
into its notification pipeline, `message` content isn't guaranteed free of
literal CRLF — this is a real, reachable gap, not theoretical.

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

### 3h. JS/PWA findings (~16, mixed) — 2 CONFIRMED REAL, several false positive, some not fully checked

- **CONFIRMED REAL — reflected/DOM XSS:** `internal/server/web/
  diagrams.js`'s `openDoc(path)`. Traced the actual external trigger:
  `const h = decodeURIComponent((location.hash||'').slice(1)); ...
  openDoc(pathPart)` — `path` is attacker-controlled via the URL hash
  fragment (a crafted link like `.../docs#<img src=x onerror=...>`), and
  two lines interpolate it into `innerHTML` **unescaped**:
  `mainEl.innerHTML = '<div class="loading">Loading ' + path + '…</div>'`
  and the matching error-path line. Everywhere *else* in the same file
  (`renderDoc`'s `title`/`h.path`/`h.excerpt`) is properly
  `.replace(/</g,'&lt;')`'d — these two spots were simply missed. Since
  this runs in the operator's own authenticated PWA session, a successful
  hit gives script execution with the operator's own session/cookies —
  this is the standout finding of the whole review in terms of exploit
  simplicity (no capability token needed at all, just getting the operator
  to click a link).
- **CONFIRMED REAL — client-side prototype pollution:** `internal/server/
  web/app.js`'s WS `hook_update` handler. The sink CodeQL flags
  (`n.hookHealth = ...`, `n.state = ...`, etc.) looks like an innocuous
  static-key assignment, but `n = _dash.nodes[hSid]` one line earlier is
  the real problem: `_dash.nodes` is a plain `{}` object, and
  `_dash.nodes["__proto__"]` on a plain object doesn't return `undefined`
  for a missing key — it returns the real `Object.prototype`. A WS message
  with `data.session_id === "__proto__"` makes `n` resolve to
  `Object.prototype` itself, and the subsequent property writes pollute it
  for the entire page's JS runtime. Reachable by anything that can push a
  `hook_update` WS message (worth determining exactly who that is as part
  of any fix).
- **FALSE POSITIVE (by design, with a documented boundary):**
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
- **LOW SEVERITY, cosmetic:** the compute-kind-migration `name.replace(/"/
  g, '\\"')` and schedule-entry `escHtml(sc.command).replace(/'/g,
  "\\'")` both escape a quote character without also escaping a literal
  backslash first — the textbook "incomplete sanitization" shape CodeQL
  flagged them for. The first is embedded in a CSS *selector* string (not
  raw HTML) built from an operator's own compute-node name — self-inflicted
  at worst. The second is embedded in an HTML `onclick='...'` attribute
  built from a scheduled command string, which is a more plausible real
  injection surface if `sc.command` can ever originate from something less
  trusted than the operator's own scheduling UI (not verified either way).
- **NOT independently re-verified:** `app.js` lines 4208 and 20752 (`area.
  innerHTML`/`el.innerHTML` from board/telemetry and channel-list data) —
  sampled but didn't trace every interpolated value's escaping all the way
  through; flagging as open rather than asserting a verdict I haven't
  earned.
- Two `js/stack-trace-exposure` alerts (`internal/channel/embed/
  channel.js`, `channel/index.ts`) are just raw error-message disclosure to
  the caller — low severity, info-leak only, not independently deep-dived
  given the low ceiling on impact.

### 3i. `actions/missing-workflow-permissions` (2) — real, trivial hygiene gap

`docs-sync.yaml` and `ebpf-gen-drift.yaml` have no explicit `permissions:`
block, inheriting the repo-level default (currently `read`, confirmed
during this session's earlier GitHub-hardening work). Not a live
vulnerability today given that default, but the explicit-block convention
this repo already follows elsewhere (per CodeQL's own recommendation,
`{contents: read}`) makes the no-write intent durable even if the repo
default ever changes. Cheapest possible fix in this whole review, whenever
authorized.

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
