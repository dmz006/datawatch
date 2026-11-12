# Backend Session State

Every session shows one of three lifecycle states — **running**, **waiting_input**,
**stalled** — plus terminal ones when the turn finishes. This page explains, for each
backend family, how the state is decided while the session is open: what counts as
working, when `waiting_input` is shown, when `stalled` is shown, and what the default
idle timeout is. The per-family classifications below follow the busy-state gate audit
([plans/2026-11-03-busy-state-gate-report.md](plans/2026-11-03-busy-state-gate-report.md)).

The guiding rule is simple: **silence must never be read as "waiting for a human"
when the backend is still working.**

---

## How the decision is made

The state engine re-examines each open session periodically. For one backend and one
moment in time it answers *running*, *waiting_input*, *stalled*, or *no opinion* (in
which case the legacy silence gap-watcher keeps its existing behaviour). It decides
from four caller-supplied facts — it derives nothing from outside:

1. **Process alive** — the session's pane process still has a live child process.
2. **Last activity** — the freshest of the channel events and the transcript / output
   file's modification time. A turn that is writing the output file counts as work
   even though the pane says nothing.
3. **At a real input prompt** — the pane is currently showing the backend's own input
   prompt (an input box line or a shell prompt on one of the last lines of the pane).
   Only this signal produces `waiting_input`.
4. **The two time limits**: the **liveness window** (how long an alive worker may be
   silent before it is no longer "writing") and the **idle timeout** (the silence gap
   that may flip a genuinely idle interactive session to `waiting_input`).

The rules, in priority order:

- **Process gone while the record is still open → `stalled`, never `waiting_input`.**
  A dead worker is not a patient one.
- **Print-mode, alive, fresh activity within the liveness window → `running`.**
- **Print-mode, alive, silent beyond the liveness window → `stalled`.** Print-mode
  backends are headless one-run adapters: they never wait on a human, so a silent
  alive process is a stall signal, not an input signal.
- **Interactive, alive, at a real input prompt → `waiting_input`.**
- **Interactive, alive, no prompt, fresh activity within the window → `running`.**
- **Interactive, alive, silent beyond the window** — family-specific: `claude-code`
  keeps its busy-probe semantics and stays `running` (silence alone never stalls a
  busy Claude agent); for the other interactive families the legacy gap-watcher keeps
  its behaviour as a guard band, and the cross-package silence check (used by the
  autonomous supervisor watch) reports the case as stalled when it opts in.

