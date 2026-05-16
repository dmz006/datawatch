# Test Isolation & Secrets-Driven Architecture Guide

## Quick Start

### Basic Run (Ollama only)
```bash
bash scripts/run-tests.sh
```

### With Claude Support (Major Releases)
```bash
export CLAUDE_API_KEY="sk-ant-..."
bash scripts/run-tests.sh
```

### With Kubernetes Tests
```bash
# 1. Import kubectl context
./scripts/import-kubectl-context.sh \
  --context=testing \
  --target-daemon=http://localhost:18080 \
  --token=dw-test-token-12345

# 2. Run tests
bash scripts/run-tests.sh
```

---

## Architecture Overview

### Isolation Model

```
Production Daemon (8443)
├─ Production data at ~/.datawatch/
└─ Never touched by tests

Test Daemon (18080/18443)
├─ Isolated data at ./.datawatch-test/
├─ Foreground mode (no daemonization)
└─ Credentials injected from secrets manager
    ├─ GitHub PAT (via gh CLI)
    ├─ Claude API key (via CLAUDE_API_KEY env)
    ├─ Kubectl context (via import script)
    └─ SSH keys (via export script)

Docker Simulator (19180/19443)
└─ Isolated data for container tests

K8s Testing
└─ Dedicated namespace: datawatch-e2e
```

### Credential Flow

```
1. Test startup (run-tests.sh)
   ↓
2. start_test_daemon() creates config
   ↓
3. Daemon becomes healthy (health check passes)
   ↓
4. _setup_test_secrets() injects credentials
   ├─ GitHub PAT from `gh auth token`
   ├─ Claude API key from CLAUDE_API_KEY env
   └─ Stores in test daemon's secrets manager
   ↓
5. Tests run with injected credentials
   ├─ CLI tests via cli_test() wrapper (forces --config)
   ├─ API tests via api() helper (uses TEST_BASE=18080)
   └─ K8s tests via kubectl with KUBECONFIG env
   ↓
6. cleanup_all() on EXIT
   ├─ Delete GitHub repos created during tests
   ├─ Delete secrets from secrets manager
   ├─ Kill test daemon (PID-validated)
   └─ Clean .datawatch-test/ directory
```

---

## CLI Test Isolation (Critical)

**Problem**: CLI commands default to production daemon at `~/.datawatch/config.yaml` without `--config` flag.

**Solution**: Always use `cli_test()` wrapper which forces `--config "$TEST_DATA/config.yaml"`.

### Before (DANGEROUS ❌)
```bash
# WRONG: This targets production daemon!
"$TEST_BINARY" sessions kill 12345
```

### After (CORRECT ✓)
```bash
# RIGHT: Forces test daemon config
cli_test sessions kill 12345
```

---

## PID Validation for Safe Daemon Kill (Critical)

**Problem**: `kill -0 $PID` only checks if PID exists, doesn't verify it's the right process.

**Solution**: Validate PID is test daemon before killing.

### Before (DANGEROUS ❌)
```bash
if kill -0 "$DAEMON_PID" 2>/dev/null; then
  kill "$DAEMON_PID"  # Could kill wrong process!
fi
```

### After (CORRECT ✓)
```bash
if _validate_test_daemon_pid "$DAEMON_PID" 18080; then
  kill "$DAEMON_PID"  # Verified on port 18080
fi
```

The `_validate_test_daemon_pid()` helper checks:
1. PID exists
2. Process listens on expected port (18080 for test, 8080 for prod)
3. Returns 0 only if both are true

---

## GitHub Credential Management

### Automatic (Built-in)

Test script automatically:
1. Gets GitHub PAT via `gh auth token`
2. Stores in secrets: `test-github-pat`
3. Creates random private test repo: `datawatch-test-<timestamp>`
4. Tracks repo in CLEANUP_LOG
5. Deletes repo on cleanup (via `gh repo delete --confirm`)

### Manual (Advanced)

```bash
# Create and use a specific test repo
gh repo create my-test-project --private
git clone https://github.com/YOU/my-test-project
# ... run tests that reference my-test-project ...
gh repo delete my-test-project --confirm
```

---

## Claude Backend Configuration

### For Major Releases Only

When testing Claude integration:

```bash
export CLAUDE_API_KEY="sk-ant-..."
bash scripts/run-tests.sh
```

