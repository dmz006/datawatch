# upstream_watch.py live smoke — 2026-11-01

Command (from repo root):
    python3 scripts/upstream_watch.py --report-md .upstream-watch-smoke/report.md --report-json .upstream-watch-smoke/report.json

- Exit status: 0 (first attempt exited 2 only because the scratch parent dir did
  not exist; after `mkdir -p .upstream-watch-smoke` the run succeeded in ~19s,
  unauthenticated, real network)
- report.md starts with `<!-- upstream-watch -->`
- report.json parses; has findings_count=83 (== len(findings)), findings, standing, unmapped, not_checked (also checked_pins, last_checked, marker, report_sha256_prefix)
- Standing notes: Trivy installer fetched from the moving `main` branch in 3
  workflow files / 8 lines: .github/workflows/containers.yaml (L93, L170),
  .github/workflows/image-refresh.yaml (L86, L164, L235, L563),
  .github/workflows/release.yaml (L280, L374) — matched against grep of the actual files
- Version ARG coverage: 43 unique `ARG <X>_VERSION=` in docker/dockerfiles;
  9 under Unmapped pins, cargo-audit under Not checked (OSV 400 + crates.io no
  stable version), remaining 33 checked (34 pins had findings; 83 = 46 advisory + 37 stale-pin), counts in header line: 37 checked, 10 unmapped, 2 not checked
- No tracked file modified: git status --porcelain showed only `?? .upstream-watch-smoke/`
- .upstream-watch-smoke deleted afterwards

## Full report.md
<!-- upstream-watch -->

**upstream-watch**: 83 finding(s) (46 advisorie(s), 37 stale pin(s)) across 37 checked pin(s); 10 unmapped; 2 not checked.

## Findings

### Advisories (OSV, advisory-only — these pins may outdate even when no newer version exists)

