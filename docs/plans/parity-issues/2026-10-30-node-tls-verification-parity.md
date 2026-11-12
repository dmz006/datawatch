# [sync] fix(compute): node certificate verification settings (datawatch next patch release)

**Not filed yet.** The supervisor files this on the `dmz006/datawatch-app` repository with `gh issue create` at merge. The issue title is the heading above and the issue body is the text below, from "Parent release" through "Acceptance (mobile)". Whoever files it records the issue number in the Filing record at the end.

Parent release: datawatch next patch release

## What changed in datawatch

The supervisor watch reads which models a compute node has resident by calling the node's `/api/ps` over `https://`. That probe used to accept any certificate. It now verifies the node's certificate by default, against the system roots (the daemon process honours `SSL_CERT_FILE` and `SSL_CERT_DIR`; there is no daemon-wide CA setting). A node whose certificate the system roots do not cover needs one of three settings on its record:

- `tls_ca_file`: path, on the daemon's machine, to a PEM CA bundle trusted in addition to the system roots. For a node signed by a private CA.
- `tls_cert_fingerprint`: the SHA-256 fingerprint of the node's certificate. For a single self-signed certificate. With only the fingerprint set, the presented certificate must match and nothing else is checked; with a CA file as well, the chain and host name must verify and the certificate must match.
- `tls_insecure_skip_verify`: turns certificate checking off for that node. Off by default. It cannot be combined with the other two.

A self-signed node with none of the three now fails the probe. The supervisor watch then treats the node as unknown and raises no finding for it, and the error names the fix. The settings only matter for nodes whose address starts with `https://`.

The skip setting is made visible so it cannot be left on unnoticed: the node record carries it, the PWA node row shows a red badge, the audit log records it, and a supervisor watch finding raised for such a node says that certificate verification is turned off.

Only the supervisor watch probe changed. The connectivity probe made when a node is saved, and the node monitoring and models handlers, still do not check certificates.

## Changed REST contract

Three fields on the compute node object, on `GET /api/compute/nodes`, `GET /api/compute/nodes/{name}`, `POST /api/compute/nodes` and `PUT /api/compute/nodes/{name}`:

| Field | Type | Default | Meaning |
|---|---|---|---|
| `tls_ca_file` | string | absent | Path to a PEM CA bundle. Trimmed of surrounding spaces. |
| `tls_cert_fingerprint` | string | absent | SHA-256 fingerprint, 64 hex digits. On write, upper or lower case, with or without colons or spaces, is accepted; on read it comes back as 64 lowercase digits without separators. |
| `tls_insecure_skip_verify` | boolean | absent (false) | `true` turns certificate checking off for the node. |

The daemon omits an empty or false value rather than sending `null` or `""`, so each key is an **absent key** when it has no value. A client must treat an absent key (and, to be safe, a `null`) as empty or false.

`PUT` replaces the whole node, so an edit must send all three fields every time: an empty string clears a CA file or a pin, and `false` turns verification back on. Leaving a key out has the same effect as clearing it. The PWA sends `""`, `""` and `false` when none is set, and sends `""` for the CA file and the fingerprint when the skip box is ticked.

Write errors are a `400` with a plain-text body (not JSON), from the same node validation on `POST` and `PUT`:

- Invalid fingerprint, wrong length: `compute node: tls_cert_fingerprint must be 64 hex chars (SHA-256), got 12` (the number is the length after colons and spaces are removed).
- Invalid fingerprint, not hexadecimal: `compute node: tls_cert_fingerprint must be hex (SHA-256)`.
- Skip combined with a CA file or a fingerprint: `compute node: tls_insecure_skip_verify cannot be combined with tls_ca_file or tls_cert_fingerprint (choose one: verify, pin or skip)`.

A CA file path that cannot be read is not refused on save; it is reported when the node is next probed.

The same three keys are accepted by the MCP tools `compute_node_add` and `compute_node_update`, the CLI (`--tls-ca-file`, `--tls-cert-fingerprint`, `--tls-insecure-skip-verify`), the comm channel (`tls_ca_file=<path>`, `tls_cert_fingerprint=<hex>`, `tls_insecure_skip_verify=true`) and the `compute_nodes` entries of the YAML config. No other endpoint changes.

## Audit log

