# CHECKPOINT

- 2026-10-11T18:27:39-04:00 — Wire plugin service supervision into daemon
- done: main.go wiring (StartManaged + deferred StopAll 30s), daemon-actor audit adapter, enable {managed} body + 400, MCP managed arg, tests; go test ./... + node --test green; gofmt/vet/gosec clean on changed lines
- now: committed
- remaining: none
