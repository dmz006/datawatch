# fix(parity): model pulls always verify the node certificate; node form wording changed (datawatch v9.182.2)

Parent release: datawatch v9.182.2

## What changed

A fix for eleven open high and critical code scanning alerts. Two parts reach the apps.

1. Model pulls to a compute node now always verify the node's certificate (system roots, plus the node's CA file or pinned fingerprint when set). The per-node "Verify TLS for model pulls" setting is kept for compatibility but no longer turns verification off, and "skip certificate verification" now applies only to node probes, not to pulls. To pull from a node whose certificate the system does not trust, set the node's CA file or fingerprint. A plain http node is unaffected.
2. The web app now escapes numeric field values and the node name in the compute node and LLM edit forms, and strips tags until none remain in one title helper. Visible output does not change.

## Settings to add to the mobile app

None. No new setting. The existing node fields (verify_tls, tls_insecure_skip_verify, tls_ca_file, tls_cert_fingerprint) keep their names and types.

## REST contract

None added.

## Changed locale strings (English; the same three keys changed in the de, es, fr and ja files)

- compute_tls_insecure_skip_verify_warn: "Certificate verification will be OFF for the probes of this node (model pulls still verify it). Anyone on the network path can impersonate it and read or alter what is sent, including API keys."
- compute_verify_tls: "Verify TLS for model pulls (always on; kept for compatibility)"
- compute_verify_tls_help: "Kept for configuration compatibility. Model pulls to this node always verify its certificate against the system roots, plus the node's tls_ca_file or tls_cert_fingerprint when set."

## Screens

- Compute node add and edit form: the verify-TLS label and help text, and the skip-verification warning text.

## For the mobile app

Update the three strings above where the app shows the node TLS fields. If the app edits a node that has a certificate the system does not trust, tell the operator to set the CA file or the fingerprint instead of turning verification off for pulls. Check that numeric and name fields in the node and LLM forms are not placed into markup or script without escaping.
