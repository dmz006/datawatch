# Routing rules — backend auto-selection (BL20)

**Shipped in v3.9.0.** Pattern-driven backend dispatch on session
start. The first matching rule overrides any default; no match falls
through to the request's explicit backend or `session.llm_backend`.

---

## When to use

- Route "fix the test" / "add tests" tasks to claude-code while
  letting Ollama handle quick research questions.
- Route privacy-sensitive prompts to a local backend (`ollama`,
  `openwebui`) regardless of operator default.
- A/B which backend handles which kind of work without changing
  every operator command.

---

## Configuration

```yaml
session:
  routing_rules:
    - pattern:     "(?i)(test|spec|coverage)"
      backend:     "claude-code"
      description: "Test work goes to Claude"
    - pattern:     "(?i)(quick|small|tiny)"
      backend:     "ollama"
      description: "Trivial tasks go local"
    - pattern:     "(?i)refactor"
      backend:     "opencode"
      description: "Refactors prefer opencode's wider context"
```

`pattern` is a Go regexp matched against the task text on session
start. `backend` is any registered LLM backend name. `description` is
operator-facing context.

Rules are evaluated in order; first match wins. An empty
`routing_rules` list disables auto-selection.

---

## Endpoints

```
GET    /api/routing-rules
POST   /api/routing-rules                body: {"rules": [...]}
POST   /api/routing-rules/test           body: {"task": "..."}
                                         response: {"matched": bool, "backend": "...", "pattern": "..."}
```

`POST /api/routing-rules` validates every regex before saving; an
invalid pattern returns 400 without modifying state.

---

## CLI

```bash
datawatch routing-rules list
datawatch routing-rules test "add unit tests for the parser"
```

## MCP

```
routing_rules_list
routing_rules_test  args: { task: "..." }
```

## Comm

Use the generic `rest` passthrough:
```
rest GET /api/routing-rules
rest POST /api/routing-rules/test {"task":"add tests for x"}
```
