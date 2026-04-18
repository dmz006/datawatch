# Composition examples — agent + (lang OR tools)

The F10 image taxonomy gives you small, single-purpose images you wire
together at deploy time. **One agent per image. One language or tools
bundle per image. Compose them in a Pod (or docker-compose service group)
sharing a `/workspace` volume.**

This doc shows concrete pairings for both **coding** and **non-coding**
work. Same model either way — only the second container changes.

---

## The model in one picture

```
┌────────────────────────────────────────────────────────────┐
│                       Pod / compose                         │
│ ┌─────────────────────┐      ┌──────────────────────────┐ │
│ │   agent-claude       │◄────►│   lang-go     OR         │ │
│ │   (or opencode,      │      │   lang-kotlin OR         │ │
│ │    gemini, aider)    │      │   lang-ruby   OR         │ │
│ │                      │      │   tools-ops   OR         │ │
│ │   talks to the user, │      │   tools-data  OR         │ │
│ │   reads/writes       │      │   (whatever fits)        │ │
│ │   /workspace         │      │                          │ │
│ └──────────┬───────────┘      └────────────┬─────────────┘ │
│            │                               │                │
│            └─────── shared /workspace ─────┘                │
└────────────────────────────────────────────────────────────┘
```

The agent sees:
- `/usr/local/bin/claude` (or opencode/gemini/aider) — the LLM client
- `/workspace/...` — files written by the language/tools container
- `/workspace/...` — its own outputs visible to the language/tools container

The other container provides toolchains the agent shells out to. It runs
nothing of its own; it's a "tools-on-the-PATH" sidecar reachable via
shared volume + the agent's `kubectl exec` through datawatch's session
proxy (`agent` is the entrypoint container; lang/tools is a co-located
service).

For local docker dev, replace "Pod" with `docker-compose` and "shared
volume" with a named volume mounted at `/workspace` in both services.

---

## Coding examples

### Python service work

```yaml
# kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: dw-python-claude
spec:
  containers:
  - name: agent
    image: harbor.dmzs.com/datawatch/agent-claude:v2.4.5
    volumeMounts: [{name: workspace, mountPath: /workspace}]
  - name: lang
    image: harbor.dmzs.com/datawatch/lang-python:v2.4.5
    command: ["sleep", "infinity"]
    volumeMounts: [{name: workspace, mountPath: /workspace}]
  volumes:
  - name: workspace
    persistentVolumeClaim: { claimName: dw-workspace }
# EOF
```

`docker-compose.yml` equivalent:

```yaml
services:
  agent:
    image: harbor.dmzs.com/datawatch/agent-claude:v2.4.5
    volumes: [./workspace:/workspace]
  lang:
    image: harbor.dmzs.com/datawatch/lang-python:v2.4.5
    command: sleep infinity
    volumes: [./workspace:/workspace]
```

### KMP/Android (datawatch-app)

```yaml
spec:
  containers:
  - name: agent
    image: harbor.dmzs.com/datawatch/agent-claude:v2.4.5
  - name: lang
    image: harbor.dmzs.com/datawatch/lang-kotlin:v2.4.5
    command: ["sleep", "infinity"]
    resources:
      requests: { memory: "2Gi" }   # gradle is hungry
      limits:   { memory: "4Gi" }
```

### Ruby (infosecquote)

```yaml
spec:
  containers:
  - name: agent
    image: harbor.dmzs.com/datawatch/agent-aider:v2.4.5  # aider for diff-driven edits
  - name: lang
    image: harbor.dmzs.com/datawatch/lang-ruby:v2.4.5
    command: ["sleep", "infinity"]
```

(Same model — pick the agent based on workflow style, the lang based on
the project. Aider's diff-by-default UX often suits Rails refactors;
claude is more open-ended.)

---

## Non-coding examples

The whole point: the agent doesn't need to write code to be useful.
Pair it with a `tools-*` image instead of a `lang-*` image and it gets
a different toolbelt.

### Cluster ops & investigation

> "Claude, the staging cluster's API latency p99 doubled this morning.
>  Find the cause and propose a fix."

```yaml
spec:
  containers:
  - name: agent
    image: harbor.dmzs.com/datawatch/agent-claude:v2.4.5
    env:
    - name: KUBECONFIG
      value: /workspace/.kube/config
  - name: tools
    image: harbor.dmzs.com/datawatch/tools-ops:v2.4.5
    command: ["sleep", "infinity"]
  volumes:
  - name: workspace
    secret:
      secretName: investigation-kubeconfig  # mounts kubeconfig under /workspace/.kube
```

`tools-ops` ships kubectl + helm + terraform + aws/gcloud + dig + nmap
+ mtr + yq. The agent shells out to them and writes findings into
`/workspace/findings.md`. No code generated.

### Research / writing / analysis