This automatically:
1. Detects `CLAUDE_API_KEY` env var
2. Adds Claude section to test daemon config
3. Uses `claude-haiku-4-5` with `quick` effort (minimizes cost)
4. Tests can now use `backend:claude-code` in automaton specs

### Test Config (Auto-Generated)

```yaml
claude:
  enabled: true
  api_key_ref: ${secret:claude-test-api-key}
  model: claude-haiku-4-5-20251001
  default_effort: quick
```

---

## Kubernetes Context Import

### Step 1: Export Context to Secrets

```bash
./scripts/import-kubectl-context.sh \
  --context=testing \
  --target-daemon=http://localhost:18080 \
  --token=dw-test-token-12345
```

This:
1. Exports kubectl context to JSON
2. POSTs to test daemon's `/api/secrets`
3. Stores as `k8s-context-testing`

### Step 2: Use in Tests

```bash
# Retrieve kubeconfig from secrets
KUBECONFIG=/tmp/kubeconfig-test
api GET /api/secrets/k8s-context-testing > "$KUBECONFIG"
export KUBECONFIG

# Now kubectl commands target test daemon's K8s context
kubectl get pods
```

---

## SSH Public Key Export

For tests that need SSH authentication:

```bash
./scripts/export-ssh-pubkey.sh \
  --key-path=$HOME/.ssh/id_rsa.pub \
  --target-daemon=http://localhost:18080 \
  --token=dw-test-token-12345
```

This:
1. Reads SSH public key
2. POSTs to test daemon's `/api/secrets`
3. Stores as `ssh-test-pubkey`

Usage in test code:
```bash
pubkey=$(api GET /api/secrets/ssh-test-pubkey | jq -r '.value')
echo "$pubkey" >> ~/.ssh/authorized_keys
```

---

## Troubleshooting

### Test daemon won't start
```bash
# Check logs
tail -20 .datawatch-test/daemon.log

# Verify port not in use
lsof -i :18080
```

### CLI test fails with "connection refused"
```bash
# Wrong:
cli_test sessions list
# Error: can't connect to :8080 (production daemon)

# This means cli_test() wrapper isn't working
# Check that run-tests.sh has cli_test() function defined
grep -A3 "^cli_test()" scripts/run-tests.sh
```

### GitHub repo creation fails
```bash
# Verify gh is authenticated
gh auth status

# Verify token has repo scope
gh auth token
```

### Claude tests skip/fail
```bash
# Verify env var set
echo $CLAUDE_API_KEY

# Verify config includes claude block
grep -A5 "^claude:" .datawatch-test/config.yaml

# Check daemon logs for secret injection errors
tail -50 .datawatch-test/daemon.log | grep -i claude
```

---

## Implementation Details

### Files Modified

- `scripts/run-tests.sh` — Added credential injection, PID validation, GitHub repo creation
- `scripts/import-kubectl-context.sh` — New utility for K8s context import
- `scripts/import-claude-credentials.sh` — New utility for Claude API key import
- `scripts/export-ssh-pubkey.sh` — New utility for SSH key export

### Environment Variables

| Variable | Purpose | Example |
|----------|---------|---------|
| `CLAUDE_API_KEY` | Claude API key (for major releases) | `sk-ant-...` |
| `TEST_BASE` | Test daemon HTTP URL | `http://127.0.0.1:18080` |
| `TEST_TLS` | Test daemon HTTPS URL | `https://127.0.0.1:18443` |
| `TEST_TOKEN` | Bearer token for test daemon | `dw-test-token-12345` |
| `TEST_DATA` | Test data directory | `./.datawatch-test` |
| `KUBECONFIG` | Path to kubectl config (used by K8s tests) | `/tmp/kubeconfig-test` |

---

## Best Practices

1. **Always use `cli_test()` for CLI commands** — prevents accidental production daemon kill
2. **Validate PIDs before killing** — especially for daemon lifecycle
3. **Track created resources in CLEANUP_LOG** — enables precise cleanup without wildcards
4. **Don't hardcode external credentials** — use secrets manager for all auth
5. **Use smallest/cheapest models for major releases** — `claude-haiku-4-5` + `quick` effort
6. **Test in isolation first** — run against test daemon, then production smoke test
7. **Document new credential types** — update this guide when adding new auth import/export
