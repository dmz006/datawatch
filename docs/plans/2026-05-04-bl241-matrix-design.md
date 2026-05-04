# BL241 — Matrix.org communication channel: design discussion

**Filed:** 2026-05-03 (BL241 entry in `docs/plans/README.md`)
**Status:** Design discussion — **Round 2 answered (2026-05-04)**: SAS verification (OOB in v2), key backup with recovery key in secrets store, separate identity (pending operator confirm — wrote "storage identity"), full surface for verification, secrets store mandatory (project-wide policy implications in §11-Policy), match Signal for ACL (no ACL in v1), embed `session_id` for v2 forward-compat. **Round 3 pending** — see §11-Detail for expanded lower-priority decisions and §11-Policy for the cross-cutting secrets-store-as-rule scope question. Plan II implementation plan is in §7B; will be revised once Round 3 lands.
**Owner of this doc:** the design conversation between the operator and the implementing assistant.
**Why this doc exists:** Matrix is too big to specify in a single backlog line. Every other comm channel in datawatch (Signal, Telegram, Discord, Slack, Ntfy, Twilio, GitHub) maps cleanly onto the `messaging.Backend` interface with one auth model, one room/recipient model, and zero encryption. Matrix breaks every one of those simplifying assumptions, so the implementation choice has to be deliberate.

> **How to use this doc.** Each numbered "Decision Point" has options + tradeoffs and an explicit "Questions for operator" subsection. Read top-to-bottom; the diagrams in §5 are tied back to the decision points. The consolidated questions list is in §11 — answers there flow back into the per-DP sections.

---

## Table of contents

