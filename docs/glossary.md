# Glossary

Short definitions of terms used across the docs. The full reference is
[datawatch-definitions.md](datawatch-definitions.md).

## Pool fall-through

The setting `autonomous.capacity_fallthrough` (default off). When on, a worker
task whose requested capacity pool cannot be granted is admitted on an
alternate backend that has room instead of waiting. Verifiers never fall
through. See [howto/automata-capacity.md](howto/automata-capacity.md#pool-fall-through).

## Allowed fall-through target

An LLM entry setting (`fallthrough_target`, default off). With pool
fall-through on, a marked entry may receive a worker whose requested pool is
full. The other lane fill backend is tried first and needs no mark. See
[howto/automata-capacity.md](howto/automata-capacity.md#pool-fall-through).

## agent-aider venv Python

The Python that runs aider inside the agent-aider container image: a uv-managed Python 3.12 in a dedicated venv, not Debian 12's system Python 3.11.2. It exists so soupsieve 4.x (needs Python 3.11.5 or newer) can be installed. See [Container image refresh](howto/container-image-refresh.md#agent-aider-python-and-the-soupsieve-floor).
