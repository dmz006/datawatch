# Default build supervisor pack

The pack a build supervisor reads on every fresh session. See
`docs/howto/supervisor-fresh-sessions.md` for how it is used, how to
change it and how to build and install your own.

| File | What it is |
| --- | --- |
| `pack.yaml` | The manifest: names the files below |
| `rules.md` | The default rules layer of the handover |
| `handover.md.tmpl` | The template a supervisor's start-up text follows |
| `watch.yaml` | The light cadence pass |
| `window-guard.yaml` | The usage-window projection pass |
| `tick.yaml` | The full pass |
| `automation/` | One build module per stack: Go, Python, docs site |
