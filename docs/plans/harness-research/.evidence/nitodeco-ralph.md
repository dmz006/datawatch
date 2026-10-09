# Ralph

<div align="center">

## A CLI tool for long-running PRD-driven development with AI coding agents

[![Version](https://img.shields.io/github/v/release/nitodeco/ralph)](https://github.com/nitodeco/ralph/releases/latest)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

![Ralph Screenshot](docs/image.png)

</div>

Ralph automates the process of working through a Product Requirements Document (PRD) by orchestrating AI coding agents (Cursor, Claude Code, or Codex) to complete tasks one at a time.
Ralph is inspired by the Ralph loop methodology created by Geoffrey Huntley and outlines in this article: https://ghuntley.com/ralph/

## Installation

Install Ralph with a single command:

```bash
curl -fsSL https://raw.githubusercontent.com/nitodeco/ralph/main/scripts/install.sh | bash
```

## Quick Start

### 1. Initialize a new project

```bash
ralph init
```

This will interactively create all necessary files in the user directory (`~/.ralph`). You'll be prompted to select your preferred AI agent (Cursor, Claude Code, or Codex).

### 2. Run the agent

```bash
ralph run [iterations]
```

Runs the configured AI agent in a loop, working through tasks in your PRD. Default is 10 iterations.

**The agent workflow:**

1. Run `ralph progress` and `ralph task list` to understand the current state
2. Run `ralph task current` to find the next incomplete task
3. Implement that task
4. Run `ralph progress add` and `ralph task done` to record progress
5. Commit the changes
6. Repeat until all tasks are complete or iterations are exhausted

## PRD Format

Ralph uses JSON under the hood to store the PRD and progress. The PRD can be easily edited using commands in ralph.

## Configuration

### Custom Instructions

You can provide project-specific instructions to the agent by creating an `instructions.md` file in your project's Ralph directory (`~/.ralph/projects/<project>/instructions.md`). The contents of this file will be included in the agent prompt, allowing you to customize behavior, coding standards, or project-specific guidelines.

### Configuration Options

Ralph uses a layered configuration system with global (`~/.ralph/config.json`) and project-level (`~/.ralph/projects/<project>/config.json`) settings. Project settings override global settings.

| Option             | Type                                  | Default              | Description                                                |
| ------------------ | ------------------------------------- | -------------------- | ---------------------------------------------------------- |
| `agent`            | `"cursor"` \| `"claude"` \| `"codex"` | `"cursor"`           | AI coding agent to use                                     |
| `maxRetries`       | number                                | `3`                  | Maximum retry attempts per iteration                       |
| `retryDelayMs`     | number                                | `5000`               | Delay between retries (ms)                                 |
| `agentTimeoutMs`   | number                                | `1800000`            | Agent timeout per iteration (30 min)                       |
| `stuckThresholdMs` | number                                | `300000`             | Time without output before considering agent stuck (5 min) |
| `maxRuntimeMs`     | number                                | `0`                  | Maximum total session runtime (0 = unlimited)              |
| `logFilePath`      | string                                | `".ralph/ralph.log"` | Path to log file                                           |

#### Notification Settings

```json
{
  "notifications": {
    "systemNotification": true,
    "webhookUrl": "https://hooks.slack.com/...",
    "markerFilePath": ".ralph/complete.marker"
  }
}
```

| Option               | Type    | Description                              |
| -------------------- | ------- | ---------------------------------------- |
| `systemNotification` | boolean | Enable OS notifications on completion    |
| `webhookUrl`         | string  | Webhook URL for completion notifications |
| `markerFilePath`     | string  | File to create when session completes    |

#### Memory Settings

```json
{
  "memory": {
    "maxOutputBufferBytes": 5242880,
    "memoryWarningThresholdMb": 500,
    "enableGarbageCollectionHints": true
  }
}
```

| Option                         | Type    | Default   | Description                           |
| ------------------------------ | ------- | --------- | ------------------------------------- |
| `maxOutputBufferBytes`         | number  | `5242880` | Max agent output buffer size (5 MB)   |
| `memoryWarningThresholdMb`     | number  | `500`     | Memory warning threshold              |
| `enableGarbageCollectionHints` | boolean | `true`    | Enable GC hints for memory management |

### Example Configuration

```json
{
  "agent": "claude",
  "maxRetries": 5,
  "retryDelayMs": 10000,
  "agentTimeoutMs": 3600000,
  "stuckThresholdMs": 600000,
  "maxRuntimeMs": 28800000,
  "notifications": {
    "systemNotification": true,
    "webhookUrl": "https://hooks.slack.com/services/..."
  }
}
```

## Requirements

One of the following AI coding agents must be installed and available in your PATH:

- **[Cursor CLI](https://docs.cursor.com/cli)** - available as `agent` command
- **[Claude Code](https://docs.anthropic.com/en/docs/claude-code)** - available as `claude` command
- **[Codex CLI](https://github.com/openai/codex)** - available as `codex` command

## Development

Contributions are welcome! To get started with development:

```bash
# Install dependencies
bun install

# Run in development mode
bun run dev

# Build binaries
bun run build

# Type check
bun run typecheck
```

## Contributing

Contributions are welcome! See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

This project is licensed under the MIT License. See [LICENSE](LICENSE) for details.