1. [Background — why Matrix is hard](#1-background--why-matrix-is-hard)
2. [Where Matrix fits in the existing comm-channel architecture](#2-where-matrix-fits-in-the-existing-comm-channel-architecture)
3. [What's already in tree (state of the matrix backend stub)](#3-whats-already-in-tree-state-of-the-matrix-backend-stub)
4. [Decision Points (DP1–DP10)](#4-decision-points-dp1dp10)
5. [Architecture diagrams per viable shape](#5-architecture-diagrams-per-viable-shape)
6. [Per-surface parity matrix](#6-per-surface-parity-matrix)
7. [Implementation phasing — three candidate paths](#7-implementation-phasing--three-candidate-paths)
7B. [**Plan II — locked-in implementation plan (post-Round-1)**](#7b-plan-ii--locked-in-implementation-plan-post-round-1)
8. [Testing strategy](#8-testing-strategy)
9. [Risks + things that could derail this](#9-risks--things-that-could-derail-this)
10. [Out-of-scope (explicitly deferred)](#10-out-of-scope-explicitly-deferred)
11. [Consolidated open questions for the operator](#11-consolidated-open-questions-for-the-operator)
12. [References](#12-references)

---

## 1. Background — why Matrix is hard

Matrix is a federated, decentralised messaging spec — not a single SaaS like Signal or Telegram. The Client-Server API alone covers ~40 endpoint families; the federation, application-service, and identity-service APIs cover several more. Because Matrix is federated, every "decision" datawatch-side has to be valid across **every** homeserver an operator might use — `matrix.org`, self-hosted Synapse, Dendrite, Conduit, Conduwuit, etc. — without negotiating capabilities each session.

The asymmetries that matter for datawatch:

| Asymmetry | Signal/Telegram model | Matrix |
|---|---|---|
| **Identity** | One phone number / one bot token | A user MXID per homeserver (`@bot:example.com`); the same operator may have different MXIDs on different homeservers; **application services** can claim a whole namespace (e.g. `@datawatch_*:example.com`) |
| **Auth** | Token in config | Multiple modes: password login → token, access token directly, OAuth2 (recent), OIDC (rolling out), application-service token (server-to-server). Each lets you do different things. |
| **Recipient** | Single chat ID / phone | Rooms (group), DMs (which are technically rooms), Spaces (rooms-of-rooms), encrypted rooms (different state events) |
| **Encryption** | Built-in transport TLS only (Signal: E2EE for content too, but signal-cli abstracts it) | E2EE per-room; the **client** does crypto; if datawatch joins an encrypted room without crypto support, every message is opaque ciphertext to it |
| **Federation** | N/A (centralised) | Sender + room can live on any homeserver; bridges introduce additional indirection |
| **Permissions** | Bot is admin or not | Per-room power levels (0–100) and per-event-type ACLs; sending typing notifications, reading receipts, redacting messages, joining rooms — each governed separately |
| **Bridges** | N/A | Matrix bridges to Signal/Telegram/Slack/Discord/etc. are real; an operator could already have a bridged room and want datawatch to talk to that |

Two of these — **encryption** and **bridges** — are where the design conversation matters most. Everything else can be defaulted reasonably; encryption can't.

---

## 2. Where Matrix fits in the existing comm-channel architecture

datawatch already has a stable comm-channel pattern. Every backend implements the small `messaging.Backend` interface (and optionally `ThreadedSender` / `RichSender` / `ButtonSender` / `FileSender`), gets wired in `cmd/datawatch/main.go` next to `wg.Add(1); go Router.Run(ctx)`, and gains 7-surface parity (YAML, REST, MCP, CLI, comm, PWA, locale) per the Configuration Accessibility Rule (§AGENT.md). Matrix uses this same pattern; the question is **which Matrix is being implemented** behind the interface.

```mermaid
flowchart LR
    subgraph operator["Operator surfaces"]
      PWA[PWA Settings → Comms → Matrix]
      CLI[CLI: datawatch setup matrix]
      MCP[MCP: matrix_status / matrix_send]
      YAML[(datawatch.yaml<br/>matrix:)]
    end

    subgraph daemon["datawatch daemon"]
      CFG[config.MatrixConfig]
      MAIN[cmd/datawatch/main.go<br/>backend wiring]
      ROUTER[internal/router/Router]
      MSG[messaging.Backend interface]
    end

    subgraph backends["Existing backends (for reference)"]
      SIG[signal/]
      TG[telegram/]
      DC[discord/]
      SL[slack/]
      NT[ntfy/]
      MX[matrix/  ← stub today]
    end

    subgraph external["Matrix world"]
      HS[(Homeserver<br/>Synapse / Dendrite / matrix.org)]
      ROOM[Rooms / DMs / Spaces]
      FED[Federation]
      BR[Bridges]
    end

    operator --> CFG
    CFG --> MAIN
    MAIN --> MSG
    MSG --> MX
    MX -- HTTPS / WebSocket --> HS
    HS --> ROOM
    HS --> FED
    HS --> BR
    SIG -.same interface.-> MSG
    TG -.same interface.-> MSG
    DC -.same interface.-> MSG
    SL -.same interface.-> MSG
    NT -.same interface.-> MSG
    MAIN --> ROUTER
    ROUTER --> MSG
```

Key invariants this design must preserve:

- **Configuration Accessibility Rule** (AGENT.md §382): every Matrix knob reachable from YAML + REST + MCP + CLI + comm + PWA. No Matrix-only escape hatches.
- **Localization Rule** (AGENT.md §406): every new user-facing string in all 5 locale bundles, mobile issue filed.
- **Release-discipline rules** (AGENT.md §228): the Matrix work is one feature spread across multiple patches, then a minor cut when the surface is complete.
- **Secrets store integration** (BL242): no plaintext access tokens in `datawatch.yaml` once the operator opts in to secrets — all Matrix credentials should resolve via `${secret:matrix-access-token}` etc.
- **No new top-level nav tab.** Matrix lives in Settings → Comms (post-BL247 reorg), the same card as Signal/Telegram/etc.

---

## 3. What's already in tree (state of the matrix backend stub)

A skeletal Matrix backend ships today and is wired but disabled by default.

**`internal/messaging/backends/matrix/backend.go`** (74 lines, scaffolding only):

```go
type Backend struct {
    client    *mautrix.Client
    roomID    id.RoomID
    botUserID id.UserID
}

func New(homeserver, userID, accessToken, roomID string) (*Backend, error) {
    client, err := mautrix.NewClient(homeserver, id.UserID(userID), accessToken)
    // returns Backend{client, roomID, botUserID}
}

func (b *Backend) Name() string { return "matrix" }
func (b *Backend) Send(recipient, message string) error    // unencrypted text only
func (b *Backend) Subscribe(ctx, handler) error            // m.room.message events; ignores own sends
func (b *Backend) Link(deviceName, onQR) error             { return nil }   // no-op
func (b *Backend) SelfID() string                          { return b.botUserID.String() }
func (b *Backend) Close() error                            { b.client.StopSync(); return nil }
```

**`internal/config/config.go`** already declares `MatrixConfig`:

```go
type MatrixConfig struct {
    Enabled        bool   `yaml:"enabled"`
    Homeserver     string `yaml:"homeserver"`
    UserID         string `yaml:"user_id"`
    AccessToken    string `yaml:"access_token"`
    RoomID         string `yaml:"room_id"`
    AutoManageRoom bool   `yaml:"auto_manage_room"`  // unused today
}
```

**`cmd/datawatch/main.go`** wires it the same way as Telegram:

```go
if cfg.Matrix.Enabled && cfg.Matrix.AccessToken != "" {
    matrixB, err := matrix.New(cfg.Matrix.Homeserver, cfg.Matrix.UserID, cfg.Matrix.AccessToken, cfg.Matrix.RoomID)
    // ... newRouter(...); routers = append(routers, r); go r.Run(ctx)
}
```

**`datawatch setup matrix`** CLI subcommand exists and prompts for homeserver / user-id / access-token / room-id.

**`datawatch diagnose matrix`** exists.

**Dependency** `maunium.net/go/mautrix v0.22.0` is in `go.sum`.

**What does NOT work today:**

- E2EE — `mautrix-go` ships its own crypto package (`mautrix/crypto`) that requires an additional store (SQLite or in-memory) and a key-verification flow. The stub doesn't import or initialise any of it.
- `Link()` — no-op. There's no QR / SSO / device-pairing flow for Matrix the way Signal has device linking.
- `AutoManageRoom` — flag exists, no code reads it.
- DMs — the stub treats one room ID as the only target; DMs (which are also rooms) aren't handled differently.
- Spaces — not modelled.
- Threading — not implemented; the existing `ThreadedSender` interface (used by Telegram) isn't satisfied.
- Markdown / formatted body — not implemented; `RichSender` isn't satisfied.
- Buttons / file uploads — not implemented.
- Per-room ACL — datawatch will reply to **anyone** in the configured room.
- Voice messages — not handled, even though Matrix supports `m.audio` events.
- Multi-room — only one `RoomID` per config.
- Multi-account — only one homeserver per daemon.

In other words: the stub will technically connect and echo text into one preconfigured room. Everything else is design space.

---

## 4. Decision Points (DP1–DP10)

Each decision point lays out the options with tradeoffs. **The doc deliberately does not pick.** Recommendations are noted as "your assistant's lean" (with reasoning) but are not decisions. Operator answers in §11 drive what gets implemented.

### DP1 — SDK choice

The implementing library decides what's easy and what's painful for the next 12 months.

| Option | Crypto | Federation | Bridges support | Maintenance | Notes |
|---|---|---|---|---|---|
| **A. mautrix-go** (`maunium.net/go/mautrix`) | First-class via `mautrix/crypto` (Olm/Megolm/cross-signing) | Yes (server-to-server) | Mautrix project itself ships bridges (Signal/WA/Telegram); same author | Active, weekly releases | Already in `go.sum` v0.22.0; the existing stub uses it |
| **B. matrix-org/gomatrix** (the spec's own reference Go lib) | Not built-in; would have to bolt on `mautrix/crypto` anyway | Yes | None | Sporadic; Matrix Foundation prioritises Rust SDK these days | Smaller surface area; we'd write more glue |
| **C. matrix-rust-sdk via FFI** | Most complete crypto in any SDK | Yes | None directly | Most active by far, but Rust + cgo + cross-compile pain | Cross-compile to darwin / windows / arm64 becomes a maintenance load |
| **D. Hand-roll Client-Server API** (`net/http`) | Implement Olm/Megolm ourselves | Yes (we'd implement) | None | Us | Don't. |

**Your assistant's lean:** A (mautrix-go) — already in tree, has crypto, the bridge code is the most-tested in the Go ecosystem, and the existing stub matches the API. The downside is mautrix's API has been less stable than gomatrix historically; pinning + a vendored snapshot would be wise.

**Questions for operator:**
- Q1.1 — Stay on **mautrix-go** (Option A) or switch to **gomatrix** (B)?
- Q1.2 — Are we OK pinning a specific mautrix-go minor version and only bumping it deliberately (i.e., not on every `go get -u`)?

---

### DP2 — Account model

The Matrix account that datawatch uses to talk to operators determines what it can do.

| Option | What it is | Pros | Cons |
|---|---|---|---|
| **A. Bot user** | A normal user (`@datawatch:example.com`) the operator creates manually on the homeserver. Same as today's stub. | Simple; works on every homeserver including matrix.org; no homeserver-side config needed. | Bot has user-level rate limits; can't impersonate; one bot per homeserver per datawatch installation. |
| **B. Application Service** | datawatch registers an AS that owns a namespace `@datawatch_*:example.com`. Receives a transactions feed from the homeserver instead of polling. | Higher rate limits; can ghost-create users (one per session, per channel, etc.) for clearer per-source identity; receives all events in claimed namespaces without per-room joins. | Requires homeserver admin to register the AS (write a YAML registration file, add to `homeserver.yaml`, restart); does not work on matrix.org (no AS registrations from third parties). Fundamentally only works for self-hosted operators. |
| **C. Operator-as-bot** | The operator's own Matrix account has a datawatch-managed device. datawatch authenticates as the operator. | No separate account to provision; messages "from the operator" appear correctly in shared rooms. | Conflates bot activity with operator activity; key sharing across the operator's other devices is fragile; permission boundaries blur. Generally a bad idea. |
| **D. Hybrid AS + bot fallback** | Use AS when configured, fall back to bot user when not. | Best of both worlds for operators who can self-host. | Two code paths to test. |

**Your assistant's lean:** A as the v1, with the architecture leaving room for D in v2. Option B alone excludes matrix.org users.

**Questions for operator:**
- Q2.1 — Do we support **bot user only** (A) for v1, or design for **A + B** (D) from the start?
- Q2.2 — If A, do we want to add CLI tooling to **register the bot user** (`datawatch setup matrix create-account`) or assume the operator does it manually first?

---

### DP3 — Authentication

How the daemon obtains the access token.

| Option | UX | Token rotation | Notes |
|---|---|---|---|
| **A. Operator pastes access token** | Manual: log in to Element, copy the token from Settings → Help & About → Advanced. | Manual when it expires (matrix.org tokens do not expire by default; some homeservers issue short-lived tokens). | Today's stub. Easy. |
| **B. Operator pastes username + password; daemon does `/login`** | Two-step: paste creds, daemon does the round-trip and stores the resulting token. | Daemon can re-`/login` if the token gets invalidated. | Password handling: store via secrets store (BL242) or never-store-only-use-once? |
| **C. SSO / OIDC web flow** | Operator opens a URL, completes their org's SSO, daemon receives the token via redirect. | Tokens refresh per OIDC. | Most complex; requires daemon to expose a callback URL; not all homeservers support SSO. |
| **D. Application Service token** | Configured in the homeserver's `registration.yaml`; static. | Doesn't rotate; operator regenerates on the homeserver. | Only viable if DP2 = B or D. |
| **E. Mixed (daemon picks based on config presence)** | Whichever fields are set wins. | n/a | Standard pattern in datawatch (e.g., LLM backend selection). |

**Your assistant's lean:** A as v1 (existing flow works). Add B in v2 (small lift, big UX win). C is its own initiative (SSO is per-org); defer. D follows DP2.

**Questions for operator:**
- Q3.1 — v1 = **access-token paste only** (A), or do we add **username+password** (B) at the same time?
- Q3.2 — Where do Matrix credentials live? Plaintext in `datawatch.yaml`, or **must** they go through the secrets store (`${secret:matrix-access-token}`) once BL242 ships? (BL242 is shipped; the policy choice remains.)
- Q3.3 — Is OIDC/SSO (C) ever in scope, or always operator-driven outside datawatch?

---

### DP4 — Encryption (E2EE)

This is the single biggest design decision in the doc. Matrix encryption is per-room state, not server-wide. A datawatch bot that can't decrypt sees `m.room.encrypted` events with opaque ciphertext.

| Option | Scope | Operator UX | Implementation cost | Risk |
|---|---|---|---|---|
| **A. No E2EE (cleartext only)** | Bot only joins unencrypted rooms; refuses encrypted invites. | Operator must create an unencrypted room. (Element's default is **encrypted-on-create** for DMs.) | Low — today's stub already does this. | If operator misconfigures, every message looks broken. Most real-world Matrix usage is encrypted. |
| **B. E2EE always required** | Bot only joins encrypted rooms; manages keys via `mautrix/crypto`. | Bot must verify (cross-sign / SAS) on first contact. | High — crypto store, key backup, device-list updates, megolm session sharing. | If the crypto store gets corrupted, the bot loses access to historical messages until rekey; debugging crypto bugs is hard. |
| **C. E2EE supported, cleartext supported, per-room negotiation** | Bot accepts whatever the room is. Sends in the room's mode. | Same as A or B per room. | High (still need crypto for option-B rooms). | Most flexible but most surface area. |
| **D. Strict cleartext + warn-on-encrypted** | Bot joins encrypted rooms but refuses to send/decrypt and posts a warning to the operator's notification channel. | Operator sees clearly when something is wrong. | Low + warn path. | Bot is functionally useless in encrypted rooms; this is just an "honest stub" mode. |

**Sub-decision (only relevant if E2EE is in scope):**

- **Crypto store backend.** mautrix-go's crypto package needs persistent storage. Options: SQLite file at `~/.datawatch/matrix-crypto.db` (recommended, isolated), or piggyback on the existing daemon SQLite (cleaner but tight coupling).
- **Verification UX.** First-message verification needs a flow:
  - **a)** Operator scans an emoji SAS in their Matrix client
  - **b)** Operator passes the bot's session key out-of-band on `datawatch setup matrix`
  - **c)** Auto-trust everything (insecure; defeats the point)
- **Key backup.** Should the bot back up its megolm session keys to the homeserver (encrypted with a recovery key) so it survives device wipes? Adds complexity but matches Element's default.
- **Device verification model.** Cross-sign with the operator's own MXID, or treat the bot as a separate identity that doesn't cross-sign?

**Your assistant's lean:** D for v1 (cleartext-only with a clear warning when an encrypted room is encountered) and B for a v2 dedicated to crypto. Reasoning: crypto is a 2-3-week implementation by itself with non-trivial test surface, and shipping a half-broken crypto backend is worse than shipping no crypto with a clear error.

**Questions for operator:**
- Q4.1 — v1 scope: **cleartext only with warn** (D), **cleartext only with refuse** (A), or **E2EE in scope from day one** (B/C)?
- Q4.2 — If E2EE is ever in scope: SAS verification (a), out-of-band key (b), or auto-trust + audit (c)?
- Q4.3 — Does the bot do **key backup** to the homeserver, or are session keys local-only (lose on `~/.datawatch/` wipe)?
- Q4.4 — Is the bot a **separate identity** or does it **cross-sign with the operator's MXID**?

---

### DP5 — Routing model (rooms / DMs / spaces / multi-room)

Matrix has more "where do messages go" choices than any other backend datawatch ships.

| Model | Description | Pros | Cons |
|---|---|---|---|
| **A. One room, fixed in config** | Today's stub. Bot joins one preconfigured room; all sends go there; all receives come from there. | Simple; matches Telegram's `chat_id` shape. | Loses Matrix's primary advantage (multi-room organisation); doesn't model DMs. |
| **B. One room + DM-per-operator** | Bot joins one main room (broadcast / state) AND any operator can DM the bot for private commands. | Mirrors Signal's model (group + per-operator DM). | Need to detect DM rooms vs group rooms (`m.direct` event vs membership count vs both). |
| **C. Room-per-session** | Each datawatch session opens a Matrix room named `datawatch-<session-id>`; the operator gets invited. | Best per-session UX (Matrix threads aren't as good as separate rooms); easy notification scoping. | Room sprawl; operator's room list fills with stale rooms; needs a "close & archive" verb. |
| **D. Spaces (rooms-of-rooms)** | One Matrix Space per datawatch project / cluster / etc.; sessions land as rooms inside the space. | Maps cleanly onto datawatch's project / cluster abstractions. | Spaces are still maturing; not all clients render them well. Adds a lot of state events. |
| **E. Operator-defined per-channel** | Config has `routes: [{name: "general", room: "!abc:..."}, {name: "alerts", room: "!def:..."}]`; comm-channel commands like `route set <session> <route>` aim sessions at routes. | Maximum flexibility; matches the existing routing-rules feature. | More YAML; operator has to design their room layout. |

**Sub-decision (only if B is in scope):**

- DMs imply consent flows. If a stranger DMs the bot, does it auto-respond? Auto-reply with "you're not on the allow-list"? Ignore silently?

**Your assistant's lean:** A in v1 (matches the existing stub and Telegram), B in v2 (Signal parity), E as a stretch. C and D are cool but they're separate features that should be backlogged on their own (BL241-followup).

**Questions for operator:**
- Q5.1 — v1 routing: **A** (single room) or **B** (single room + DMs)?
- Q5.2 — If DMs are in scope: **allow-list of operator MXIDs**, or **ack-and-ignore** unknown senders, or **bounce with help text**?
- Q5.3 — Is **room-per-session** (C) appealing enough that we should design for it now even if implementation comes later? (i.e., should the v1 message format include a `session_id` somewhere structured so a later C layer can route on it?)
- Q5.4 — **Spaces** (D) — defer entirely, or design the room-naming convention now so future Spaces work isn't a rewrite?

---

### DP6 — Federation behaviour

Matrix users can live on any homeserver, and rooms can be federated across many. The bot's homeserver matters less than which rooms it joins.

| Question | Options |
|---|---|
| Does the bot only join rooms hosted on its own homeserver? | A. Yes (least surprise; refuses cross-server invites) · B. No (joins anywhere it's invited) |
| What does the bot do when a federated user (`@stranger:other.org`) sends to a room it's in? | A. Ignore (allow-list mode; operator has to whitelist) · B. Process the same as same-server users · C. Process but tag the source (`from: matrix:other.org`) so audit log captures it |
| Cross-federation E2EE? | If E2EE is enabled (DP4), federated rooms still encrypt end-to-end; the bot must handle keys for users on other homeservers |

**Your assistant's lean:** B (no homeserver lock-in) + C (process federated, tag source). Locking the bot to one homeserver defeats Matrix's central design.

**Questions for operator:**
- Q6.1 — Federation policy: **bot's homeserver only**, or **anywhere it's invited**?
- Q6.2 — Federated-sender behaviour: **ignore unless allow-listed**, **process same as local**, or **process + tag source**?

---

### DP7 — Inbound message filtering / ACL

Datawatch's existing comm channels assume "if you're in the configured room, you're authorised." Matrix may need finer ACLs because rooms are easier to invite people to.

| Option | Description |
|---|---|
| **A. Trust everyone in the room** | Same as Signal/Telegram today. |
| **B. Allow-list of MXIDs** | Only `@operator:example.com` (and a configured list) can issue commands; others' messages are stored but ignored. |
| **C. Power-level gate** | Only senders with power-level ≥ N (e.g., 50 = moderator) can issue commands. |
| **D. Hybrid** | Default A; if `matrix.acl` is set, switch to B or C. |

**Your assistant's lean:** D — default to A (consistent with Signal/Telegram), opt in to B for higher-security setups.

**Questions for operator:**
- Q7.1 — Do we ship an ACL in v1 (B/C/D), or trust the room (A) and add ACLs in v2?
- Q7.2 — If ACL in v1: is it the operator's MXID hard-coded in config, or a list?

---

### DP8 — Bridges (Matrix ↔ other networks)

A Matrix room can be bridged to Signal, Telegram, Slack, Discord, IRC, etc. via mautrix bridges and others. The bot doesn't need to know it's bridged — it just sees Matrix events. **But:**

- Sender names from bridges look like `@signal_+15555550100:matrix.example.com` — does the operator want these mapped back to phone numbers / handles?
- Files coming from a bridged source have larger latency.
- The "bridge ghost" user can flap if the bridge restarts; should the bot track that?

**Your assistant's lean:** Out of scope for v1. Bridges work transparently because Matrix is consistent end-to-end; the bot doesn't need to know. Optional v2 enhancement: pretty-print bridge-ghost MXIDs.

**Questions for operator:**
- Q8.1 — Confirm bridge-awareness is **out of scope** for the initial Matrix BL241 work, with a future BL for "bridge user detection + display".

---

### DP9 — Operator linking / first-time setup flow

How does the operator go from "I want to enable Matrix" to "datawatch is live in my room"?

| Step | Today (CLI only) | Could be |
|---|---|---|
| 1. Provide homeserver URL | `datawatch setup matrix` prompts for it | Same; PWA Settings → Comms → Matrix has a form |
| 2. Provide MXID + access token | Prompts | PWA form. If DP3 = B, prompt for password, daemon does `/login`. |
| 3. Provide room ID | Prompts (`!abcdef:matrix.org`) | Room ID is hard for humans. **Better:** operator provides a room **alias** (`#datawatch:matrix.org`) and daemon resolves it. **Best:** operator pastes a `matrix:` URI from Element. |
| 4. Verify connectivity | `datawatch diagnose matrix` | PWA shows status badge; reload-after-save checks connectivity |
| 5. Bot joins the room | Operator has to invite the bot manually from their Matrix client first | Could be automated if `auto_manage_room` flag is implemented (bot creates a room and invites the operator) |
| 6. (E2EE only) Verify the bot's device | Operator does SAS in Element | Same; could surface the SAS emojis in the PWA so operator types them in Element |

**Your assistant's lean:** Phase 1 = parity with today's CLI flow + a PWA form. Phase 2 = room-alias resolution + auto-invite. Phase 3 = E2EE verification helper.

**Questions for operator:**
- Q9.1 — Should v1 support **room alias** (`#datawatch:matrix.org`) input, or is the raw room ID OK?
- Q9.2 — Is **auto-create a room and invite operator** a v1 feature (the existing `AutoManageRoom` flag), or v2?
- Q9.3 — If E2EE: should the PWA show **SAS emojis** so operator can verify directly from the PWA without opening Element?

---

### DP10 — Configuration model

How config is shaped affects every other decision and especially the upgrade path.

| Option | Shape | Notes |
|---|---|---|
| **A. Single block, like today** | `matrix: {enabled, homeserver, user_id, access_token, room_id}` | Easy. Doesn't model multiple rooms / accounts. |
| **B. Single block + room list** | `matrix: {homeserver, user_id, access_token, rooms: [...]}` | Supports DP5 model E. |
| **C. List of accounts** | `matrix: [{homeserver: ..., user_id: ..., rooms: [...]}, {...}]` | Models multiple homeservers. Probably overkill for v1. |
| **D. Single block + secrets refs** | `matrix: {homeserver, user_id, access_token: "${secret:matrix-token}", room_id}` | Mandatory after BL242; just wiring. |

**Migration concern:** today's stub uses Option A. Moving to B is a YAML schema change; we'd want to keep Option A working as a single-room shorthand.

**Your assistant's lean:** A + D for v1 (extending the existing stub minimally), with the YAML schema designed so adding a `rooms:` list later is a non-breaking superset.

**Questions for operator:**
- Q10.1 — Confirm v1 sticks with **single account, single room** YAML shape, just adding secret-ref support.
- Q10.2 — Should `room_id` accept a **room alias** as well as a room ID (transparently resolved)?

---

## 5. Architecture diagrams per viable shape

These are the three shapes the operator's answers to DPs above are most likely to land us on.

### Shape α — "Matrix-as-Telegram-clone" (cleartext, single room, single bot)

The minimum-viable shape that brings the existing stub to feature-complete. Equivalent to how Telegram works today.

```mermaid
flowchart TD
    OP[Operator]
    EL[Element / any Matrix client]
    HS[(Matrix homeserver<br/>Synapse)]
    R1[(Room: !abc:example.com<br/>UNENCRYPTED)]
    DW[datawatch daemon]
    BOT[matrix.Backend<br/>mautrix-go client]
    ROUTER[router.Router]
    SESS[session.Manager]

    OP -->|talks in| EL
    EL -->|m.room.message| HS
    HS --> R1
    R1 --> BOT
    BOT --> ROUTER
    ROUTER -->|commands like<br/>'sessions list'| SESS
    SESS -->|reply| ROUTER
    ROUTER -->|m.room.message| BOT
    BOT --> R1
    R1 --> EL
    EL --> OP
```

Properties: deterministic, no crypto state, no key management, fits the existing comm-channel test harness.

---

### Shape β — "Signal-parity Matrix" (cleartext, room + DMs, allow-list)

What Signal does today but on Matrix: a main room for broadcast + each operator can DM the bot for private commands. Implements DP5 = B and DP7 = B.

```mermaid
flowchart LR
    subgraph operators["Operators"]
      OP1["@op1:example.com"]
      OP2["@op2:example.com"]
      STR["@stranger:example.com<br/>(not on allow-list)"]
    end

    subgraph hs["Homeserver"]
      MAIN["Room: !main:example.com<br/>broadcast / status"]
      DM1["DM: @op1 ↔ @datawatch"]
      DM2["DM: @op2 ↔ @datawatch"]
    end

    subgraph dw["datawatch daemon"]
      BOT[matrix.Backend]
      ACL[ACL filter:<br/>allow-list MXIDs]
      ROUTER[router.Router]
    end

    OP1 --> MAIN
    OP2 --> MAIN
    STR --> MAIN
    OP1 --> DM1
    OP2 --> DM2
    STR -.attempted DM.-> BOT

    MAIN --> BOT
    DM1 --> BOT
    DM2 --> BOT
    BOT --> ACL
    ACL -- pass --> ROUTER
    ACL -. drop + log .-> X[/dropped/]
    ROUTER --> BOT
    BOT --> MAIN
    BOT --> DM1
    BOT --> DM2
```

---

### Shape γ — "Encrypted Matrix" (E2EE everywhere, single room or DMs, key backup)

Implements DP4 = B (or C). Adds the full crypto stack.

```mermaid
flowchart TD
    OP["@op:example.com<br/>verified device"]
    EL[Element]
    HS[(Homeserver)]
    ROOM["Room: !sec:example.com<br/>m.room.encryption=m.megolm.v1.aes-sha2"]
    BOT["matrix.Backend<br/>+ mautrix/crypto"]
    DB[("crypto store<br/>~/.datawatch/matrix-crypto.db")]
    KB["Server-Side Key Backup<br/>encrypted with recovery key"]
    ROUTER[router.Router]

    OP -->|SAS verify on first contact| BOT
    OP --> EL
    EL -->|m.room.encrypted| HS
    HS --> ROOM
    ROOM --> BOT
    BOT <--> DB
    BOT <--> KB
    BOT --> ROUTER
    ROUTER --> BOT
    BOT -->|m.room.encrypted| ROOM
    ROOM --> EL
```

The crypto store is the new state surface (matrix sessions, room keys, device keys). Loss of the crypto store = loss of decryption for historical messages until rekey. Backup is recommended.

---

## 6. Per-surface parity matrix

Per the Configuration Accessibility Rule, every Matrix knob has to be reachable from every operator surface. This table is the v1 acceptance criterion.

| Knob | YAML | REST | MCP | CLI | Comm | PWA | Locale |
|---|---|---|---|---|---|---|---|
| Enable / disable | `matrix.enabled` | `PUT /api/config` (existing) | `config_set` (existing) | `datawatch config set matrix.enabled true` | `configure matrix.enabled true` | Settings → Comms → Matrix toggle | `comm_matrix_enabled` |
| Homeserver URL | `matrix.homeserver` | same | same | same | same | text input | `comm_matrix_homeserver` |
| MXID | `matrix.user_id` | same | same | same | same | text input | `comm_matrix_user_id` |
| Access token | `matrix.access_token` (or `${secret:...}`) | same | same | same | same | password-style input | `comm_matrix_access_token` |
| Room ID / alias | `matrix.room_id` | same | same | same | same | text input + (v2) "browse joined rooms" | `comm_matrix_room_id` |
| Auto-manage room | `matrix.auto_manage_room` | same | same | same | same | toggle | `comm_matrix_auto_manage` |
| Status (read-only) | n/a | `GET /api/matrix/status` | `matrix_status` | `datawatch matrix status` | `matrix status` | Settings → Comms → status badge | `comm_matrix_status_*` |
| Test send | n/a | `POST /api/matrix/test` | `matrix_test` | `datawatch matrix test [room]` | `matrix test` | "Send test message" button | `comm_matrix_test_*` |
| (DP5=B) DMs enabled | `matrix.dms.enabled` | … | `matrix_dms_*` | `datawatch matrix dms` | `matrix dms` | DM section in card | `comm_matrix_dms_*` |
| (DP7) ACL allow-list | `matrix.acl.allowed_mxids: [...]` | … | `matrix_acl_*` | `datawatch matrix acl add @op:server` | `matrix acl add @op:server` | List editor | `comm_matrix_acl_*` |
| (DP4) Encryption mode | `matrix.encryption: cleartext\|warn\|required` | … | `matrix_encryption_*` | … | … | Dropdown | `comm_matrix_encryption_*` |
| (DP4) Crypto store path | `matrix.crypto_store: ~/.datawatch/matrix-crypto.db` | … | … | … | … | path picker (read-only) | `comm_matrix_crypto_store` |
| Diagnose | n/a | n/a | n/a | `datawatch diagnose matrix` (exists) | `diagnose matrix` | "Run diagnose" button | `comm_matrix_diagnose_*` |

mobile (datawatch-app) parity is filed as the standard issue per the Localization Rule once the v1 ships.

---

## 7. Implementation phasing — three candidate paths

The phase plan depends on the answers to DP4 (E2EE) more than anything else. Three plans laid out; operator picks one.

### Plan I — "Cleartext-first, E2EE later" (recommended if v1 needs to ship in a single minor)

Targets Shape α + most of Shape β by v6.7.0, defers Shape γ to v6.8.0 or beyond.

| Phase | Target | Scope |
|---|---|---|
| **P1** | v6.7.0-α (≈3 days) | Matrix backend feature-complete for cleartext: Send + Subscribe + AutoJoin + reject encrypted rooms with operator notification. `RichSender` (Markdown). `ThreadedSender` via Matrix threads. Status + test endpoints across all 7 surfaces. Locale keys. Mobile issue filed. |
| **P2** | v6.7.0-β (≈2 days) | DM support (Shape β). Allow-list ACL. Auto-invite operator on room create (`auto_manage_room`). Room-alias resolution. |
| **P3** | v6.7.0-γ (≈1 day) | PWA Settings card with status, test button, ACL editor. Diagnose panel. |
| **P4** | v6.8.0 (when scheduled) | E2EE: `mautrix/crypto` integration, SAS verification flow, key backup, encrypted-room support. New top-level encryption config block. PWA SAS-verification helper. |

### Plan II — "Encrypted-from-day-one"

Targets Shape γ in v6.7.0. Higher initial effort, no half-feature shipped.

| Phase | Target | Scope |
|---|---|---|
| **P1** | v6.7.0-α (≈4 days) | Cleartext backend feature-complete (same as Plan I P1). |
| **P2** | v6.7.0-β (≈5 days) | E2EE: crypto store, megolm session management, device list updates, megolm key sharing. |
| **P3** | v6.7.0-γ (≈3 days) | SAS verification flow + key backup. PWA verification helper. |
| **P4** | v6.7.0-δ (≈2 days) | DMs + ACL + auto-manage room (Plan I's P2). |
| **P5** | v6.7.0-ε (≈1 day) | PWA card, diagnose panel (Plan I's P3). |

### Plan III — "Stub it, gate it, learn"

Smallest possible v1: ship Shape α with conservative defaults, get one operator using it, design v2 from real-world feedback.

| Phase | Target | Scope |
|---|---|---|
| **P1** | v6.7.x (≈2 days) | Bring existing stub to "actually works" — fix Send error handling, implement basic Subscribe filtering, refuse encrypted rooms loudly, surface status across all 7 surfaces. **No new features.** |
| **P2** | follow-on BL | Everything else (DMs, ACL, E2EE) gets its own design conversation when an operator hits the gap. |

**Your assistant's lean:** Plan I is the right balance for a single minor cut; Plan III is the right balance if you want a real-world signal first.

**Questions for operator:**
- Q-Phase.1 — Which plan: I, II, or III?
- Q-Phase.2 — Target version (v6.7.0 minor, v6.7.x patches, v7.0.0 major)?
- Q-Phase.3 — Is there an operator-side Matrix homeserver where we test this, or do we use matrix.org for development?

---

## 7B. Plan II — locked-in implementation plan (post-Round-1)

Based on the Round-1 answers in Appendix A, here is the concrete phase-by-phase work for v6.7.0. Every cell in §6 (per-surface parity matrix) lands inside one of the phases below.

> **Estimates are working-day estimates** for the implementing assistant (not calendar days). Assumes the operator is available for the SAS verification / AS registration UAT loops at phase boundaries. Not a hard schedule — adjust for context.

### Plan II at a glance

```mermaid
flowchart LR
    P1["P1 (4d)<br/>Backend foundation<br/>+ AS hybrid wiring<br/>+ adapter layer"]
    P2["P2 (5d)<br/>E2EE crypto stack<br/>(mautrix/crypto)<br/>encrypt+decrypt"]
    P3["P3 (3d)<br/>Verification (SAS or OOB)<br/>+ key backup<br/>+ recovery flow"]
    P4["P4 (2d)<br/>Surface parity:<br/>REST/MCP/CLI/Comm/PWA<br/>+ locale + mobile issue"]
    P5["P5 (2d, overlaps)<br/>Tests:<br/>unit + Synapse integration<br/>+ smoke section"]

    P1 --> P2
    P2 --> P3
    P3 --> P4
    P5 -.runs alongside P1-P4.-> P4
    P4 --> RELEASE["v6.7.0 cut"]
```

Total: **~16 working days**. Phase boundaries are good check-in points for operator UAT.

---

### Architecture — hybrid AS + bot account model (DP2 = D)

Two code paths. The backend reads `cfg.Matrix.ApplicationService.Enabled` at startup and picks one.

```mermaid
flowchart TD
    CFG[(cfg.Matrix)]
    CFG -->|as.enabled=true| AS[Application Service path]
    CFG -->|as.enabled=false| BOT[Bot user path]

    subgraph AS_path["AS path (self-hosted only)"]
      ASR[register.yaml<br/>generated by<br/>'datawatch setup matrix as-register']
      ASLISTEN[Daemon listens for<br/>HS → AS transactions<br/>POST /api/matrix/_as_txn]
      ASNS["Claims namespace:<br/>@datawatch_*:server<br/>!datawatch_*:server"]
      ASCRYPTO[Per-namespace device keys<br/>via mautrix/crypto]
    end

    subgraph BOT_path["Bot path (works on matrix.org + self-hosted)"]
      BU["Single user MXID:<br/>@datawatch:server"]
      BSYNC["Long-poll /sync"]
      BCRYPTO["Single device's keys<br/>via mautrix/crypto"]
    end

    AS --> ASR
    AS --> ASLISTEN
    AS --> ASNS
    AS --> ASCRYPTO
    BOT --> BU
    BOT --> BSYNC
    BOT --> BCRYPTO

    ASCRYPTO -->|same crypto store<br/>SQLite| STORE[(~/.datawatch/<br/>matrix-crypto.db)]
    BCRYPTO --> STORE
```

The `messaging.Backend` interface presented to `router.Router` is identical in both paths. The `matrix.Backend` struct internally branches on which transport it owns.

---

### Architecture — E2EE crypto store + recovery (DP4 = B/C)

The crypto store is the new persistent surface this work introduces. Loss of the store = loss of historical decryption until rekey, which is why backup is the key open question (Q4.3).

```mermaid
flowchart LR
    OP["Operator's device<br/>(Element)"]
    BOT["matrix.Backend"]
    HS[(Homeserver)]
    STORE[(matrix-crypto.db<br/>Olm sessions<br/>Megolm sessions<br/>device keys<br/>cross-sign keys?)]
    BACKUP[("Server-side<br/>Key Backup<br/>(if Q4.3=yes)")]

    OP -- m.room.encrypted --> HS
    HS --> BOT
    BOT <-->|read/write<br/>Olm/Megolm sessions| STORE
    BOT -- m.room.encrypted --> HS
    HS --> OP

    STORE -.rotated session keys<br/>encrypted with recovery key.-> BACKUP
    BACKUP -.restore on<br/>store wipe.-> STORE

    OP -. SAS verification<br/>(Q4.2 = a) .- BOT
```

**Open questions blocking detail in this diagram:**
- Q4.2 (SAS / OOB / auto-trust) determines what the dashed verification path looks like.
- Q4.3 (key backup yes/no) determines whether the BACKUP node exists.
- Q4.4 (cross-sign with operator MXID?) determines whether `cross-sign keys` is in STORE.

---

### Phase P1 — Backend foundation + AS hybrid wiring + adapter layer (~4 days)

**Goal:** Replace the 74-line stub with a feature-complete cleartext backend that the AS hybrid layer can sit on top of. Crypto comes in P2.

**Files touched:**
- `internal/messaging/backends/matrix/backend.go` — expand from 74 lines to ~400 lines
- `internal/messaging/backends/matrix/adapter.go` — **new** — thin shim over mautrix-go so a future SDK swap is contained
- `internal/messaging/backends/matrix/as.go` — **new** — Application Service registration + transaction handler
- `internal/messaging/backends/matrix/bot.go` — **new** — bot-user auth + sync loop
- `internal/config/config.go` — extend `MatrixConfig`:

  ```go
  type MatrixConfig struct {
      Enabled        bool                 `yaml:"enabled"`
      Homeserver     string               `yaml:"homeserver"`
      UserID         string               `yaml:"user_id"`
      AccessToken    string               `yaml:"access_token"`     // accepts ${secret:...}
      RoomID         string               `yaml:"room_id"`          // accepts !id or #alias (Q9.1/Q10.2)
      AutoManageRoom bool                 `yaml:"auto_manage_room"` // (Q9.2)
      DeviceID       string               `yaml:"device_id,omitempty"`
      DeviceName     string               `yaml:"device_name,omitempty"`
      Encryption     MatrixEncryptionCfg  `yaml:"encryption"`
      AS             MatrixASCfg          `yaml:"application_service"`
      ACL            MatrixACLCfg         `yaml:"acl,omitempty"`
  }

  type MatrixASCfg struct {
      Enabled         bool   `yaml:"enabled"`
      RegistrationFile string `yaml:"registration_file,omitempty"` // path to registration.yaml
      ASToken         string `yaml:"as_token,omitempty"`
      HSToken         string `yaml:"hs_token,omitempty"`
      Namespace       string `yaml:"namespace,omitempty"`           // default: "@datawatch_*"
      ListenAddr      string `yaml:"listen_addr,omitempty"`         // default: ":29333"
  }
  ```
- `cmd/datawatch/main.go` — extend the Matrix branch to wire AS path or bot path based on config; register AS HTTP handler if AS enabled
- `cmd/datawatch/main.go` — `datawatch setup matrix as-register` subcommand: generates `registration.yaml` from operator-supplied homeserver URL + namespace, prints the `homeserver.yaml` snippet to add

**Backend contract delivered in P1 (cleartext-only — encrypted rooms warn + skip):**
- `Send(recipient, message)` — full Matrix `m.text` send to room or DM
- `RichSender.SendMarkdown(recipient, markdown)` — converts to Matrix HTML formatted body (`format: org.matrix.custom.html`)
- `ThreadedSender.SendThreaded(recipient, message, threadID)` — uses Matrix `m.thread` relations
- `FileSender.SendFile(recipient, filename, content, threadID)` — Matrix `m.file` upload
- `Subscribe(ctx, handler)` — joins configured room (resolves alias → ID if needed); handles `m.room.message` for cleartext, emits warning for `m.room.encrypted`
- `Link(deviceName, onQR)` — used for the bot-path device pairing; AS path is no-op
- `SelfID()` — returns MXID (bot path) or namespace (AS path)
- `Close()` — stops sync, flushes pending sends

**Acceptance for P1:**
- `datawatch setup matrix` — works for bot path
- `datawatch setup matrix as-register` — works for AS path; outputs registration.yaml
- Send a cleartext message to a Synapse room from `datawatch matrix test` — message appears in Element
- Receive a message from Element — `router.Router` handles it (e.g., `sessions list` works in the room)
- Encrypted room in config — daemon logs `[matrix] room !X is encrypted; cleartext-only mode pending P2 (E2EE not yet active)` and falls through to read-only

---

### Phase P2 — E2EE crypto stack (~5 days)

**Goal:** Backend can decrypt inbound and encrypt outbound for any encrypted room it joins. Mautrix-go's `crypto/store` package handles persistence.

**New files:**
- `internal/messaging/backends/matrix/crypto.go` — wires `mautrix/crypto/Helper` to the backend
- `internal/messaging/backends/matrix/crypto_store.go` — `mautrix/crypto/sql_store_upgrade`-driven SQLite store at `~/.datawatch/matrix-crypto.db`

**Crypto subsystems wired:**
- **Olm** — pairwise per-device session establishment (used for key sharing)
- **Megolm** — per-room ratcheting session for the actual encrypted message stream
- **Device list updates** — when a user joins/leaves/changes devices, the megolm session has to rotate
- **Megolm session sharing** — when the bot starts a new session in a room, it shares the key to every verified device of every member via Olm
- **Room key request handling** — when the bot can't decrypt a message, it requests the key from the sender's other devices

**Edge cases that have to work in P2:**
- Joining an encrypted room mid-conversation — the bot won't have historical megolm keys; old messages stay opaque (this is by design, mirrored by Element)
- Operator's device list changes (new phone, removed old laptop) — bot has to update and re-share megolm session; a stale device list = "Unable to decrypt" on the operator's new device
- Daemon restart — on every restart the crypto store is reopened; sessions resume cleanly
- Crypto store unwriteable (disk full / permission lost) — daemon refuses to start the Matrix backend with a clear remediation error rather than silently sending cleartext into a previously-encrypted room

**Acceptance for P2:**
- Encrypted Synapse room — bot joins, decrypts incoming, encrypts outgoing
- Element shows the bot's messages decrypted with a "✓ verified" or "⚠ unverified" indicator (verification = P3)
- Restart daemon mid-conversation — encryption resumes from store; no loss
- Test client (`python-matrix-nio`) sends from an encrypted room → bot receives + handler fires

---

### Phase P3 — Verification + key backup + recovery (~3 days)

**Goal:** Operator can establish trust between their Element devices and the bot, and recover the bot's keys after a `~/.datawatch/` wipe.

**Open before P3 starts:** Q4.2 (verification mode), Q4.3 (key backup), Q4.4 (cross-sign).

**Three sub-phases depending on Q4.2:**

- **(a) SAS** — bot exposes `datawatch matrix verify-start <user-mxid>` which initiates `m.key.verification.start`; operator's Element pops up an emoji prompt; both sides confirm. PWA shows the same emoji sequence so the operator can verify without leaving the PWA (assuming Q9.3 = yes).
- **(b) Out-of-band key** — `datawatch setup matrix` prompts for a pre-shared key the operator generated in Element; the bot uses it to "manually verify" the device. Less secure, simpler.
- **(c) Auto-trust + audit** — every device the bot sees gets trusted; every trust event is audit-logged. Insecure but easiest. Only suitable if the bot's room is already access-controlled at the homeserver level.

**Key backup (only if Q4.3 = yes):**
- Generate a recovery key on first run; show in PWA + write to `~/.datawatch/matrix-recovery.key` (0600); operator stores it somewhere safe.
- Periodically push rotated megolm session keys to the homeserver's key backup endpoint, encrypted with the recovery key.
- On crypto-store wipe + recovery-key paste, restore historical session keys → bot can decrypt past messages again.

**Cross-signing (Q4.4):**
- **Separate identity** — bot has its own master key + self-signing key + user-signing key. Operator must verify the bot's master key once. Bot does not cross-sign the operator's other devices. Cleaner audit boundary.
- **Cross-sign with operator** — bot logs in as `@operator:server` (problematic; conflates bot + operator activity) OR operator manually signs the bot's device key with their user-signing key (more setup but cleaner identity). Recommend separate identity.

**Acceptance for P3:**
- Verification mode works end-to-end per the chosen Q4.2 path
- (If key backup) wipe `~/.datawatch/matrix-crypto.db`, paste recovery key, history decrypts again
- Audit log records every verification + every trusted device

---

### Phase P4 — Surface parity + locale + mobile issue (~2 days)

**Goal:** Every cell in §6's parity matrix is filled. PWA Settings card is operator-usable.

**REST endpoints added** (`internal/server/matrix.go` — new file):
- `GET /api/matrix/status` — returns `{enabled, mode (as|bot), homeserver, mxid, room, encryption, sync_state, last_event_ts, device_id, recovery_key_set}`
- `POST /api/matrix/test` — sends a test message to the configured room; returns the event ID
- `POST /api/matrix/verify-start` — body `{mxid}`; initiates SAS verification (or OOB token, depending on Q4.2)
- `POST /api/matrix/key-backup/enable` — generates recovery key, returns it ONCE; subsequent calls return `409`
- `POST /api/matrix/acl` — body `{allowed_mxids: [...]}`; updates ACL
- `POST /api/matrix/diagnose` — runs the full diagnose chain, returns structured results for the PWA

**MCP tools** (`internal/mcp/matrix.go` — new file):
- `matrix_status`, `matrix_test`, `matrix_verify_start`, `matrix_key_backup_enable`, `matrix_acl_set`, `matrix_diagnose`

**CLI subcommands** (`cmd/datawatch/main.go`):
- `datawatch matrix status` (new)
- `datawatch matrix test [room]` (new)
- `datawatch matrix verify <mxid>` (new)
- `datawatch matrix key-backup [enable|status|restore <recovery-key>]` (new)
- `datawatch matrix acl [add|remove|list] [mxid]` (new)
- `datawatch matrix as-register` (new — Phase P1)
- `datawatch diagnose matrix` (already exists; extended)

**Comm verbs** (`internal/router/commands.go`):
- `matrix status`, `matrix test`, `matrix verify`, `matrix acl`, `matrix diagnose`

**PWA** (`internal/server/web/app.js`):
- Settings → Comms → Matrix card (extends existing card structure)
- Status badge (green/yellow/red), homeserver/mxid display
- Test send button
- ACL editor (textarea of MXIDs, one per line)
- Encryption status: device verified ✓, key backup enabled ✓, recovery key shown once
- "Verify devices" button — kicks off SAS, shows emojis if Q9.3 = yes

**Locale keys** added to all 5 bundles (en/de/es/fr/ja) — estimated 30-40 keys covering: status labels, button labels, ACL editor labels, encryption strings, verification prompts, diagnose output.

**Mobile parity issue** filed at datawatch-app once v6.7.0 is tagged.

**Acceptance for P4:**
- Every cell in §6 marked ✓ in a follow-up audit
- PWA card shows status correctly
- All locale bundles validate as JSON
- `node --check internal/server/web/app.js` passes
- Mobile issue filed with the same key list + UI screenshots

---

### Phase P5 — Tests (~2 days, runs alongside P1–P4)

**Unit tests** (`internal/messaging/backends/matrix/*_test.go`):
- Mock `mautrix.Client`; assert Send routes correctly per recipient
- Subscribe ignores own sends; processes others; emits warning for encrypted in cleartext mode
- Alias resolution: `#name:server` → `!id:server`
- ACL: allow / deny matches expected
- AS hybrid: routing decision based on `cfg.Matrix.AS.Enabled`
- Crypto (P2): mock crypto store; encrypted send path produces `m.room.encrypted` event
- Crypto (P2): inbound `m.room.encrypted` decrypts via mock keys → handler called with plaintext

**Integration tests** (`scripts/test-matrix-synapse.sh`):
- Brings up Synapse + Element-web + nio test client via `docker-compose -f scripts/matrix-stack.yml up`
- Bot joins a room; nio client posts; bot receives; assert audit log
- Encrypted room: same flow; assert ciphertext at the wire, plaintext at the handler
- Daemon restart in the middle of a conversation: assert no message loss, encryption resumes

**Smoke section** added to `scripts/release-smoke.sh`:
```
== N. v6.7.0 BL241 — Matrix channel ==
  PASS  matrix status: backend enabled (mode=bot)
  PASS  matrix test: event delivered to room
  PASS  matrix encryption: bot decrypts test message from nio client
  SKIP  matrix not configured (gated on cfg.Matrix.Enabled)
```

Skip path keeps smoke green for operators who don't run Matrix.

---

### Plan II — risks, dependencies, mitigations

| Risk | Mitigation |
|---|---|
| **Crypto store corruption** mid-conversation | SQLite WAL mode; daemon refuses to start Matrix backend if store can't be opened cleanly; `datawatch matrix key-backup restore` recovers from backup |
| **Phase P2 takes longer than 5 days** | mautrix/crypto's API has changed across minor versions; budget contingency by keeping P1 + P5 cleanly cuttable as v6.7.0 even without P2-P4 (would re-plan as Plan I if needed) |
| **AS registration doesn't work on operator's homeserver** | Hybrid model means bot path is the always-works fallback; AS is opt-in |
| **Synapse Docker image churn** | Pin to a specific `matrixdotorg/synapse:v1.X.Y` tag in `scripts/matrix-stack.yml` |
| **Operator can't verify the bot device** | Document the OOB key path (Q4.2 = b) as the always-works fallback |
| **Federation outage during integration tests** | Integration tests use only the local Synapse — no cross-server federation tested in CI; cross-server is documented as a manual smoke step |

---

### Plan II — what does NOT ship in v6.7.0 (carried to v6.7.x or v6.8.0)

Per the Round-1 answers, these are explicitly out of v6.7.0:

- **DM-per-operator** (Shape β) — Q5.1 answered as single-room v1
- **Room-per-session** (Shape γ-extended)
- **Spaces** (DP5 D)
- **OIDC / SSO** — Q3.3 not answered yet; assumed v2 unless flagged
- **Bridge user pretty-printing** — DP8
- **Voice messages** — `m.audio` event handling
- **Multi-room / multi-account / multi-homeserver** — DP10 C/D

These get their own backlog entries when the v1 ships and operators ask for them.

---

## 8. Testing strategy

What "verified" means at each phase boundary.

| Test type | Coverage |
|---|---|
| **Unit** | Mock `mautrix.Client`; assert: Send routes to right room; Subscribe filters own messages; encrypted-room rejection emits the right warning; ACL allow/deny matches expectations; alias→ID resolution. |
| **Integration** | Spin up a local Synapse (Docker) in `scripts/test-matrix.sh`. Bot joins a room, exchanges messages with a `python-matrix-nio` test client, verifies bidirectional delivery + audit log entries. |
| **E2EE integration (Plan II only)** | Same harness with `m.room.encryption` enabled in the test room. Verify the bot can decrypt messages from the test client and the test client can decrypt the bot's messages. SAS emojis match. Key backup + restore works across crypto-store wipe. |
| **Smoke (`scripts/release-smoke.sh`)** | New section "§N. Matrix channel": registers a test peer, sends a test message via daemon API, asserts it appears in the audit log. Skip if `cfg.Matrix.Enabled` is false (consistent with how other channel sections skip). |
| **Manual** | PWA test-button round-trip; CLI `datawatch diagnose matrix`; comm verb `matrix test`; verifies the new locale keys render in all 5 languages. |

---

## 9. Risks + things that could derail this

- **mautrix-go API churn.** Major-version bumps in the past have broken downstream integrations. Mitigation: pin the minor; vendor if necessary; write adapter layer thin so a future SDK swap is feasible.
- **E2EE crypto-store corruption** (Plan II). Mitigation: `~/.datawatch/matrix-crypto.db` has its own SQLite file with WAL mode + the standard backup recipe; daemon refuses to start if the store can't be opened, with a clear remediation message.
- **Federation outages.** A user's homeserver going down means their messages stop arriving; the bot should not consider this a fatal error — it's a transient federation issue. Mitigation: surface as a warning, not a crash; alert via the existing alert stream.
- **Rate limiting.** matrix.org is aggressive about rate limits for bots that aren't application services. Mitigation: backoff on `M_LIMIT_EXCEEDED`; surface limits in `datawatch matrix status`; document in setup help that self-hosted homeservers are recommended for heavy use.
- **Large room joins.** Some rooms have 50k+ members and joining them downloads enormous state. Mitigation: surface a warning before joining a room with `>1000 members`; provide a `--lazy-load` option.
- **Bot user policy.** Some homeservers block bot registrations; matrix.org requires a CAPTCHA. Mitigation: documented in setup help; can't fix from datawatch side.
- **Bridge identity collisions.** Bridge ghosts may share an MXID prefix; if ACL is by MXID, this could accidentally allow bridged users in. Mitigation: ACL config supports MXID patterns, not just exact matches.

---

## 10. Out-of-scope (explicitly deferred)

These are NOT in BL241; they get their own backlog entries when relevant.

- **Application Service mode** (DP2 Option B / D). v2.
- **Spaces** (DP5 Option D). Separate BL.
- **Room-per-session** (DP5 Option C). Separate BL.
- **OIDC / SSO** (DP3 Option C). Separate BL.
- **Bridge user pretty-printing** (DP8). Separate BL.
- **Voice messages** (`m.audio` events ↔ Whisper). Could ride on the existing voice-input infra; separate BL.
- **Image / file uploads to a room.** `FileSender` interface implementation; separate BL after v1.
- **Matrix as the carrier for the inter-mesh control plane** referenced in BL243's "Future" notes. Separate BL once Tailscale mesh has a few production users.
- **Multi-homeserver / multi-account** (DP10 Option C). Separate BL.

---

## 11. Consolidated open questions for the operator

The full list, ordered by what blocks what. Answer in any format — these flow back into the per-DP sections.

### A. Foundational — answer these first; everything else depends on them

- ✅ **Q4.1** — E2EE in v1? — **Answered 2026-05-04: B/C (E2EE in scope from day one).**
- ✅ **Q-Phase.1** — Which implementation plan: I, II, or III? — **Answered: Plan II.**
- ✅ **Q-Phase.2** — Target version? — **Implied by Plan II: v6.7.0 minor.** _(Operator to confirm — assumed unless flagged.)_
- ✅ **Q-Phase.3** — Test homeserver? — **Answered: local Docker Synapse (operator dev box).**

### B. Account + auth

- **Q1.1** — Stay on **mautrix-go** (already in `go.sum` v0.22.0) or switch to **gomatrix**? _Lean: stay on mautrix-go._ See §11-Detail Q1.1.
- **Q1.2** — Pin mautrix-go minor version (deliberate bumps only)? See §11-Detail Q1.2.
- ✅ **Q2.1** — v1 = bot user only or hybrid AS + bot? — **Answered Round 1: D (Hybrid AS + bot fallback).**
- **Q2.2** — Does CLI offer **bot-user creation** (`datawatch setup matrix create-account`)? See §11-Detail Q2.2.
- **Q3.1** — v1 auth = **access-token paste only** (A), also **username+password login** (B), or **AS token** path when AS is configured (D)? See §11-Detail Q3.1.
- ✅ **Q3.2** — Secrets store policy for credentials — **Answered Round 2: secrets store mandatory; treat as a project-wide rule (see §11-Policy).**
- **Q3.3** — Is OIDC/SSO ever in scope? See §11-Detail Q3.3.

### C. E2EE (now load-bearing — Q4.1 answered B/C)

- ✅ **Q4.2** — Verification — **Answered Round 2: SAS (option a) out-of-the-box; out-of-band (option b) added in v2.** Auto-trust never.
- ✅ **Q4.3** — Key backup — **Answered Round 2: yes, key backup enabled by default; recovery key lives in the secrets store (not on disk).** v2 supports multiple backup mechanisms / multiple recovery secrets.
- ⚠ **Q4.4** — Bot identity model — **Answered Round 2: "storage identity" — interpreted as separate identity (option a). Pending operator confirmation; if intended otherwise, fix in place.**

### D. Routing + ACL

- ✅ **Q5.1** — v1 routing — **Answered Round 1: A (single room) for v1; plan for v2 expansion.**
- **Q5.2** — DM unknown-sender behaviour _(deferred to v2; flagged for v2 design)_.
- ✅ **Q5.3** — Embed `session_id` in v1 messages — **Answered Round 2: yes, embed; plan v2 layering on top.**
- **Q5.4** — **Spaces** — design the v1 room-naming convention now to leave room for nesting later? See §11-Detail Q5.4.
- **Q6.1** — Federation: **bot's homeserver only** or **anywhere it's invited**? See §11-Detail Q6.1.
- **Q6.2** — Federated-sender behaviour. See §11-Detail Q6.2.
- ✅ **Q7.1 + Q7.2** — ACL in v1 — **Answered Round 2: match Signal (no ACL — trust everyone in the configured room); plan v2 lockdown.**

### E. Bridges + UX + config

- **Q8.1** — Confirm bridges = **out of scope for v1**. See §11-Detail Q8.1.
- **Q9.1** — v1 supports **room aliases** (`#datawatch:matrix.org`)? See §11-Detail Q9.1.
- **Q9.2** — `auto_manage_room` (auto-create + invite operator) — v1 or v2? See §11-Detail Q9.2.
- ✅ **Q9.3** — Verification surface — **Answered Round 2: full Configuration Accessibility surface — CLI + REST + MCP + PWA + Comm channels per the project rule.**
- **Q10.1** — v1 YAML stays **single-account, single-room** plus secret refs? See §11-Detail Q10.1.
- **Q10.2** — `room_id` accepts **room alias** transparently? See §11-Detail Q10.2.

---

## §11-Policy — implications of Round 2 answers

The Round 2 answer to Q3.2 is bigger than BL241. **"Secrets store for everything; this should be a rule"** is a project-wide directive that applies to every credential-bearing config field across every channel and integration, not just Matrix.

✅ **Q-Policy.1 — Answered 2026-05-04: Project-wide rule, retroactive on next-touch (Option Policy.B).**

The rule is being added to `AGENT.md` in the same commit as this update:

> **Secrets-Store Rule** — All credential-bearing config fields (access tokens, API keys, passwords, signing secrets, recovery keys, webhook tokens, etc.) must accept and prefer `${secret:...}` references resolved from the BL242 secrets manager. YAML plaintext is **deprecated** for new fields and **removed** for each existing backend the next time it is opened for substantive work. New backends ship secrets-store-only from day one. A separate backlog item tracks the audit + retroactive sweep across already-shipped backends.

Implications for backlog: a new BL gets filed for the audit + sweep across Signal/Telegram/Slack/Discord/Ntfy/Twilio/GitHub/SMTP/etc. credential fields. That work runs at operator-driven cadence; it does not block BL241.

---

## §11-Detail — expanded lower-priority questions

Operator asked for more detail on the "lower-priority" items so they can decide rather than accept defaults. Each gets options, tradeoffs, and a recommendation that is **not** a decision.

### Q1.1 — Stay on mautrix-go, or switch to gomatrix?

| Option | Pros | Cons |
|---|---|---|
| **A. mautrix-go** (`maunium.net/go/mautrix`) | Already in `go.sum` v0.22.0; first-class crypto via `mautrix/crypto`; same author as the bridge ecosystem; crypto store SQL upgrade scripts ship with it | Single maintainer (Tulir Asokan); occasional API churn between minor versions; some types reach into Mautrix-specific extensions |
| **B. matrix-org/gomatrix** | Matrix Foundation reference; smaller surface | No crypto package — we'd bolt `mautrix/crypto` on anyway; sporadic releases; we'd write more glue code |
| **C. element-hq matrix-rust-sdk via cgo** | Best crypto in the ecosystem; actively developed | cgo + cross-compile pain (darwin / windows / arm64); huge dependency tree |

**Recommendation:** A — already in tree, has the only Go crypto implementation that matters, and even gomatrix users typically import mautrix/crypto for E2EE.

---

### Q1.2 — Pin mautrix-go minor version?

| Option | Behavior |
|---|---|
| **A. Pin minor in go.mod** (e.g., `maunium.net/go/mautrix v0.22.0` exact) | Deliberate `go get -u maunium.net/go/mautrix@v0.23.x` upgrades only; surface API changes in PRs |
| **B. Allow patch upgrades** (`v0.22.x`) | `go get -u` picks up patches automatically; minor bumps still deliberate |
| **C. Float on latest** | Routine `go mod tidy` may pull breaking changes silently |

**Recommendation:** B — patch upgrades for security fixes, deliberate minor bumps for API churn. Matches how we treat other security-sensitive deps.

---

### Q2.2 — CLI bot-user creation tooling?

The operator must have a Matrix user before they can paste an access token. Today they go to Element → register manually. Should `datawatch` help?

| Option | Behavior |
|---|---|
| **A. None — operator does it manually** | Documented in setup help: "create a Matrix user via your homeserver's signup flow first" |
| **B. `datawatch setup matrix create-account`** | Prompts for homeserver URL + desired username + password; calls `POST /_matrix/client/v3/register`; works on homeservers with open registration; fails clearly on closed homeservers (matrix.org behind CAPTCHA, most self-hosted) |
| **C. Same as B + reCAPTCHA solver shim** | Auto-completes the matrix.org reCAPTCHA flow via a browser callback; fragile; against matrix.org's intent |

**Recommendation:** A for v1; B as a v6.7.x patch if operators ask. C never.

---

### Q3.1 — Auth method order

With AS hybrid (Q2.1 = D) chosen and secrets-store-mandatory (Q3.2 = secrets store), the auth surface has three paths. Which does v1 cover?

| Path | When used | Effort | Required for v1? |
|---|---|---|---|
| **Access token paste** (existing stub) | Bot path on any homeserver | 0 days (already works) | **Yes** |
| **Username + password → /login** | Bot path; daemon does login round-trip; stores the resulting token in secrets store | ~0.5 day | **Optional** — better UX but access-token path covers it |
| **AS token (registration.yaml)** | AS path; bot has full namespace authority | ~1 day (P1 already plans this) | **Yes** — required for AS hybrid |

**Recommendation:** v1 = access-token + AS-token. Username+password is a v6.7.x patch.

**Q3.1.confirm — confirm v1 ships with access-token + AS-token only, password→login deferred to v6.7.x?**

---

### Q3.3 — OIDC / SSO ever in scope?

Matrix is rolling out OIDC as the standard auth flow (replacing /login). Some homeservers (e.g., `beeper.com`, corporate Synapse with Keycloak) only support OIDC.

| Option | Behavior |
|---|---|
| **A. Never** | Operators on OIDC-only homeservers can't use datawatch's Matrix integration; they generate an access token via Element and paste |
| **B. Backlog for v6.8.0+** | File as separate BL; not in BL241 scope |
| **C. v1 stretch goal** | Implement a minimal OIDC device-code flow; ~3 day effort |

**Recommendation:** B — OIDC is its own work. Most operators can paste an Element-generated token even on OIDC homeservers.

**Q3.3.confirm — defer OIDC to a future BL (B)?**

---

### Q5.4 — Spaces v1 design hooks?

Spaces are Matrix's "rooms-of-rooms" abstraction. Even if v1 doesn't use Spaces, the room name + topic chosen now affects how cleanly Spaces (or room-per-session) lands in v2.

| Option | v1 room naming convention |
|---|---|
| **A. Operator-provided room name** | `room_id` is whatever the operator passes; no convention enforced |
| **B. Convention `datawatch-{hostname}` for auto-created rooms** | If `auto_manage_room=true`, daemon names the room after the hostname; operator can override |
| **C. Convention + Space-ready naming** (`datawatch-{hostname}-main`, with future `datawatch-{hostname}-session-{id}` siblings nested under a `Datawatch ({hostname})` Space in v2) | Sets up the Space tree non-destructively in v1 |

**Recommendation:** B for v1 (operator provides; auto-create uses convention). C is over-engineered before we know operators want Spaces.

**Q5.4.confirm — option B (operator-provided + convention only when auto-creating)?**

---

### Q6.1 — Federation policy for the bot

Should the bot accept invites from rooms hosted on other homeservers?

| Option | Behavior |
|---|---|
| **A. Bot's homeserver only** | Bot refuses invites from rooms on other homeservers; cleanest isolation; loses Matrix's federated nature |
| **B. Anywhere it's invited** | Bot joins any room it's invited to; matches Matrix's design intent; operator controls via not-inviting |
| **C. Allow-list of homeservers** | `matrix.federation.allowed_servers: [example.com, other.org]`; default to A if list empty |

**Recommendation:** B for v1 (matches Matrix design; operator gates by not inviting). A is unnecessarily restrictive; C is configurable v6.7.x patch if operators ask.

**Q6.1.confirm — option B?**

---

### Q6.2 — Federated-sender behaviour

When a federated user sends a message in a room the bot is in:

| Option | Behavior |
|---|---|
| **A. Process same as local** | Identical to Q7 ACL (which Round 2 = no ACL); federated users issue commands the same as local users |
| **B. Process + tag source in audit log** | A + audit log entry includes `source_homeserver` field |
| **C. Ignore unless allow-listed** | Federated users' messages are stored but not processed by router; v1 doesn't support allow-list so this defaults to "ignore everyone federated" |

**Recommendation:** B — process them, tag the source. With Round 2 ACL = none, every sender is trusted; tagging gives audit trail without changing behavior.

**Q6.2.confirm — option B?**

---

### Q8.1 — Bridges out-of-scope confirmation

Matrix bridges (Signal/Telegram/Slack/etc.) are transparent to the bot — it sees Matrix events; it doesn't know they're bridged. Two minor concerns:

- **Bridge-ghost MXIDs** look like `@signal_+15555550100:matrix.example.com`. With ACL = none (Round 2), no impact in v1.
- **Sender display names** from bridges are bridge-controlled.

**Recommendation:** Confirm bridges = out-of-scope for v1; revisit when an operator hits a bridge UX issue.

**Q8.1.confirm — out-of-scope confirmed?**

---

### Q9.1 — Room alias support

| Option | Behavior |
|---|---|
| **A. `room_id` field accepts room ID only** (`!abcdef:matrix.org`) | Operator must look up the ID in Element |
| **B. `room_id` accepts ID or alias** (`#datawatch:matrix.org` resolved at startup) | Friendlier UX; one extra `/_matrix/client/v3/directory/room/{alias}` call at startup |
| **C. Separate fields** (`room_id` and `room_alias`) | Explicit but redundant |

**Recommendation:** B — keep one field, accept either.

**Q9.1.confirm — option B?**

---

### Q9.2 — `auto_manage_room` (already declared in MatrixConfig but unused)

| Option | Behavior |
|---|---|
| **A. Defer entirely** | Remove the unused flag; operator creates rooms manually |
| **B. v1 implementation** | When `auto_manage_room=true` and `room_id` is empty, daemon creates a room named per Q5.4 convention, invites the operator (`user_id` from config? or a configured `operator_mxid`?), persists the resulting `room_id` to YAML on first run |
| **C. v6.7.x patch** | Same as B but punted to a follow-up |

**Recommendation:** B if operator answers a follow-up "what MXID gets invited?"; otherwise C. Implementation is ~half a day.

**Q9.2.confirm — option B (specify which MXID gets the auto-invite) or C?**

---

### Q10.1 — YAML schema shape

| Option | Shape |
|---|---|
| **A. Stay with single account, single room** (today's stub, extended) | `matrix: {enabled, homeserver, user_id, ..., room_id, encryption: {...}, application_service: {...}}` |
| **B. Single account + room list** | `matrix: {homeserver, user_id, ..., rooms: [...]}` — wins when v2 lands DMs / room-per-session |
| **C. Multiple accounts** | `matrix: [{...}, {...}]` — multi-homeserver |

**Recommendation:** A for v1. The `rooms: []` list (B) can be added in v2 without breaking A by parsing both shapes (A becomes shorthand for a single-element B). Multi-account (C) is multi-homeserver; deferred indefinitely.

**Q10.1.confirm — option A?**

---

### Q10.2 — Alias resolution mechanic

If Q9.1 = B (alias accepted in `room_id`):

| Option | Behavior |
|---|---|
| **A. Resolve once at startup; cache the ID** | Fast; alias rename invalidates cache until daemon restart |
| **B. Resolve on every event** | Always current; one extra request per event |
| **C. Resolve at startup + on `m.room.canonical_alias` event** | Best of both; fires on alias rename |

**Recommendation:** C — Matrix already publishes `m.room.canonical_alias` state changes; subscribing is one event handler.

**Q10.2.confirm — option C?**

---

## 12. References

- [Matrix specification index](https://spec.matrix.org/latest/) — entry point for Client-Server, Application Service, Federation, Identity Service APIs.
- [`maunium.net/go/mautrix`](https://github.com/mautrix/go) — primary Go SDK; current version in `go.sum` is v0.22.0.
- [mautrix/crypto subpackage](https://pkg.go.dev/maunium.net/go/mautrix/crypto) — Olm/Megolm implementation if E2EE is in scope.
- [matrix-org/synapse](https://github.com/element-hq/synapse) — reference homeserver for integration tests.
- [matrix-appservice-bot examples](https://github.com/turt2live/matrix-bot-sdk) — patterns for bot-user vs AS account models, even though our impl is Go-side.
- BL242 (closed v6.4.7) — secrets store integration target for `${secret:matrix-token}`.
- BL244 (closed v6.3.0) — Plugin Manifest v2.1; Matrix is **not** a plugin (it's a first-class comm channel like Signal), but the comm-command routing pattern is the same.
- AGENT.md §382 (Configuration Accessibility Rule) — drives the parity matrix in §6.
- AGENT.md §406 (Localization Rule) — drives the locale + mobile issue requirement.
- AGENT.md §228 (Release-discipline rules) — drives the phase plan in §7.

---

## Appendix A — answers log

### Round 1 — 2026-05-04 (the four foundational questions + one gating question)

| Q | Answer | Effect |
|---|---|---|
| **Q4.1** — E2EE in v1? | **B/C — E2EE in scope from day one.** | Triggers crypto stack work in P2 + P3 of Plan II. mautrix/crypto, megolm session management, SAS or alternative verification, key backup question opens. |
| **Q-Phase.1** — Plan? | **Plan II — encrypted from day one.** | Target v6.7.0 minor (~16 days). E2EE work is on the v1 critical path, not deferred. |
| **Q-Phase.3** — Test homeserver? | **Local Docker Synapse on the operator dev workstation.** | `scripts/test-matrix-synapse.sh` brings up Synapse + Element-web in compose; integration tests run against it; production target homeserver picked separately when shipped. |
| **Q5.1** — Routing v1? | **A — single room for v1; plan for v2 expansion.** | DMs / room-per-session / Spaces all deferred. v1 message format embeds `session_id` so v2 layering is non-breaking (recommendation pending Q5.3 confirm). |
| **Q2.1** — Account model? | **D — Hybrid AS + bot fallback.** | When AS registration is configured (homeserver admin), use it. When not (e.g., operator is on matrix.org), fall back to a bot user. Two code paths to test. |

### Round 2 — 2026-05-04

| Q | Answer | Effect |
|---|---|---|
| **Q4.2** — Verification mode | **SAS (option a) out-of-the-box; out-of-band (option b) added in v2.** | P3 implements SAS as primary path; OOB hooks left in place but not wired to PWA in v1. Auto-trust never appears. |
| **Q4.3** — Key backup | **Yes — key backup enabled by default. Recovery key lives in the secrets store (not on disk). v2 supports multiple backup secrets / mechanisms.** | P3 generates recovery key on first run, writes it as `${secret:matrix-recovery-key}` (auto-creating if absent), uses it to encrypt megolm session backups. Restore reads from the secrets store. v2 backlog item: multi-secret backup rotation. |
| **Q4.4** — Bot identity | **"Storage identity"** — interpreted as **separate identity (option a)**. ⚠ Pending operator confirmation; if intended otherwise, the doc gets fixed in place. | P3 generates the bot's own master key + self-signing key + user-signing key; operator verifies the bot's master key once via SAS; bot does not cross-sign the operator's other devices. |
| **Q9.3** — Verification surface | **Full Configuration Accessibility surface** — CLI + REST + MCP + PWA + Comm channels, per the project rule. | P4 ships verify-start endpoints on all 5 surfaces. PWA shows the SAS emoji sequence so verification can complete without leaving the PWA. |
| **Q3.2** — Secrets-store policy | **Secrets store mandatory; this should be a project-wide rule.** | Matrix v1 ships secrets-store-only (no plaintext access tokens in YAML). The project-wide implications are flagged in §11-Policy as a follow-on for operator decision. |
| **Q7.1 + Q7.2** — ACL | **Match Signal — no ACL in v1; trust everyone in the configured room. Plan v2 lockdown.** | P1 omits ACL config block; v2 backlog item: per-MXID + per-power-level ACL. |
| **Q5.3** — `session_id` embed | **Yes — embed in v1 messages; plan v2 layering on top.** | P1 emits each outbound `m.room.message` with a custom `m.datawatch.session` field in `content` carrying `{session_id, host, role}`. Inbound parsing reads the field if present (backward-compat fallback to "no session" when sent from Element by hand). Used in v2 to drive room-per-session routing without changing the wire format. |

### Round 3 — _pending operator answers to §11-Detail expansions + §11-Policy scope_

The next round consists of:

- **§11-Policy Q-Policy.1** — Scope of "secrets store for everything" (BL241-only / project-wide rule / hard cutover before v7.0)
- **§11-Detail clarification questions** — confirm or override the recommendations on each lower-priority question. The `.confirm` Qs in §11-Detail are the explicit yes/no/override slots.

After Round 3, §7B's Plan II is finalised (most likely no shape change — just resolved details on AS registration UX, room aliases, federation policy, bridge out-of-scope confirm).
