# How-to: Project + Cluster Profiles

Project Profiles describe **what** to do (workspace dir + git policy
+ default backend + skills + pre/post hooks). Cluster Profiles
describe **where** to do it (Kubernetes context + namespace + node
selector + resource limits). Sessions and Automatons reference
profiles by name so the operator doesn't repeat configuration.

## What it is

Two operator-managed YAML stores:

- `~/.datawatch/profiles/projects/<name>.yaml` — Project Profile.
- `~/.datawatch/profiles/clusters/<name>.yaml` — Cluster Profile.

The session-spawn flow takes `--profile <name>` (project) and
`--cluster <name>` (cluster) and inherits everything declared.
Operator-overrides on the spawn command take precedence.

## Base requirements

- `datawatch start` — daemon up.
- For Cluster Profiles: a working kubeconfig with cluster access from
  the daemon's host.

## Setup

```sh
mkdir -p ~/.datawatch/profiles/{projects,clusters}
```

That's it — profiles are operator-authored YAMLs.

## Two happy paths

### 4a. Happy path — CLI

```sh
# 1. Author a Project Profile.
cat > ~/.datawatch/profiles/projects/datawatch-dev.yaml <<'EOF'
name: datawatch-dev
project_dir: ~/work/datawatch
default_backend: claude-code
default_effort: thorough
skills:
  - go-style
  - test-first
  - rtk-cli-aware
git:
  auto_init: false
  auto_commit: true
  commit_template: "%feat(scope): summary\n\nDetails ..."
hooks:
  pre_session: scripts/pre-session-checks.sh
  post_session: scripts/post-session-tidy.sh
env:
  GO111MODULE: "on"
  GOPROXY: "https://proxy.golang.org,direct"
EOF

# 2. List + verify.
datawatch profiles projects list
#  → datawatch-dev   workspace=~/work/datawatch  backend=claude-code

datawatch profiles projects get datawatch-dev

# 3. Spawn a session against it.
datawatch sessions start --profile datawatch-dev --task "Audit BL266"
# Inherits project_dir, backend, effort, skills, git policy, env.

# 4. Author a Cluster Profile (if you'll use container workers).
cat > ~/.datawatch/profiles/clusters/lab-east.yaml <<'EOF'
name: lab-east
kubeconfig: ~/.kube/lab-east.yaml
namespace: datawatch-agents
node_selector:
  workload: ai
resource_limits:
  cpu: "2"
  memory: 4Gi
image: datawatch/agent:latest
image_pull_policy: IfNotPresent
EOF

datawatch profiles clusters list
#  → lab-east   ns=datawatch-agents  context=...

# 5. Spawn an agent worker against the cluster.
datawatch sessions start --backend claude-code --agent k8s \
  --cluster lab-east --profile datawatch-dev --task "Run integration tests"
```

### 4b. Happy path — PWA

1. Settings → Agents → **Project Profiles** card → click **+ New Profile**.
2. Editor opens with a starter YAML; fill in name, workspace dir,
   default backend, skills (multi-select chip picker), git policy,
   pre/post hook script paths, env vars. **Save**.
3. The new profile appears in the card. Edit / Clone / Delete actions
   on each row.
4. Repeat for **Cluster Profiles** below — same shape but with
   kubeconfig path, namespace, node selector, resource limits.
5. Spawn a session: Sessions tab → + FAB → **Workspace** dropdown
   shows configured Project Profiles. Pick one; the wizard pre-fills
   backend/effort/skills.
6. Same FAB has a **Cluster** dropdown when `agent: k8s` is chosen.

## Other channels

### 5a. Mobile (Compose Multiplatform)

Same Settings → Agents → Project + Cluster Profiles cards. YAML
editor renders as a multi-line text input.

### 5b. REST

```sh
# Project profiles.
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/profiles/projects
curl -sk -H "Authorization: Bearer $TOKEN" \
  $BASE/api/profiles/projects/datawatch-dev
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d @datawatch-dev.json \
  $BASE/api/profiles/projects
curl -sk -X DELETE -H "Authorization: Bearer $TOKEN" \
  $BASE/api/profiles/projects/datawatch-dev

# Cluster profiles — same shape under /api/profiles/clusters.
```

