# TS-825 header Tags dropdown run

- Command: `env -u TMUX -u TMUX_TMPDIR bash scripts/e2e-pwa.sh TS-825`
- Date: 2026-10-28
- Daemon origin: sandbox daemon started by the script on a free 127.0.0.1 port (http://127.0.0.1:34819, TLS 50683); never production 8443
- Runner: `scripts/e2e-pwa.sh` (not `scripts/run-tests.sh --stories=TS-825`, which hits the bash judge-size story)
- Exit code: 0
- Assertions: no assertion on the removed visible word "Tags" or the removed row edit button was found, so `scripts/test-stories/pwa/TS-825.mjs` is unchanged.
- Runner fix: `scripts/e2e-pwa.sh` captured `td_start` with `$(...)`, which prints no PID, so `DAEMON_PID` was empty and the daemon was stopped on subshell exit. It now calls `td_start` directly and reads `TD_LAST_PID`.

pass: 1
fail: 0

The story printed 9 PWA/REST check lines plus the cleanup line, all ok.
