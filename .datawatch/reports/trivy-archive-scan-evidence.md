# Trivy archive-scan evidence (run 2026-11-16 00:12 UTC-5)

Proves `scripts/trivy-scan-image.sh` **fails on a real HIGH/CRITICAL finding and passes on a clean image**, and records real runs of the `docker-agent-base` gate step. All commands were executed in this worktree on 2026-11-16 ~00:10–00:12 UTC-5; outputs quoted or summarized below.

## Environment

- Docker 29.1.3 (build 29.1.3-0ubuntu4.1), buildx "default" instance (docker driver)
- trivy 0.74.0 (`trivy` on PATH)

## Fixtures (new files under `tests/fixtures/trivy-scan/`)

- `Dockerfile.clean` — `FROM scratch` + `COPY clean.txt /clean.txt`. One layer, no packages.
- `Dockerfile.vulnerable` — `FROM scratch` + a PyYAML 5.3.1 `dist-info/METADATA` copied to `/usr/lib/python3.8/site-packages/PyYAML-5.3.1.dist-info/METADATA`. trivy's image scanner reads this from the filesystem (python-pkg target) and reports the pinned version.
- `METADATA`, `clean.txt` — the fixture payloads (PyYAML 5.3.1 pins **CVE-2020-14343, CRITICAL**, fixed in 5.4).
- `empty.trivyignore` — empty ignore file, passed via `TRIVY_IGNOREFILE` so the repo's `.trivyignore` cannot mask the finding.
- `run-evidence.sh` (executable) — requires `docker` and `trivy` via `command -v` (else prints `SKIP` and exits 0); builds `datawatch-trivy-fixture:clean` and `:vulnerable` (FROM scratch, no pulls); runs `scripts/trivy-scan-image.sh` on each with `TRIVY_IGNOREFILE=<fixture>/empty.trivyignore`; deletes both images via `trap ... EXIT INT TERM`; prints both exit codes; exits 0 only if clean=0 and vulnerable=1.

## Command 1 — fixture evidence

```
bash tests/fixtures/trivy-scan/run-evidence.sh
```

Result: **PASS, exit 0** (~1 s). After exit, `docker images` lists no `datawatch-trivy-fixture` image (trap cleanup).

### Clean image: `datawatch-trivy-fixture:clean`

trivy scanned, `Number of language-specific files num=0`, report summary shows all columns `-` (no findings).

```
Report Summary
│ Target │ Type │ Vulnerabilities │ Secrets │
│   -    │  -   │        -        │    -    │
```

**Exit code: 0** (trivy clean, `--exit-code 1` not tripped).

### Vulnerable image: `datawatch-trivy-fixture:vulnerable`

```
┌─────────────────────────────────────────────────────────────────────────────────────────┬────────────┬─────────────────┬─────────┐
│                                  Target                                                 │    Type    │ Vulnerabilities │ Secrets │
│ usr/lib/python3.8/site-packages/PyYAML-5.3.1.dist-info/METADATA                         │ python-pkg │        1        │    -    │
└─────────────────────────────────────────────────────────────────────────────────────────┴────────────┴─────────────────┴─────────┘

Python (python-pkg)
Total: 1 (HIGH: 0, CRITICAL: 1)
│ PyYAML (METADATA) │ CVE-2020-14343 │ CRITICAL │ fixed │ 5.3.1 │ 5.4 │ PyYAML: incomplete fix for CVE-2020-1747 │
```

**Exit code: 1** — the helper correctly fails on a real CRITICAL finding.

## Command 2 — the real gate

```
PRE_TAG_ONLY=docker-agent-base PRE_TAG_RECORD_FILE=.datawatch/reports/evidence-pre-tag-record bash scripts/pre-tag.sh
```

The `docker-agent-base` gate step self-skips when no `Dockerfile*`, containers workflow or accepted-risks file differs from the last tag (`datawatch-pre-c241`) — which is true here, since this task deliberately does not touch any of them. Two runs were recorded:

### 2a. Honest run (no Dockerfile change on the worktree)

```
[docker-agent-base] running
SKIP [docker-agent-base]: no Dockerfile, containers workflow or accepted-risks change since datawatch-pre-c241
pre-tag: all steps passed
```

**Exit code: 0.** This is the gate's correct behaviour for this branch state; it does not exercise the scan.

### 2b. Forced-scan run (change-detection satisfied transiently, disclosed)

To make the gate *execute* the build + archive scan, one comment line was appended to the tracked `docker/dockerfiles/Dockerfile.agent-base` so the step's `git diff --name-only <tag>` check saw a change, and the file was restored to its exact original content immediately afterwards (`git checkout --`; `git status` is clean for that path). Nothing in the committed tree changed.

```
[docker-agent-base] running
[docker-agent-base] changed since datawatch-pre-c241:
docker/dockerfiles/Dockerfile.agent-base
...
#36 naming to docker.io/library/datawatch-agent-base:pre-tag done
2026-11-16T00:12:03-05:00  FATAL  Fatal error  run error: image scan error: scan error: scan failed:
  failed analysis: analyze error: pipeline error: failed to analyze layer
  (sha256:2328a9cb555ddf1bcf1fd05c12f4855268f4a3eb65e5d3bc9d2a37b9d16caad9):
  walk error: failed to extract the archive: archive/tar: invalid tar header
[docker-agent-base] FAIL (8s)
```

**Exit code: 1.** The build succeeded entirely from local BuildKit cache (no network), and the gate invoked `scripts/trivy-scan-image.sh datawatch-agent-base:pre-tag` — i.e. the archive-scan path inside the gate ran — but trivy 0.74 failed with a FATAL error on this host.

### Root-cause analysis of the 2b trivy FATAL (environment issue, not a fixture/helper defect)

- `docker save` on Docker 29.1.3 writes an OCI-layout archive whose manifest's first layer has mediaType `application/vnd.docker.image.rootfs.diff.tar` (uncompressed) while the remaining layers are `…diff.tar.gzip` (mixed mediaTypes).
- The failing layer diffID `sha256:2328a9cb…` corresponds to stored blob `sha256:2189c3f5…` (15,252,871 bytes). That blob **is a valid gzip tar**: I extracted the archive, decompressed it with Python and verified its sha256 equals `2328a9cb…` and that it contains a well-formed tar. So the saved image content is intact; the extraction failure is inside trivy 0.74's archive-layer handling.
- The same FATAL was reproduced independently of the gate: `bash scripts/trivy-scan-image.sh datawatch-agent-base:noprov` (a `docker build --provenance=false` rebuild) fails on the identical layer 2328a9cb.
- Contrast: the fixture images in command 1 (single uncompressed-tar layer archives) scan cleanly in both directions on the same trivy binary, which is exactly the pass/fail contract this task had to prove.
- Conclusion: on this host (Docker 29.1.3 + trivy 0.74.0), the gate's trivy step cannot parse the OCI archive that `docker save` now produces for multi-layer buildkit images. This is the same class of trivy/daemon image-analysis issue that motivated the archive helper; a follow-up (trivy version bump or `docker save` format control) is needed for the gate step to pass here. It is outside this task's scope, disclosed per the spec ("run … with its result").

## Deliverables

- `tests/fixtures/trivy-scan/{Dockerfile.clean,Dockerfile.vulnerable,METADATA,clean.txt,empty.trivyignore,run-evidence.sh}`
- This report. `AGENT.md`/`CLAUDE.md` were not touched (git status for `AGENT.md` matches the task base).
- Auxiliary logs kept in `.datawatch/tmp/{run-evidence.log,gate-honest.log,gate-forced.log}` (gitignored scratch).