### 5c. MCP

Tools: `profile_project_list`, `profile_project_get`,
`profile_project_set`, `profile_project_delete`,
`profile_cluster_list`, etc.

When an autonomous LLM coordinator wants to spawn a sub-PRD against
a specific workspace, it can `profile_project_get` to read the
defaults, then `prd_create` with the matching `profile:` reference.

### 5d. Comm channel

| Verb | Example |
|---|---|
| `profile list` | Lists Project + Cluster Profiles. |
| `profile use <name>` | Sets the chat's default profile. |
| `profile show <name>` | Returns YAML in the chat. |

Authoring profiles is gated to CLI/PWA — too much YAML for chat.

### 5e. YAML

Project Profile schema:

```yaml
name: <required; matches filename>
project_dir: <required; absolute or ~ path>
default_backend: <claude-code | ollama | openai | ...>
default_model: <optional; backend-specific>
default_effort: <quick | normal | thorough>      # claude-code only
skills: [<skill-name>, ...]
git:
  auto_init: <bool>
  auto_commit: <bool>
  commit_template: <printf-style>
hooks:
  pre_session: <script path; absolute or relative to project_dir>
  post_session: <script path>
env:
  KEY: VALUE
  TOKEN: ${secret:NAME}      # secrets refs work in env
```

Cluster Profile schema:

```yaml
name: <required>
kubeconfig: <path to kubeconfig>
context: <optional context name within the kubeconfig>
namespace: <namespace>
node_selector: { key: value }
resource_limits: { cpu: "...", memory: "..." }
image: <agent image>
image_pull_policy: <Always | IfNotPresent | Never>
service_account: <optional>
```

`datawatch reload` picks up new / edited profiles.

## Diagram

```
  ┌──────────────────────┐
  │ Project Profile       │
  │  workspace + git +    │
  │  skills + backend     │
  └──────────┬───────────┘
             │ session --profile <name>
             ▼
  ┌──────────────────────┐    ┌──────────────────────┐
  │ Session spawn         │ +  │ Cluster Profile       │
  │  (operator command)   │    │  kubeconfig + ns +    │
  └──────────┬───────────┘    │  resources + image    │
             │                 └──────────┬───────────┘
             │  --cluster <name>         │
             ▼                            │
        local tmux                        │
        OR                                ▼
        agent pod (Docker / k8s) ────► joins Tailscale mesh
```

## Common pitfalls

- **Profile name vs filename mismatch.** Daemon takes the `name:`
  field from the YAML, not the filename. Keep them in sync to avoid
  confusion.
- **Operator-overrides win.** `--backend claude-code` on the spawn
  command overrides the profile's `default_backend`. Useful but easy
  to forget.
- **Skills not synced.** Profile references a skill not in the local
  registry → spawn fails. Sync first: `datawatch skills sync`.
- **Cluster context typo.** kubectl works locally but the daemon
  fails to spawn — usually a context-name mismatch. Verify with
  `kubectl --kubeconfig <path> config get-contexts`.
- **Hook scripts not executable.** Pre/post hooks must be `chmod +x`.
  The daemon won't auto-fix.

## Linked references

- See also: [`container-workers.md`](container-workers.md) — Cluster Profiles in action.
- See also: [`skills-sync.md`](skills-sync.md) — installing skills.
- See also: [`autonomous-planning.md`](autonomous-planning.md) — PRDs reference profiles.
- Architecture: `../architecture-overview.md` § Profiles.

## Screenshots needed (operator weekend pass)

- [ ] Settings → Agents → Project Profiles card with multiple entries
- [ ] Project Profile editor with filled-in YAML
- [ ] Cluster Profiles card
- [ ] Sessions FAB wizard with Workspace dropdown showing profiles
- [ ] CLI `datawatch profiles projects list` output
