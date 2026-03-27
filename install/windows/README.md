# Windows Installation

> **Recommended**: Use WSL2 (Windows Subsystem for Linux) and follow the Linux installation instructions.

## Option A: WSL2 (Recommended)

WSL2 gives you a full Linux environment on Windows.

```powershell
# Install WSL2
wsl --install

# After restart, open Ubuntu in WSL2, then follow Linux instructions:
curl -fsSL https://raw.githubusercontent.com/dmz006/datawatch/main/install/install.sh | bash
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

### Install datawatch

```powershell
# Download binary
$url = "https://github.com/dmz006/datawatch/releases/latest/download/datawatch-windows-amd64.exe"
Invoke-WebRequest -Uri $url -OutFile "$env:LOCALAPPDATA\datawatch\datawatch.exe"
# Add $env:LOCALAPPDATA\datawatch to PATH
```

### Run as a Windows Service

Using NSSM (Non-Sucking Service Manager):
```powershell
winget install NSSM.NSSM

nssm install Datawatch "C:\Users\YOU\AppData\Local\datawatch\datawatch.exe"
nssm set Datawatch AppParameters "start"
nssm set Datawatch AppDirectory "C:\Users\YOU"
nssm set Datawatch DisplayName "Datawatch"
nssm set Datawatch Description "Signal to Claude Code Bridge"
nssm set Datawatch Start SERVICE_AUTO_START
nssm start Datawatch
```

View logs:
```powershell
nssm edit Datawatch
# or
Get-EventLog -LogName Application -Source Datawatch -Newest 50
```
