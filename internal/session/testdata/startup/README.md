# Startup dialog fixtures

Each `.txt` file here is a reconstructed capture of one Claude Code
startup dialog variant (or a normal idle prompt), one variant per file:

| File                 | Classifies as         |
|----------------------|-----------------------|
| `trust-folder.txt`       | `trust-folder`  |
| `bypass-accept.txt`      | `bypass-accept` |
| `dev-channels.txt`       | `dev-channels`  |
| `mcp-trust.txt`          | `mcp-trust`     |
| `confirm-continue.txt`   | `confirm-continue` |
| `confirm-unsafe.txt`     | `confirm-unsafe`  |
| `update-prompt.txt`      | `update-prompt`   |
| `unknown-confirm.txt`    | `unknown-confirm` |
| `idle-prompt.txt`        | `none`            |

The fixtures were **reconstructed from observed wording** (the dialog
text quoted in the daemon's existing prompt-pattern lists and the
recorded self-update transcript at
`../claude_self_update_dialog.txt`), not copied verbatim from a live
pane. Box-drawing frame characters and the `❯` selector are included so
the fixtures exercise the same shape as real tmux pane captures.

**Replace every file in this directory with a live recording when one
exists** — a `tmux capture-pane` of the real dialog, saved raw (with
ANSI escapes) or stripped, is strictly better than the reconstruction.
