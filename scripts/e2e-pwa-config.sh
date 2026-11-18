#!/usr/bin/env bash
# e2e-pwa-config.sh — sourceable helper for generating the isolated e2e
# daemon's config from the repo template.
#
# Nothing executes when this file is sourced; it only defines
# e2e_pwa_write_config for callers (see scripts/e2e-pwa.sh).
#
# e2e_pwa_write_config TMPL OUT RUN_DIR HTTP_PORT TLS_PORT SSE_PORT TOKEN
#
#   TMPL      config template to transform (testdata/datawatch.yaml)
#   OUT       generated config path (e.g. $RUN_DIR/config.yaml)
#   RUN_DIR   per-run temp dir; its PARENT becomes the single
#             allowed_project_roots entry, so the story fixtures that
#             seed sessions under the temp folder pass the project-root
#             guard
#   TOKEN     API token substituted for the empty server token

e2e_pwa_write_config() {
  local TMPL="$1" OUT="$2" RUN_DIR="$3" HTTP_PORT="$4" TLS_PORT="$5" SSE_PORT="$6" TOKEN="$7"
  sed \
    -e "s|data_dir: /data|data_dir: $RUN_DIR/data|g" \
    -e "s|default_project_dir: /data/projects|default_project_dir: $RUN_DIR/data/projects|g" \
    -e "s|port: 8080|port: $HTTP_PORT|g" \
    -e "s|tls_port: 8443|tls_port: $TLS_PORT|g" \
    -e "s|sse_port: 9090|sse_port: $SSE_PORT|g" \
    -e "s|host: 0\.0\.0\.0|host: 127.0.0.1|g" \
    -e "s|token: \"\"|token: \"${TOKEN}\"|g" \
    -e "s|token: \"WEBHOOK_NOAUTH\"|token: \"\"|g" \
    -e "s|^server:\$|server:\\n  allowed_project_roots: [\"$(dirname "$RUN_DIR")\"]|" \
    "$TMPL" > "$OUT"
}
