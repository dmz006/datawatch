# Windows Installation

> **Recommended**: Use WSL2 (Windows Subsystem for Linux) and follow the Linux installation instructions.

## Option A: WSL2 (Recommended)

WSL2 gives you a full Linux environment on Windows.

```powershell
# Install WSL2
wsl --install

# After restart, open Ubuntu in WSL2, then follow Linux instructions:
curl -fsSL https://raw.githubusercontent.com/dmz006/claude-signal/main/install/install.sh | bash
```

## Option B: Native Windows

### Prerequisites

1. **Java 17+** — Download from [Adoptium](https://adoptium.net/)
   ```powershell
   winget install EclipseAdoptium.Temurin.17.JDK
   ```

2. **tmux** — via Scoop
   ```powershell
   Set-ExecutionPolicy RemoteSigned -Scope CurrentUser
   irm get.scoop.sh | iex
   scoop install tmux
   ```

3. **Git** — usually pre-installed, or:
   ```powershell
   winget install Git.Git
   ```

### Install signal-cli

```powershell
$version = "0.13.4"
$url = "https://github.com/AsamK/signal-cli/releases/download/v$version/signal-cli-$version.tar.gz"
Invoke-WebRequest -Uri $url -OutFile "$env:TEMP\signal-cli.tar.gz"
tar -xzf "$env:TEMP\signal-cli.tar.gz" -C "$env:LOCALAPPDATA"
# Add to PATH via System Properties > Environment Variables
```

### Install claude-signal

```powershell
# Download binary
$url = "https://github.com/dmz006/claude-signal/releases/latest/download/claude-signal-windows-amd64.exe"
Invoke-WebRequest -Uri $url -OutFile "$env:LOCALAPPDATA\claude-signal\claude-signal.exe"
# Add $env:LOCALAPPDATA\claude-signal to PATH
```

### Run as a Windows Service

Using NSSM (Non-Sucking Service Manager):
```powershell
winget install NSSM.NSSM

nssm install ClaudeSignal "C:\Users\YOU\AppData\Local\claude-signal\claude-signal.exe"
nssm set ClaudeSignal AppParameters "start"
nssm set ClaudeSignal AppDirectory "C:\Users\YOU"
nssm set ClaudeSignal DisplayName "Claude Signal"
nssm set ClaudeSignal Description "Signal to Claude Code Bridge"
nssm set ClaudeSignal Start SERVICE_AUTO_START
nssm start ClaudeSignal
```

View logs:
```powershell
nssm edit ClaudeSignal
# or
Get-EventLog -LogName Application -Source ClaudeSignal -Newest 50
```