```yaml
spec:
  containers:
  - name: agent
    image: harbor.dmzs.com/datawatch/agent-claude:v2.4.5
  # Solo — agent-base already has git, curl, ripgrep, jq, less.
  # Workspace mounted from a notes / research repo.
```

This is "claude with no language pairing". You get markdown editing,
git, basic shell. Useful for journal/notes/research/writing tasks
that don't need a toolchain.

### Data exploration (lightweight)

```yaml
spec:
  containers:
  - name: agent
    image: harbor.dmzs.com/datawatch/agent-claude:v2.4.5
  - name: lang
    image: harbor.dmzs.com/datawatch/lang-python:v2.4.5  # pandas/jupyter on demand
    command: ["sleep", "infinity"]
```

Then in the agent:
```
> claude, install pandas + duckdb in the workspace and analyze the
  CSVs in /workspace/raw/
> [claude shells into the lang-python container via kubectl exec,
   uses uv to install pandas + duckdb in a project venv, runs them
   on /workspace/raw/, writes /workspace/analysis.md]
```

### Multi-agent (orchestrator + executor)

A more advanced pattern from F10 Sprint 7's plan: one agent acts as
orchestrator, another as executor.

```yaml
spec:
  containers:
  - name: orchestrator
    image: harbor.dmzs.com/datawatch/agent-claude:v2.4.5
    env: [{name: ROLE, value: orchestrator}]
  - name: executor
    image: harbor.dmzs.com/datawatch/agent-aider:v2.4.5
    env: [{name: ROLE, value: executor}]
  - name: lang
    image: harbor.dmzs.com/datawatch/lang-go:v2.4.5
    command: ["sleep", "infinity"]
```

Use case: claude plans the change set, hands subtasks to aider, aider
executes diffs against the workspace. Both share `/workspace` so each
sees the other's progress.

### Bash / shell-only assistant

For quick interactive shell work without any agent (operator drives,
maybe with a recording of session output):

```yaml
spec:
  containers:
  - name: tools
    image: harbor.dmzs.com/datawatch/tools-ops:v2.4.5
    stdin: true
    tty: true
    command: ["bash"]
```

Datawatch records the session, archives it to memory, can replay or
summarize via an out-of-band agent later. (No agent in the Pod itself.)

---

## How to pick the agent

| Agent | When to reach for it |
|---|---|
| `agent-claude` | Default. Open-ended tasks, conversation, exploration. |
| `agent-opencode` | Long-running multi-step refactors. Strong on structured edits. |
| `agent-gemini` | Cheap-tier or Google-ecosystem work; multimodal. |
| `agent-aider` | Diff-driven edits to existing code. Strong git integration. |

Mix them in one Pod when their strengths complement each other.

## How to pick the second container

| Pair | Use case |
|---|---|
| `lang-go` | Go services, datawatch itself |
| `lang-node` | Node/TS apps, frontends, scripts |
| `lang-python` | Python services, data work, ML scripting |
| `lang-rust` | Rust crates, systems work |
| `lang-kotlin` | KMP, Android, datawatch-app |
| `lang-ruby` | Rails apps, infosecquote |
| `tools-ops` | Cluster/cloud admin, infra investigation |
| **(none)** | Writing, research, conversation, journaling |

---

## Profile-driven composition (Sprint 2+)

Today the manifests above are hand-written. Sprint 2's Project Profile
schema gains an `image_pair` that the K8s driver expands automatically:

```yaml
profiles:
  - name: datawatch-app
    git: { url: https://github.com/dmz006/datawatch-app, branch: main }
    image_pair:
      agent: agent-claude
      sidecar: lang-kotlin
    memory: { mode: shared }

  - name: prod-cluster-debug
    image_pair:
      agent: agent-claude
      sidecar: tools-ops
    workspace_mounts:
      - { name: kubeconfig, secret: prod-kubeconfig, path: /workspace/.kube }
    memory: { mode: ephemeral }
```

`datawatch agent spawn --profile prod-cluster-debug --task "investigate API latency"`
materializes the Pod, runs claude with kubectl access, archives findings
to memory (or doesn't, if ephemeral).

---

## Adding new tools-* or lang-* images

The pattern is small and consistent:

1. New file at `docker/dockerfiles/Dockerfile.<variant>`
2. `FROM ${REGISTRY}/agent-base:${BASE_TAG}`
3. Add only what's specific to your domain — agent-base already has
   git, ssh, curl, jq, ripgrep, fd, gh, tmux, the datawatch binary
4. Land in `LANG_TYPES` or a new `TOOL_TYPES` list in the Makefile so
   `make container` picks it up
5. Smoke locally with `make container-<variant> PUSH=false`
6. Push: `make container-<variant>` (with `.env.build` set)

Worked examples in this repo: `lang-ruby` (added for infosecquote),
`tools-ops` (this doc's primary non-coding example).
