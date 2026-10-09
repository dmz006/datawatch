# 🛡️ AgentArmor

[![PyPI version](https://img.shields.io/pypi/v/agentarmor-core)](https://pypi.org/project/agentarmor-core/)
[![Python](https://img.shields.io/pypi/pyversions/agentarmor-core)](https://pypi.org/project/agentarmor-core/)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Tests](https://img.shields.io/badge/tests-127%2B_passing-brightgreen)](https://github.com/Agastya910/agentarmor)

**Comprehensive open-source security framework for agentic AI applications.**

AgentArmor provides 8-layer defense-in-depth security for AI agents, covering every point in the data flow where data is at rest, in transit, or in use. Built to address the [OWASP Top 10 for Agentic Applications (2026)](https://owasp.org/www-project-top-10-for-agentic-security-and-integrity/).

---

## 🚀 What's New in v0.5.0 — Hardened Security Layers

This is a **major security release** that upgrades four layers from basic implementations to production-grade, adversarially-tested enforcement engines:

- 🧠 **L3: Hardened Context Assembly** — GoalLock anchor prevents goal hijacking mid-conversation. CanaryVault injects multiple unique canary tokens per session. Tiered context assembly strips template injection before it reaches the LLM. Validated against 48 adversarial test cases.

- 🎯 **L4: Hardened Planning & Reasoning** — ActionChainTracker detects multi-step attack chains (reconnaissance → escalation → exfiltration). Semantic risk scoring evaluates action intent, not just verbs. Validated against 40 adversarial test cases.

- 🔒 **L5: Hardened Execution Control** — Five enforcement domains: Network Policy (DNS rebinding + SSRF protection), Rate Limiting (token bucket + circuit breaker), Resource Budget (timeout + size limits), Output Sanitizer (UTF-8 + binary strip), and Side-Effect Auditor (immutable execution records). Validated against 39 adversarial test cases.

- 🛡️ **L6: Hardened Output Security** — Five-scanner pipeline: Credential Scanner (13+ patterns, zero false positives), PII Scanner (confidence-gated Presidio), Harmful Content Detector (jailbreak + system prompt leak detection), Semantic Exfiltration Detector (cross-response tracking), and Schema Validation. Supports both streaming and non-streaming responses. Validated against 12 adversarial test cases.

- 🔐 **L2: Encrypted Storage** — All data stored in Studio's SQLite database is now AES-256-GCM encrypted with HMAC-based MAC signatures for tamper detection.

> **127+ adversarial test cases** validate the hardened layers end-to-end.

---

## Why AgentArmor?

Every existing security tool is a **point solution** — output validators, prompt injection scanners, or policy engines in isolation. AgentArmor is the **first unified framework** that secures the entire agentic architecture end-to-end.

## The 8 Security Layers

| Layer | Name            | What It Protects                                                          |
| ----- | --------------- | ------------------------------------------------------------------------- |
| L1    | **Ingestion**   | Input scanning, prompt injection detection, source verification           |
| L2    | **Storage**     | AES-256-GCM encryption at rest, HMAC integrity, tamper detection          |
| L3    | **Context**     | GoalLock anchoring, multi-canary injection, template injection stripping  |
| L4    | **Planning**    | Action chain tracking, semantic risk scoring, multi-step attack detection |
| L5    | **Execution**   | DNS rebinding protection, rate limiting, circuit breakers, resource budgets |
| L6    | **Output**      | Credential redaction, PII scanning, harmful content blocking, exfiltration detection |
| L7    | **Inter-Agent** | Mutual auth (HMAC), trust scoring with time decay, delegation depth control |
| L8    | **Identity**    | Agent identity, JIT permissions, credential rotation                      |

## Quick Start

### Install

```bash
# Using uv (recommended)
uv add agentarmor-core

# With MCP server support (for Claude Code, OpenClaw, etc.)
uv add "agentarmor-core[mcp]"

# With PII detection
uv add "agentarmor-core[pii]"

# With all optional features
uv add "agentarmor-core[all]"

# Available extras: proxy, pii, otel, mcp, oauth, all, dev
```

```bash
# For development
git clone https://github.com/Agastya910/agentarmor.git
cd agentarmor
uv sync --all-extras --dev
```

### Basic Usage

```python
import asyncio
from agentarmor import AgentArmor, ArmorConfig

async def main():
    armor = AgentArmor()

    # Register your agent
    identity, token = armor.l8_identity.register_agent(
        agent_id="my-agent",
        permissions={"read.*", "search.*"},
    )

    # Intercept tool calls
    result = await armor.intercept(
        action="read.file",
        params={"path": "/data/notes.txt"},
        agent_id="my-agent",
        input_data="Read the file please",
    )

    print(f"Safe: {result.is_safe}")
    print(f"Verdict: {result.final_verdict.value}")

asyncio.run(main())
```

### Use as Decorator

```python
@armor.shield(action="database.query")
async def query_database(sql: str) -> dict:
    return db.execute(sql)
```

### Proxy Server Mode

```bash
agentarmor serve --config agentarmor.yaml --port 8400
```

```bash
curl -X POST http://localhost:8400/v1/intercept \
  -H "Content-Type: application/json" \
  -d '{"action": "read.file", "agent_id": "my-agent", "input_data": "Hello"}'
```

---

## Hardened Layer Examples

### L3 Context Hardening — GoalLock

AgentArmor's L3 layer prevents goal hijacking by anchoring the agent's purpose at the start of every conversation. Template injection attempts are stripped before reaching the LLM.

```python
from agentarmor.layers.context.assembler import L3ContextLayer

l3 = L3ContextLayer(
    agent_id="my-agent",
    agent_config={
        "system_prompt": "You are a helpful assistant.",
        "tools": ["web_search", "file_read"],
    },
)

# Build a hardened system prompt with canary tokens and GoalLock
hardened_prompt = l3.build_secure_system_prompt(
    base_system_prompt="You are a helpful assistant.",
    conversation_id="session-123",
)

# After LLM responds, check for canary leaks and goal drift
safe_response, events = await l3.check_output(
    conversation_id="session-123",
    response=llm_response,
    tool_calls=[],
    turn_number=1,
    user_message=user_input,
)
```

### L5 Execution Hardening — Network Policy

The L5 layer enforces DNS rebinding protection, protocol restrictions, and domain allowlists/blocklists on every outbound request.

```python
from agentarmor.layers.execution.l5_execution import L5ExecutionLayer, NetworkPolicy

l5 = L5ExecutionLayer(
    agent_id="my-agent",
    network_policy=NetworkPolicy(
        allow_http=False,  # HTTPS only
        domain_allowlist=["api.github.com", "*.openai.com"],
        domain_blocklist=["metadata.google.internal", "*.local"],
        dns_rebinding_protection=True,
        max_outbound_payload_bytes=50_000,
    ),
)

# Execute a tool with full L5 enforcement
result, event = await l5.execute(
    tool_name="web_search",
    tool_args={"query": "latest AI news"},
    tool_func=my_search_function,
    session_id="session-123",
    outbound_url="https://api.tavily.com/search",
)
```

### L6 Output Hardening — 5-Scanner Pipeline

The L6 layer scans every output for credentials, PII, harmful content, and semantic exfiltration patterns.

```python
from agentarmor.layers.output.filter import L6OutputLayer

l6 = L6OutputLayer(
    agent_id="my-agent",
    enable_pii_scan=True,
    enable_harmful_scan=True,
)

# Scan a response
safe_text, result = l6.process(llm_response, session_id="session-123")

if result["verdict"] == "block":
    print("Response contained critical security violation!")
else:
    print(f"Cleaned: {result['findings_count']} findings redacted")
```

---

## Integrations

### MCP Server — Zero-Code Security for Any Agent *(v0.4.0)*

AgentArmor runs as a native **MCP server** that any MCP-compatible coding agent can call directly — no Python code changes needed in your project.

**Setup for Claude Code** — add to `~/.claude/claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "agentarmor": {
      "command": "uv",
      "args": ["run", "agentarmor-mcp"],
      "cwd": "/path/to/your/project"
    }
  }
}
```

**Available MCP Tools:**

| Tool | What It Does |
|------|-------------|
| `armor_register_agent` | Register an agent with a permission set |
| `armor_scan_input` | Scan text for prompt injection, jailbreaks, DAN attacks |
| `armor_intercept` | Run a tool call through all 8 security layers |
| `armor_scan_output` | Redact PII (emails, SSNs, API keys) from output |
| `armor_scan_mcp_server` | Full TLS + OAuth 2.1 + rug-pull scan of any MCP server |
| `armor_get_status` | Health check: version, layers, registered agents |

> 📖 **Full setup guide:** [docs/claude_code_setup.md](docs/claude_code_setup.md)

### TLS + OAuth 2.1 Verification *(v0.3.0)*

```python
from agentarmor import MCPGuard

guard = MCPGuard()
result = guard.full_security_scan("https://api.example.com/mcp")
print(result["overall_risk"])  # "low" / "medium" / "high" / "critical"
```

### OpenClaw Identity Guard *(v0.2.0)*

```python
from agentarmor import OpenClawGuard
guard = OpenClawGuard(identity_dir="~/.openclaw")
enc_report = guard.encrypt_identity_files()  # AES-256-GCM + BLAKE3
```

### LangChain / OpenAI

```python
# LangChain
from agentarmor.integrations.langchain import AgentArmorCallback
callback = AgentArmorCallback(armor=armor)

# OpenAI
from agentarmor.integrations.openai import secure_openai_client
client = secure_openai_client(OpenAI(), armor=armor)
```

> 📖 **Full integration guide:** [docs/integrations.md](docs/integrations.md)

---

## CLI Commands

| Command                        | Description                            |
| ------------------------------ | -------------------------------------- |
| `agentarmor init`              | Generate a config file                 |
| `agentarmor validate <config>` | Validate configuration                 |
| `agentarmor scan -t "text"`    | Scan text for threats                  |
| `agentarmor serve`             | Start proxy server                     |
| `agentarmor keygen`            | Generate encryption key                |
| `agentarmor-mcp`               | Start MCP server (stdio transport)     |

## Custom Security Policies

```yaml
# policies/my_agent.yaml
version: "1.0"
name: "database_agent"
agent_type: "database"
risk_level: "high"

global_denied_actions:
  - "database.drop"
  - "database.truncate"

require_human_approval_for:
  - "database.delete"

rules:
  - name: "limit_transfer_amount"
    action_pattern: "transfer.*"
    conditions:
      - field: "params.amount"
        operator: ">"
        value: "1000"
    verdict: "escalate"
    priority: 100
```

## Architecture

```
                            MCP Agents (Claude Code, OpenClaw, Cursor, etc.)
                                       │
                                  stdio │ (agentarmor-mcp)
                                       ▼
Agent Runtime                   ┌─────────────────┐
(LangChain /                    │  MCP Server      │
 CrewAI /                       │  6 tools         │
 OpenAI SDK /  ─── Python ────► │  (v0.4.0)        │
 MCP)                           └────────┬─────────┘
         │                               │
         └───────────────┬───────────────┘
                         ▼
              ┌─────────────────────────────┐
              │      AgentArmor Pipeline     │
              │  ┌───────────────────────┐  │
              │  │  L8: Identity & IAM   │  │
              │  ├───────────────────────┤  │
              │  │  L1: Data Ingestion   │  │
              │  ├───────────────────────┤  │
              │  │  L2: Memory/Storage   │  │
              │  ├───────────────────────┤  │
              │  │  L3: Context Assembly │  │
              │  ├───────────────────────┤  │
              │  │  L4: Plan Validation  │  │
              │  ├───────────────────────┤  │
              │  │  L5: Action Execution │  │
              │  ├───────────────────────┤  │
              │  │  L7: Inter-Agent Sec  │  │
              │  └───────────────────────┘  │
              │  L6: Output Filter (post)   │
              │  Audit Logger (cross-cut)   │
              │  Policy Engine (cross-cut)  │
              └─────────────────────────────┘
                         │
                         ▼
                External Tools / APIs / LLMs
```

## OWASP ASI Coverage

| OWASP ASI Risk            | AgentArmor Layer(s)                          |
| ------------------------- | -------------------------------------------- |
| ASI01: Goal Hijacking     | L1 (injection), L3 (GoalLock + canary tokens) |
| ASI02: Tool Misuse        | L4 (chain tracking), L5 (execution gates), Policy Engine |
| ASI03: Identity Abuse     | L8 (identity), L5 (JIT perms), OpenClaw Guard |
| ASI04: Supply Chain       | L1 (source verify), MCP Scanner              |
| ASI05: Code Execution     | L5 (5-domain enforcement), L4 (risk scoring)  |
| ASI06: Memory Poisoning   | L2 (AES-256-GCM + MAC integrity), L3 (canary tokens) |
| ASI07: Inter-Agent        | L7 (mutual auth, trust scoring with decay)    |
| ASI08: Cascading Failures | L4 (chain depth + circuit breaker), L5 (rate limits) |
| ASI09: Human Trust        | L6 (5-scanner pipeline), Audit Logger         |
| ASI10: Rogue Agents       | L8 (credential rotation), L7 (trust decay)    |

## Documentation

| Doc | Description |
|-----|-------------|
| [Quick Start](docs/quickstart.md) | Installation and first steps |
| [Hardened Layers](docs/hardened_layers.md) | Deep dive into the v0.5.0 hardened security layers |
| [Claude Code Setup](docs/claude_code_setup.md) | MCP server setup for Claude Code, OpenClaw, Cursor |
| [Architecture](docs/architecture.md) | 8-layer pipeline design and data flow |
| [Integrations](docs/integrations.md) | MCP Server, OpenClaw, TLS/OAuth, LangChain, OpenAI |
| [Policy Language](docs/policy_language.md) | YAML policy reference and examples |
| [Threat Model](docs/threat_model.md) | OWASP ASI attack vectors and defenses |
| [Use Cases](docs/use_cases.md) | Financial, coding, RAG, multi-agent, MCP examples |
| [Publishing](docs/pypi_and_github.md) | PyPI & GitHub release guide |

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

Apache 2.0. Free for commercial and open-source use.
