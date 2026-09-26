# Automata capacity admission flow

How a task gets a slot before its session starts, and what the operator
sees while it waits.

```mermaid
sequenceDiagram
    participant Ex as executeOne
    participant L as capacity.Ledger
    participant S as session.Manager
    participant Op as Operator (PWA / CLI / API)

    Ex->>L: Acquire(task, pools: host + node + llm, priority)
    alt every pool has room
        L-->>Ex: lease granted
    else a pool is full or GPU is busy
        L-->>Ex: wait (reason: pool node:gpu-1 full 1/1, held by t42)
        Ex->>Ex: task = waiting_capacity, wait_reason set
        Op->>L: GET /api/capacity (pools, holders, queue)
        Note over L: dispatch on release / limit change / poll:<br/>priority, then aged waiters, model affinity,<br/>least recently served Automaton, FIFO
        L-->>Ex: lease granted
        Ex->>Ex: task = in_progress (no retry consumed)
    end
    Ex->>S: spawn session (may still hit max_sessions)
    S-->>Ex: "max sessions reached" → wait 15 s and retry, still waiting_capacity
    Ex->>L: Bind(task, session)
    Ex->>Ex: verify, finish
    Ex->>L: Release(task)
    Note over L: reaper (10 s) frees leases whose session ended<br/>so a missed release cannot leak capacity
```

## Pools

| Pool | Limit from | Counts |
|---|---|---|
| `host` | `session.max_sessions` minus `session.reserved_interactive` | autonomous leases plus operator sessions on this host |
| `node:<name>` | node `max_concurrent_sessions` | autonomous sessions on that node |
| `llm:<name>` | LLM `max_inflight` | autonomous sessions on that LLM |

A limit of 0 means unlimited. Planning (decompose) sessions occupy a host
slot and wait for one to free instead of failing.