The liveness window defaults to **120 s** (see
[Config keys](#config-keys)); the classifier's own fallback when no config is
available is a shorter built-in window — either way a legitimately quiet turn must fit
inside the window.

---

## Per-family behaviour

| Family | Kind | What counts as working | `waiting_input` is shown | `stalled` is shown | Default idle timeout |
|---|---|---|---|---|---|
| `pi-prompt` | print-mode | Process alive **and** fresh activity — the output file the one-shot `pi --print` turn writes, and the channel events from it. The pane is quiet for the whole turn; the pane returning to the shell means the turn finished, it does not mean "typing a prompt". | Only at a real input prompt — practically, after the turn has completed and the pane is usable again. Never from silence. | Process gone, or alive but silent beyond the liveness window. | **Off (0)** — print-mode default |
| `pi` | interactive, real prompt | The TUI streaming turns into the pane; the live pane itself is the activity signal. | When `pi`'s own input prompt is visible in the pane. | Process gone. | 10 s (global default) |
| `opencode` | interactive, real prompt | TUI running on the alternate screen, so mid-turn frame updates in the pane are the main signal; the idle markers recognised by the daemon mark a true idle. | When the idle input prompt ("Ask anything" footer) is visible. | Process gone. | 10 s (global default) |
| `opencode-acp` | event-stream | Working state is reported structurally: every event on the backend's event stream is a channel event, and the backend announces idle itself. The pane only carries server logs. | Only from the backend's own idle announcement. | Process gone. | ≥ 5 min — the daemon keeps at least a five-minute silence guard band, purely to catch a dropped event stream |
| `goose` | interactive, real prompt | The TUI streaming turns; the pane is live while the agent works. | When `goose`'s input prompt (or its approval dialog) is visible. | Process gone. | 10 s (global default) |
| `gemini` | one-shot, Go-mediated | A blocking child of the daemon; the pane is silent for the whole turn, so liveness must come from **process alive + activity**, not from the pane. | Only at a real input prompt after the turn returns. | Process gone, or alive but silent beyond the liveness window. | 10 s (global default) — silence alone never decides the state while the process is alive and the window has not run out |
| `aider` | one-shot, print-mode | One-shot launch that streams chat/diff output once it starts; before the first token there is a silent stretch with no prompt in the pane, so liveness comes from process alive + activity. | Only at a real input prompt after the outcome is reported. | Process gone, or alive but silent beyond the liveness window. | 10 s (global default) |
| `ollama` | interactive, real prompt (REPL) / streamed (chat mode) | REPL mode streams model replies into the pane; chat mode streams API chunks in from Go. Both keep the channel alive while working. | When ollama's `>>>` input prompt is visible. | Process gone. | 10 s (global default) |
| `openwebui` | one-shot, print-mode | Each turn's streamed chunks are the activity; no family-owned input prompt ever appears in the pane, so a pre-first-token silence is decided by the liveness rule, not by the silence gap. | Only at a real input prompt. | Process gone, or alive but silent beyond the liveness window. | 10 s (global default) |
| `claude-code` | interactive, busy probe | A busy TUI: the pane probe looks for work markers and a live child process. Silence alone does not end the "working" reading — the busy probe deliberately keeps a silent-but-busy session `running`. | When the real input prompt is visible in the pane. | Process gone (silence alone never stalls it). | 10 s (global default), governed through the busy probe |

Notes on the vocabulary (from the audit report):

- **Print-mode** — one-turn, non-interactive launches that are pane-silent or
  pane-quiet while the turn is in flight. Classified by the activity window, not by a
  prompt in the pane.
- **Interactive with a real prompt** — the pane shows the backend's own input prompt
  when idle; the flip to `waiting_input` is legitimate only there.
- **Guard band** — for families that already report idle structurally, the
  silence-based watcher is a fallback, not the working-state signal
  (`opencode-acp` is the main example).

---

## Reading the states

- **running** — the process is alive and either writing (fresh transcript/output
  activity or live pane/channel activity within the liveness window) or, for
  `claude-code`, still busy by the pane probe.
- **waiting_input** — shown **only at a real input prompt** for interactive
  backends. For print-mode and one-shot backends it is not a silence verdict: a
  silent but alive worker is `stalled` instead, so an alert on `waiting_input` always
  means "the backend is asking for something to be typed or confirmed".
- **stalled** — the process is gone while the record is still open, or a print-mode
  process has been silent beyond the liveness window. A stall is a signal to look at
  the worker (attach to the pane, check the transcript, kill and reschedule) — not a
  prompt to type into the pane.
- **No opinion** — for silent interactive backends outside `claude-code` where the
  caller has not opted in to stalling, the classifier defers to the legacy
  silence gap-watcher, so the historical behaviour is preserved.

---

## Config keys

All keys are under `session:` in the daemon config and are resolved through the
effective-value accessors — raw zeros in the config are a sentinel ("use the
built-in default"), not a value to read directly.

### `session.liveness_window_seconds`

The liveness silence window in seconds: how long an alive worker may go without fresh
activity (transcript/output file change or channel event) before the liveness rule
stops calling it "writing" and treats the silence as a stall (for print-mode) or as
no-opinion (for the other interactive families).

- **Default: 120 s.** `0` or absent means the built-in default (120); a positive
  value is used as-is; negative values are treated like `0`.
- Longer than the 15 s default silence gap, because a one-shot turn can
  legitimately be quiet for a while (model thinking, large file writes, slow first
  token).
- Raise it if you see `pi-prompt` / one-shot workers stalling mid-turn on slow
  models; lower it if you want faster stall detection and accept that slow, quiet
  turns will be flagged.

**Where to set them.** Both keys are carried by the YAML file, the REST config
routes, the MCP `config_set` tool, `datawatch config set`, the comm-channel
`configure` command, and the web Settings page. Every surface applies the same
validation: a negative number is refused, and a backend name that is not a known
backend or family is refused rather than silently ignored.

```bash
datawatch config set session.liveness_window_seconds 180
datawatch config set session.liveness_idle_timeout_overrides '{"pi": 30, "goose": 0}'
```

### `session.liveness_idle_timeout_overrides`

Per-backend input-idle timeout overrides, as a map of backend names to seconds.
Keys are **backend family names** — `"pi"` matches `"pi-prompt"` — or the full
backend name. Works for every family that does not end in `-prompt` too, e.g.

```yaml
session:
  liveness_idle_timeout_overrides:
    pi: 30          # pi-prompt: 30 s before silence may decide its state
    goose: 0        # goose: silence-based flip disabled entirely
    claude-code: 25 # claude-code: 25 s idle timeout
```

Resolution order for a backend named `X` (the effective-value rule callers must use):

1. **An explicit override for `X` (or for its family) wins — including an explicit
   `0`.** `0` in a key means "off", not "unset".
2. **Otherwise a print-mode backend (name ends in `-prompt`, e.g. `pi-prompt`)
   defaults to `0` — off.** Headless one-run launches never wait for a human input,
   so the interactive 10-second input-idle clock does not apply; silence is decided
   by the liveness window instead.
3. **Otherwise the global `session.input_idle_timeout` applies**, itself defaulting
   to **10 s** when `0` or unset.

### `session.input_idle_timeout`

The global input-idle timeout in seconds (default **10**). It is the silence gap
after which a genuinely idle **interactive** session may be flipped to
`waiting_input`. It does not apply to print-mode backends (their effective idle
timeout is off unless an explicit override says otherwise), and it is a floor that
the daemon raises for `opencode-acp` (≥ 5-minute guard band) because that backend
reports idle explicitly and a slow model can be silent well past 10 s mid-turn.

---

## Why both a liveness window and an idle timeout

They answer different questions for different families:

- The **liveness window** (default 120 s) says: *"Is this alive worker still
  working, or has it gone quiet?"* It governs print-mode and one-shot backends,
  where silence is the only thing to look at, and it is what turns an
  alive-but-silent print-mode worker into `stalled`.
- The **idle timeout** (global 10 s, overridable per backend) says: *"Is this
  interactive session genuinely waiting for input?"* It governs the interactive
  families, where the real input prompt is the positive signal and silence alone is
  a fallback.

For a print-mode worker the two compose as: **process gone → stalled; alive and
writes within the window → running; alive and silent beyond the window → stalled;
never a `waiting_input` from silence.** For an interactive worker: **process gone →
stalled; live and writing → running; real prompt → `waiting_input`; silent
beyond the window → the legacy gap-watcher or the supervisor's stall view decides,
family by family.**

---

Related: [llm-backends.md](llm-backends.md) · [backends.md](backends.md) ·
[config-reference.yaml](config-reference.yaml) ·
[plans/2026-11-03-busy-state-gate-report.md](plans/2026-11-03-busy-state-gate-report.md)