- **GHSA-4vp2-6q8c-pvq2** on @anthropic-ai/claude-code 2.1.114 (`CLAUDE_CODE_VERSION` in docker/dockerfiles/Dockerfile.agent-claude): @anthropic-ai/claude-code has an Insecure Temporary File in /copy Command that Enables Response Disclosure and Symlink-Based File Write
- **GHSA-7835-87q9-rgvv** on @anthropic-ai/claude-code 2.1.114 (`CLAUDE_CODE_VERSION` in docker/dockerfiles/Dockerfile.agent-claude): Claude Code: Sandbox Escape via Git Worktree Path Confusion Allows Unsandboxed Code Execution
- **GHSA-fg94-h982-f3mm** on @anthropic-ai/claude-code 2.1.114 (`CLAUDE_CODE_VERSION` in docker/dockerfiles/Dockerfile.agent-claude): Claude Code: Out-of-Band Data Exfiltration via Pre-Approved HuggingFace Domain in WebFetch
- **GHSA-pq7j-f95f-qfcv** on @anthropic-ai/claude-code 2.1.114 (`CLAUDE_CODE_VERSION` in docker/dockerfiles/Dockerfile.agent-claude): Claude Code: Plugin Hash Pin Bypass Allows Bypass of Hash Pinning
- **GHSA-7w7m-v5vp-w699** on aider-chat 0.86.0 (`AIDER_VERSION` in docker/dockerfiles/Dockerfile.agent-aider): Aider is vulnerable to Code Injection via editor_coder.run function
- **GHSA-hchg-qm84-cj9p** on aider-chat 0.86.0 (`AIDER_VERSION` in docker/dockerfiles/Dockerfile.agent-aider): Aider has an SSRF vulnerability through its AWS EC2 Metadata Endpoint
- **PYSEC-2026-2335** on aider-chat 0.86.0 (`AIDER_VERSION` in docker/dockerfiles/Dockerfile.agent-aider): Aider is vulnerable to Code Injection via editor_coder.run function
- **PYSEC-2026-2336** on aider-chat 0.86.0 (`AIDER_VERSION` in docker/dockerfiles/Dockerfile.agent-aider): Aider has an SSRF vulnerability through its AWS EC2 Metadata Endpoint
- **GO-2026-6860** on golang.org/x/net 0.60.0 (`GH_XNET_VERSION` in docker/dockerfiles/Dockerfile.agent-base): Avoid quadratic complexity when coalescing text nodes in html in golang.org/x/net
- **GO-2026-6866** on golang.org/x/net 0.60.0 (`GH_XNET_VERSION` in docker/dockerfiles/Dockerfile.agent-base): Check all ancestor namespace transitions when rendering raw text in html in golang.org/x/net
- **GHSA-3qhv-2rgh-x77r** on pnpm 10.33.0 (`PNPM_VERSION` in docker/dockerfiles/Dockerfile.lang-node): pnpm: Repository config can expand victim environment secrets into registry requests before scripts run
- **GHSA-4gxm-v5v7-fqc4** on pnpm 10.33.0 (`PNPM_VERSION` in docker/dockerfiles/Dockerfile.lang-node): pnpm: Reserved bin name deletes PNPM_HOME during global remove
- **GHSA-54hh-g5mx-jqcp** on pnpm 10.33.0 (`PNPM_VERSION` in docker/dockerfiles/Dockerfile.lang-node): pnpm: Unsafe default behavior breaks integrity check
- **GHSA-5wx6-mg75-v57r** on pnpm 10.33.0 (`PNPM_VERSION` in docker/dockerfiles/Dockerfile.lang-node): pnpm: Manifest identity spoof satisfies allowBuilds and runs attacker lifecycle
- **GHSA-72r4-9c5j-mj57** on pnpm 10.33.0 (`PNPM_VERSION` in docker/dockerfiles/Dockerfile.lang-node): pnpm: `patch-remove` could delete project-selected files outside the patches directory
- **GHSA-c59q-g84q-2gj5** on pnpm 10.33.0 (`PNPM_VERSION` in docker/dockerfiles/Dockerfile.lang-node): pnpm: Virtual store linker path traversal via unvalidated depPath name in lockfileToDepGraph
- **GHSA-cjhr-43r9-cfmw** on pnpm 10.33.0 (`PNPM_VERSION` in docker/dockerfiles/Dockerfile.lang-node): pnpm binds unscoped user-level npm auth credentials to a repository-selected registry
- **GHSA-fr4h-3cph-29xv** on pnpm 10.33.0 (`PNPM_VERSION` in docker/dockerfiles/Dockerfile.lang-node): pnpm: Hoisted install imports lockfile alias outside node_modules
- **GHSA-gj8w-mvpf-x27x** on pnpm 10.33.0 (`PNPM_VERSION` in docker/dockerfiles/Dockerfile.lang-node): pnpm: Repository-controlled configDependencies can select a pacquet native install engine
- **GHSA-hg3w-7f8c-63hp** on pnpm 10.33.0 (`PNPM_VERSION` in docker/dockerfiles/Dockerfile.lang-node): pnpm: Tarball hash of GitHub git dependencies is not stored in lockfile
- **GHSA-hwx4-2j3j-g496** on pnpm 10.33.0 (`PNPM_VERSION` in docker/dockerfiles/Dockerfile.lang-node): pnpm: Transitive dependency alias path traversal allows project path override via symlink replacement
- **GHSA-p4xf-rf54-rj3x** on pnpm 10.33.0 (`PNPM_VERSION` in docker/dockerfiles/Dockerfile.lang-node): pnpm: Git Fetch Argument Injection via Lockfile resolution.commit
- **GHSA-q6j5-fjx5-2mc3** on pnpm 10.33.0 (`PNPM_VERSION` in docker/dockerfiles/Dockerfile.lang-node): pnpm Has an Integrity Check Bypass via Missing Lockfile Integrity Field
- **GHSA-qrv3-253h-g69c** on pnpm 10.33.0 (`PNPM_VERSION` in docker/dockerfiles/Dockerfile.lang-node): pnpm: Path traversal in configDependencies env lockfile allows symlink creation outside node_modules/.pnpm-config
- **GHSA-rxhj-4m44-96r4** on pnpm 10.33.0 (`PNPM_VERSION` in docker/dockerfiles/Dockerfile.lang-node): pnpm Vulnerable to Arbitrary File Write/Delete via Malicious Patch File (Path Traversal)
- **GHSA-vq4v-j7r6-jq4m** on pnpm 10.33.0 (`PNPM_VERSION` in docker/dockerfiles/Dockerfile.lang-node): pnpm: A tarball dependency's manifest `name` escapes node_modules → arbitrary file write/overwrite on install
- **GHSA-vx52-2968-3vc6** on pnpm 10.33.0 (`PNPM_VERSION` in docker/dockerfiles/Dockerfile.lang-node): pnpm: Environment secrets exfiltrated via env-placeholder expansion in proxy settings read from an untrusted pnpm-workspace.yaml
- **GHSA-w466-c33r-3gjp** on pnpm 10.33.0 (`PNPM_VERSION` in docker/dockerfiles/Dockerfile.lang-node): pnpm: Project env lockfile can short-circuit package-manager resolution and execute lockfile-selected pnpm bytes
- **GHSA-2599-h6xx-hpxp** on poetry 1.8.5 (`POETRY_VERSION` in docker/dockerfiles/Dockerfile.lang-python): Poetry Has Wheel Path Traversal Which Can Lead to Arbitrary File Write
- **GHSA-73h3-mf4w-8647** on poetry 1.8.5 (`POETRY_VERSION` in docker/dockerfiles/Dockerfile.lang-python): Poetry has Path Traversal in tar extraction on Python 3.10.0 - 3.10.12 and 3.11.0 - 3.11.4
- **PYSEC-2026-2260** on poetry 1.8.5 (`POETRY_VERSION` in docker/dockerfiles/Dockerfile.lang-python): Poetry is a dependency manager for Python. From version 1.4.0 to before version 2.3.3, a crafted wheel can contain ../ paths that Poetry writes to disk without containment checks, allowing arbitrary file write with the privileges of the Poetry process. It is reachable from untrusted package artifacts during normal install flows. (Normally, installing a malicious wheel is not sufficient for execution of malicious code. Malicious code will only be executed after installation if the malicious packa
- **PYSEC-2026-2890** on poetry 1.8.5 (`POETRY_VERSION` in docker/dockerfiles/Dockerfile.lang-python): Poetry has Path Traversal in tar extraction on Python 3.10.0 - 3.10.12 and 3.11.0 - 3.11.4
- **GO-2026-6599** on stdlib 1.26.6 (`GO_VERSION` in Dockerfile.dev): Reset context tracking on consecutive template expressions in html/template
- **GO-2026-6600** on stdlib 1.26.6 (`GO_VERSION` in Dockerfile.dev): Recognize yield as regexp preceder keyword in html/template
- **GO-2026-6603** on stdlib 1.26.6 (`GO_VERSION` in Dockerfile.dev): HTTP/2 server memory exhaustion due to Trailer headers in net/http
- **GO-2026-6604** on stdlib 1.26.6 (`GO_VERSION` in Dockerfile.dev): Root.Mkdir(All) can follow junctions out of the root on Windows in os
- **GO-2026-6605** on stdlib 1.26.6 (`GO_VERSION` in Dockerfile.dev): HTTP/1 client connection desynchronization after CONNECT rejection in net/http
- **GO-2026-6607** on stdlib 1.26.6 (`GO_VERSION` in Dockerfile.dev): Reject malformed ECH outer extension references in crypto/tls
- **GO-2026-6608** on stdlib 1.26.6 (`GO_VERSION` in Dockerfile.dev): Memory limit bypass when parsing MIME headers in net/textproto, mime/multipart
- **GO-2026-6609** on stdlib 1.26.6 (`GO_VERSION` in Dockerfile.dev): Lack of limit on size of parsed Range headers in net/http
- **GO-2026-6610** on stdlib 1.26.6 (`GO_VERSION` in Dockerfile.dev): HTTP/2 transport accepts malformed framing-related headers in net/http
- **GO-2026-6611** on stdlib 1.26.6 (`GO_VERSION` in Dockerfile.dev): Excessive CPU consumption from repeated initial window changes in net/http
- **GO-2026-6612** on stdlib 1.26.6 (`GO_VERSION` in Dockerfile.dev): Double flow control refund on HTTP/2 server streams in net/http
- **GO-2026-6613** on stdlib 1.26.6 (`GO_VERSION` in Dockerfile.dev): HTTP/1 server connection desynchronization after 2xx CONNECT response in net/http
- **GO-2026-6617** on stdlib 1.26.6 (`GO_VERSION` in Dockerfile.dev): HTTP/2 server crash due to HPACK encoder race in net/http
- **GO-2026-6809** on stdlib 1.26.6 (`GO_VERSION` in Dockerfile.dev): Distrust compromised intermediate certificates in crypto/x509

### Stale pins (newer upstream version available)

- **patch**: Claude Code 2.1.114 -> 2.1.316 — edit `CLAUDE_CODE_VERSION` in `docker/dockerfiles/Dockerfile.agent-claude` (upstream: https://www.npmjs.com/package/@anthropic-ai/claude-code)
- **patch**: Pyright 1.1.404 -> 1.1.414 — edit `PYRIGHT_VERSION` in `docker/dockerfiles/Dockerfile.lang-python` (upstream: https://pypi.org/pypi/pyright/json)
- **patch**: aider-chat 0.86.0 -> 0.86.2 — edit `AIDER_VERSION` in `docker/dockerfiles/Dockerfile.agent-aider` (upstream: https://pypi.org/pypi/aider-chat/json)
- **patch**: bundler-audit 0.9.2 -> 0.9.3 — edit `BUNDLER_AUDIT_VERSION` in `docker/dockerfiles/Dockerfile.lang-ruby` (upstream: https://rubygems.org/api/v1/gems/bundler-audit.json)
- **patch**: ruby-lsp 0.26.9 -> 0.26.11 — edit `RUBY_LSP_VERSION` in `docker/dockerfiles/Dockerfile.lang-ruby` (upstream: https://rubygems.org/api/v1/gems/ruby-lsp.json)
- **patch**: signal-cli 0.14.2 -> v0.14.9 — edit `SIGNAL_CLI_VERSION` in `docker/dockerfiles/Dockerfile.parent-full` (upstream: https://api.github.com/repos/AsamK/signal-cli/releases/latest)
- **minor**: Bandit 1.8.3 -> 1.9.4 — edit `BANDIT_VERSION` in `docker/dockerfiles/Dockerfile.lang-python` (upstream: https://pypi.org/pypi/bandit/json)
- **minor**: Gemini CLI 0.62.0 -> 0.67.0-preview.0 — edit `GEMINI_CLI_VERSION` in `docker/dockerfiles/Dockerfile.agent-gemini` (upstream: https://www.npmjs.com/package/@google/gemini-cli)
- **minor**: GitHub CLI (cli/cli) 2.102.0 -> v2.103.3 — edit `GH_VERSION` in `docker/dockerfiles/Dockerfile.agent-base` (upstream: https://api.github.com/repos/cli/cli/releases/latest)
  - coupled edit: GH_COMMIT in the same Dockerfile must move to the new tag's commit (the build verifies the clone HEAD equals it)
- **minor**: Go toolchain 1.26.10 -> go1.27.3 — **human-only** — edit `GO_VERSION` in `docker/dockerfiles/Dockerfile.agent-base` (upstream: https://go.dev/dl/?mode=json)
  - coupled edit: Go versions have a floor in scripts/check-go-toolchain-pins.sh and the pin must keep scripts/check-dockerfile-go-versions.sh passing
- **minor**: Go toolchain 1.26.10 -> go1.27.3 — **human-only** — edit `GO_VERSION` in `docker/dockerfiles/Dockerfile.lang-go` (upstream: https://go.dev/dl/?mode=json)
  - coupled edit: Go versions have a floor in scripts/check-go-toolchain-pins.sh and the pin must keep scripts/check-dockerfile-go-versions.sh passing
- **minor**: Go toolchain 1.26.10 -> go1.27.3 — **human-only** — edit `GO_VERSION` in `docker/dockerfiles/Dockerfile.stats-cluster` (upstream: https://go.dev/dl/?mode=json)
  - coupled edit: Go versions have a floor in scripts/check-go-toolchain-pins.sh and the pin must keep scripts/check-dockerfile-go-versions.sh passing
- **minor**: Go toolchain 1.26.10 -> go1.27.3 — **human-only** — edit `GO_VERSION` in `docker/dockerfiles/Dockerfile.validator` (upstream: https://go.dev/dl/?mode=json)
  - coupled edit: Go versions have a floor in scripts/check-go-toolchain-pins.sh and the pin must keep scripts/check-dockerfile-go-versions.sh passing
- **minor**: Go toolchain 1.26.6 -> go1.27.3 — **human-only** — edit `GO_VERSION` in `Dockerfile.dev` (upstream: https://go.dev/dl/?mode=json)
  - coupled edit: Go versions have a floor in scripts/check-go-toolchain-pins.sh and the pin must keep scripts/check-dockerfile-go-versions.sh passing
- **minor**: Kotlin compiler 2.1.10 -> v2.4.21 — edit `KOTLIN_VERSION` in `docker/dockerfiles/Dockerfile.lang-kotlin` (upstream: https://api.github.com/repos/JetBrains/kotlin/releases/latest)
- **minor**: RuboCop 1.86.1 -> 1.92.2 — edit `RUBOCOP_VERSION` in `docker/dockerfiles/Dockerfile.lang-ruby` (upstream: https://rubygems.org/api/v1/gems/rubocop.json)
- **minor**: Ruff 0.8.4 -> 0.17.3 — edit `RUFF_VERSION` in `docker/dockerfiles/Dockerfile.lang-python` (upstream: https://pypi.org/pypi/ruff/json)
- **minor**: Trivy v0.73.0 -> v0.76.1 — **human-only** — edit `TRIVY_VERSION` in `.github/workflows/image-refresh.yaml` (upstream: https://api.github.com/repos/aquasecurity/trivy/releases/latest)
- **minor**: bun 1.3.6 -> bun-v1.4.4 — edit `BUN_VERSION` in `docker/dockerfiles/Dockerfile.lang-node` (upstream: https://api.github.com/repos/oven-sh/bun/releases/latest)
  - coupled edit: installed via the bun.sh installer script (moving endpoint)
- **minor**: delve v1.26.1 -> v1.27.2 — edit `DELVE_VERSION` in `docker/dockerfiles/Dockerfile.lang-go` (upstream: https://api.github.com/repos/go-delve/delve/releases/latest)
- **minor**: golangci-lint v2.6.1 -> v2.15.0 — edit `GOLANGCI_LINT_VERSION` in `docker/dockerfiles/Dockerfile.lang-go` (upstream: https://api.github.com/repos/golangci/golangci-lint/releases/latest)
- **minor**: goose 1.43.0 -> v1.57.0 — edit `GOOSE_VERSION` in `docker/dockerfiles/Dockerfile.agent-goose` (upstream: https://api.github.com/repos/aaif-goose/goose/releases/latest)
- **minor**: kubectl v1.34.1 -> v1.37.2 — edit `KUBECTL_VERSION` in `docker/dockerfiles/Dockerfile.tools-ops` (upstream: https://dl.k8s.io/release/stable.txt)
- **minor**: opencode 1.4.11 -> 1.19.5 — edit `OPENCODE_VERSION` in `docker/dockerfiles/Dockerfile.agent-opencode` (upstream: https://www.npmjs.com/package/opencode-linux-x64)
- **minor**: pip-audit 2.8.0 -> 2.10.1 — edit `PIP_AUDIT_VERSION` in `docker/dockerfiles/Dockerfile.lang-python` (upstream: https://pypi.org/pypi/pip-audit/json)
- **minor**: tsx 4.20.6 -> 4.23.15 — edit `TSX_VERSION` in `docker/dockerfiles/Dockerfile.lang-node` (upstream: https://www.npmjs.com/package/tsx)
- **minor**: uv 0.5.18 -> 0.13.6 — edit `UV_VERSION` in `docker/dockerfiles/Dockerfile.lang-python` (upstream: https://api.github.com/repos/astral-sh/uv/releases/latest)
  - coupled edit: installed via the astral.sh versioned installer
- **minor**: yq v4.45.1 -> v4.54.1 — edit `YQ_VERSION` in `docker/dockerfiles/Dockerfile.tools-ops` (upstream: https://api.github.com/repos/mikefarah/yq/releases/latest)
- **major**: Brakeman 7.0.1 -> 8.1.0 — **human-only** — edit `BRAKEMAN_VERSION` in `docker/dockerfiles/Dockerfile.lang-ruby` (upstream: https://rubygems.org/api/v1/gems/brakeman.json)
- **major**: Bundler 2.5.23 -> 4.0.23 — **human-only** — edit `BUNDLER_VERSION` in `docker/dockerfiles/Dockerfile.lang-ruby` (upstream: https://rubygems.org/api/v1/gems/bundler.json)
- **major**: ESLint 9.39.0 -> 10.14.0 — **human-only** — edit `ESLINT_VERSION` in `docker/dockerfiles/Dockerfile.lang-node` (upstream: https://www.npmjs.com/package/eslint)
- **major**: Poetry 1.8.5 -> 2.5.1 — **human-only** — edit `POETRY_VERSION` in `docker/dockerfiles/Dockerfile.lang-python` (upstream: https://pypi.org/pypi/poetry/json)
- **major**: TypeScript 6.0.3 -> 7.1.0-dev.20260708.3 — **human-only** — edit `TYPESCRIPT_VERSION` in `docker/dockerfiles/Dockerfile.lang-node` (upstream: https://www.npmjs.com/package/typescript)
- **major**: eslint-plugin-security 3.0.1 -> 4.2.1 — **human-only** — edit `ESLINT_SECURITY_VERSION` in `docker/dockerfiles/Dockerfile.lang-node` (upstream: https://www.npmjs.com/package/eslint-plugin-security)
- **major**: helm v3.16.4 -> v4.3.1 — **human-only** — edit `HELM_VERSION` in `docker/dockerfiles/Dockerfile.tools-ops` (upstream: https://api.github.com/repos/helm/helm/releases/latest)
- **major**: pnpm 10.33.0 -> 12.17.0 — **human-only** — edit `PNPM_VERSION` in `docker/dockerfiles/Dockerfile.lang-node` (upstream: https://www.npmjs.com/package/pnpm)
- **major**: rtk 0.37.0 -> v1.1.0 — **human-only** — edit `RTK_VERSION` in `docker/dockerfiles/Dockerfile.agent-base` (upstream: https://api.github.com/repos/rtk-ai/rtk/releases/latest)

## Standing notes

- Trivy installer fetched from the moving `main` branch in 3 workflow file(s), 8 line(s) total: .github/workflows/containers.yaml (2 lines, L93-170), .github/workflows/image-refresh.yaml (4 lines, L86-563), .github/workflows/release.yaml (2 lines, L280-374). Monitoring only — not edited by this script.

## Unmapped pins (no upstream feed in scope — reported, never silent)

- `GH_XNET_VERSION`=0.60.0 in docker/dockerfiles/Dockerfile.agent-base: Go module (golang.org/x/net) forced into the gh build's module graph — advisory-only, no release feed in scope
- `NODE_MAJOR`=22 in docker/dockerfiles/Dockerfile.agent-gemini, docker/dockerfiles/Dockerfile.lang-node: Node.js major from deb.nodesource.com; not a release pin
- `GOPLS_VERSION`=v0.20.0 in docker/dockerfiles/Dockerfile.lang-go: installed via `go install` from the Go module proxy (golang.org/x/tools) — advisory-only, no release feed in scope
- `GOVULNCHECK_VERSION`=v1.1.4 in docker/dockerfiles/Dockerfile.lang-go: installed via `go install` from the Go module proxy (golang.org/x/vuln) — advisory-only, no release feed in scope
- `JDK_VERSION`=17 in docker/dockerfiles/Dockerfile.lang-kotlin, docker/dockerfiles/Dockerfile.parent-full: apt openjdk-${JDK_VERSION} package; not a release pin
- `ANDROID_CMDLINE_TOOLS_VERSION`=11076708 in docker/dockerfiles/Dockerfile.lang-kotlin: dl.google.com commandlinetools archive; no version feed in scope
- `GRADLE_VERSION`=8.11.1 in docker/dockerfiles/Dockerfile.lang-kotlin: services.gradle.org binary download; no JSON version feed in scope
- `RUBY_PKG_VERSION`=3.1 in docker/dockerfiles/Dockerfile.lang-ruby: ruby image variant tag; not a tool release pin
- `RUST_VERSION`=1.85.0 in docker/dockerfiles/Dockerfile.lang-rust: rustup toolchain (static.rust-lang.org dist server); no version feed in scope
- `TERRAFORM_VERSION`=1.10.5 in docker/dockerfiles/Dockerfile.tools-ops: releases.hashicorp.com binary download; no version feed in scope

## Not checked (feed failures / unnameable packages)

- `https://api.osv.dev/v1/query` — advisory check for cargo-audit (0.21.0): HTTPError: HTTP Error 400: Bad Request
- `https://crates.io/api/v1/crates/cargo-audit` — no stable version in response

## Last checked

2026-11-02T02:49:13Z
