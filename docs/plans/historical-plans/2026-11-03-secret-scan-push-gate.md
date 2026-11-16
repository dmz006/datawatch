# Secret scan before daemon push

Status: shipped in 9.76.0 as shadow-by-default; follow-up Automaton files the remaining tests, audit and stats work.

## Parity surface

| Surface | Included | Reason |
|---|---|---|
| REST | yes | `GET`/`PUT /api/config` carry `git.secret_scan.*` with validation |
| MCP | yes | `secret_scan_config_get` and `secret_scan_config_set` |
| CLI | yes | `datawatch config set git.secret_scan.mode ...` through the generic config surface |
| Comm channel | yes | the generic `configure` verb |
| YAML/config | yes | `git.secret_scan` block in `docs/config-reference.yaml` |
| PWA | yes | settings fields with five locales |
| Android | yes | datawatch-app issue filed at the merge |
| iPhone | yes | same issue |