A node saved with `tls_insecure_skip_verify` set writes an audit entry with the action `compute_node_tls_verification_off` (details: `resource_type` `compute_node`, `resource_id` the node name, `tls_insecure_skip_verify` true, and `source`: `compute_node_add` or `compute_node_update`). The same action is written when the daemon starts with a configured node that has it (`source` `config_seed`) and the first time the supervisor watch probes such a node (`source` `supervisor_watch_probe`, once per change to the node's TLS settings). `GET /api/audit?action=compute_node_tls_verification_off` lists them. The `compute_node_add` and `compute_node_update` entries also carry three booleans, `tls_insecure_skip_verify`, `tls_ca_file_set` and `tls_cert_fingerprint_set`; the path and the fingerprint themselves are never written. The app does not need to do anything with the audit log; it is context for the badge.

## PWA changes

### Node form

- Under the **Address** field of the add and edit compute node form there is a **Certificate verification** group with a **CA file (PEM, for a private CA)** field (placeholder `/etc/ssl/private-ca.pem`), a **Certificate fingerprint (SHA-256)** field (placeholder `AB:CD:… (64 hex characters)`) and a **Skip certificate verification (not recommended)** checkbox. Each has a help link to `datawatch-definitions.md#compute-node-tls-verification`.
- The three controls exclude each other: while the checkbox is ticked the CA file and fingerprint fields are disabled and a red warning reads "Certificate verification will be OFF for this node. Anyone on the network path can impersonate it and read or alter what is sent, including API keys."; while either text field has a value the checkbox is disabled. This is a convenience; the daemon enforces the rule too.
- The edit form is filled from the node record. The **Test connection** action sends the same three values with its trial node.

### Node row badge

- A node with `tls_insecure_skip_verify` true shows a red badge **⚠ TLS verification OFF** in its row, with a tooltip: "Certificate verification is OFF for this node. Anyone on the network path can impersonate it and read or alter what is sent, including API keys. Use a CA file or a pinned fingerprint instead."
- A node with a fingerprint shows a quiet badge **pinned** (tooltip: the fingerprint); with only a CA file it shows **CA file** (tooltip: the path); a node with none of the three shows no badge. Skip wins over the others if a record ever carried more than one.
- In the expanded node detail the three keys are listed only when they have a value; a false skip setting is not shown.

## New locale keys

| Key | EN |
|---|---|
| `compute_tls_group_title` | Certificate verification |
| `compute_tls_ca_file` | CA file (PEM, for a private CA) |
| `compute_tls_ca_file_ph` | /etc/ssl/private-ca.pem |
| `compute_tls_cert_fingerprint` | Certificate fingerprint (SHA-256) |
| `compute_tls_cert_fingerprint_ph` | AB:CD:… (64 hex characters) |
| `compute_tls_insecure_skip_verify` | Skip certificate verification (not recommended) |
| `compute_tls_insecure_skip_verify_warn` | Certificate verification will be OFF for this node. Anyone on the network path can impersonate it and read or alter what is sent, including API keys. |
| `compute_tls_off_badge` | TLS verification OFF |
| `compute_tls_off_badge_tip` | Certificate verification is OFF for this node. Anyone on the network path can impersonate it and read or alter what is sent, including API keys. Use a CA file or a pinned fingerprint instead. |
| `compute_tls_pinned_badge` | pinned |
| `compute_tls_ca_badge` | CA file |

The PWA bundles already carry DE, ES, FR and JA text for these 11 keys (`internal/server/web/locales/{de,es,fr,ja}.json`); please add the same keys to the app's own string resources in all five languages, taking the English values above and the translations from those bundles.

## Docs links

Add three keys to `DocsLinks.kt`, each pointing at `datawatch-definitions.md#compute-node-tls-verification`, the same page and anchor as in the PWA's `DOCS_LINKS_BY_KEY`:

| Key | Target |
|---|---|
| `compute_tls_ca_file` | `datawatch-definitions.md#compute-node-tls-verification` |
| `compute_tls_cert_fingerprint` | `datawatch-definitions.md#compute-node-tls-verification` |
| `compute_tls_insecure_skip_verify` | `datawatch-definitions.md#compute-node-tls-verification` |

## Where to look in datawatch

- `internal/compute/node_tls.go`: the three modes and the fingerprint rules.
- `internal/compute/node.go`: the node fields and their validation.
- `internal/server/compute.go`: the REST handlers and the audit entries.
- `internal/server/web/app.js`: `computeTLSBadgeHTML`, `computeTLSModeChanged` and the node form.
- `internal/server/web/locales/{en,de,es,fr,ja}.json`: the locale keys above.
- `docs/datawatch-definitions.md` (Compute node TLS verification) and `docs/howto/compute-nodes.md` (Certificates for self-signed and private-CA nodes).

## Acceptance (mobile)

Android and iOS both:

- The compute node add and edit form has a **Certificate verification** group with the CA file field, the fingerprint field and the skip checkbox, each with a help link opened from `compute_tls_ca_file`, `compute_tls_cert_fingerprint` and `compute_tls_insecure_skip_verify` in `DocsLinks.kt`, all three targeting `datawatch-definitions.md#compute-node-tls-verification`.
- The edit form is pre-filled from the node record; a key absent from the record shows an empty field and an unticked box.
- Ticking the checkbox disables the CA file and fingerprint fields and shows the red warning text; a value in either field disables the checkbox. Neither control can produce the combination the daemon refuses.
- Save sends all three fields on every `POST` and `PUT`: the CA file and fingerprint as strings (empty to clear), the skip setting as a boolean. With the box ticked, the CA file and fingerprint are sent empty. An edit that clears a pin really clears it.
- A `400` from save shows the daemon's plain-text message (not a JSON parse error), including the three messages listed above, and keeps the form open.
- A node with `tls_insecure_skip_verify` true shows a red **TLS verification OFF** badge in its list row, with the tooltip (or long-press text) from `compute_tls_off_badge_tip`; a node with a fingerprint shows the quiet **pinned** badge, one with only a CA file shows **CA file**, and a node with none shows no badge.
- The badge, the form group and the warning fit a phone-width screen without horizontal scrolling; long CA file paths and fingerprints wrap or truncate with the full value available.
- The 11 locale keys above exist in all of the app's languages (EN, DE, ES, FR, JA), with the English values listed, and DE, ES, FR and JA take the translations from the PWA bundles.
- A node record that lacks all three keys (an older daemon, or the default) is treated as verified by default and shows no badge and empty fields; the app never writes `null` for a key.

## Filing record

Not filed yet. When the supervisor files it, record the issue number and link here in the form used by `2026-10-30-automation-cleanup-parity.md`.
