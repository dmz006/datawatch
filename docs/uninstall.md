# Manual Uninstall

This guide covers removing datawatch for every supported installation method.

---

## Quick Uninstall (automated)

If you still have the uninstall script:

```bash
# Remove binaries and services (keeps config and data)
~/.local/bin/datawatch uninstall
# or
bash /path/to/datawatch/install/uninstall.sh

# Remove everything including config, sessions, and logs
bash /path/to/datawatch/install/uninstall.sh --purge
```

If the script is not available, use the manual steps below for your installation method.

---

## Method 1: Linux one-liner install (user install, no root)

This is the default when you ran:
```bash
curl -fsSL .../install/install.sh | bash
```

**Step 1: Stop the daemon**

```bash
# If running as a user systemd service
systemctl --user stop datawatch
systemctl --user disable datawatch
rm -f ~/.config/systemd/user/datawatch.service
systemctl --user daemon-reload

# If running in tmux
tmux kill-session -t datawatch 2>/dev/null || true

# If running as a background process
pkill -f "datawatch start" 2>/dev/null || true
```

**Step 2: Remove the binary**

```bash
rm -f ~/.local/bin/datawatch
rm -f ~/.local/bin/signal-cli   # if installed by datawatch installer
```

**Step 3: Remove signal-cli** (if installed by datawatch)

```bash
rm -rf ~/.local/opt/signal-cli-*
```

**Step 4: Remove data (optional — skip to keep sessions and config)**

```bash
rm -rf ~/.datawatch
rm -rf ~/.local/share/signal-cli   # removes all Signal account data and keys
```

---

## Method 2: Linux root install (`--root` flag)

This applies when you ran:
```bash
curl -fsSL .../install/install.sh | bash -s -- --root
```

**Step 1: Stop and disable the system service**

```bash
sudo systemctl stop datawatch
sudo systemctl disable datawatch
sudo rm -f /etc/systemd/system/datawatch.service
sudo systemctl daemon-reload
```

**Step 2: Remove binaries**

```bash
sudo rm -f /usr/local/bin/datawatch
sudo rm -f /usr/local/bin/signal-cli   # if installed by datawatch installer
```

**Step 3: Remove signal-cli** (if installed by datawatch to /opt)

```bash
sudo rm -rf /opt/signal-cli-*
```

**Step 4: Remove system user** (if created by installer)

```bash
sudo userdel datawatch 2>/dev/null || true
```

**Step 5: Remove data (optional)**

```bash
sudo rm -rf /etc/datawatch
sudo rm -rf /var/lib/datawatch
sudo rm -rf /var/log/datawatch
```

---

## Method 3: Installed from source (`go install`)

This applies when you ran:
```bash
go install github.com/dmz006/datawatch/cmd/datawatch@latest
```

**Step 1: Stop the daemon** (same as Method 1, Step 1)

**Step 2: Remove the binary**

```bash
rm -f "$(go env GOPATH)/bin/datawatch"
# or
rm -f ~/go/bin/datawatch
```

**Step 3: Remove data (optional)**

```bash
rm -rf ~/.datawatch
rm -rf ~/.local/share/signal-cli
```

---

## Method 4: Built from source (`make install` or `go build`)

**Step 1: Stop the daemon** (same as Method 1, Step 1)

**Step 2: Remove the installed binary**

```bash
# If installed to /usr/local/bin
sudo rm -f /usr/local/bin/datawatch

# If installed to ~/.local/bin
rm -f ~/.local/bin/datawatch

# If running from a local build directory, nothing to remove from system paths
```

**Step 3: Remove the source directory**

```bash
rm -rf /path/to/datawatch   # wherever you cloned it
```

**Step 4: Remove data (optional)**

```bash
rm -rf ~/.datawatch
rm -rf ~/.local/share/signal-cli
```

---

## Method 5: macOS (Homebrew / manual)

See [install/macos/README.md](../install/macos/README.md) for the macOS-specific install steps.

**Step 1: Stop the daemon**

```bash
# If running as a LaunchAgent
launchctl stop com.dmz006.datawatch
launchctl unload ~/Library/LaunchAgents/com.dmz006.datawatch.plist
rm -f ~/Library/LaunchAgents/com.dmz006.datawatch.plist

# If running in tmux
tmux kill-session -t datawatch 2>/dev/null || true

# If running as a background process
pkill -f "datawatch start" 2>/dev/null || true
```

**Step 2: Remove the binary**

```bash
rm -f ~/.local/bin/datawatch

# If installed with go install
rm -f ~/go/bin/datawatch

# If installed system-wide
sudo rm -f /usr/local/bin/datawatch
```

**Step 3: Remove signal-cli**

```bash
rm -f ~/.local/bin/signal-cli
rm -rf ~/.local/opt/signal-cli-*
```

**Step 4: Remove data (optional)**

```bash
rm -rf ~/.datawatch
rm -rf ~/.local/share/signal-cli
```

---

## Method 6: Windows (WSL2)

If you used WSL2 (recommended), open your WSL2 terminal and follow the Linux user
install steps (Method 1) inside the WSL2 environment.

## Method 7: Windows Native

**Step 1: Stop the service**

```powershell
# If installed as a service via NSSM
nssm stop Datawatch
nssm remove Datawatch confirm

# If running as a background process
Stop-Process -Name "datawatch" -Force -ErrorAction SilentlyContinue
```

**Step 2: Remove the binary**

```powershell
Remove-Item "$env:LOCALAPPDATA\datawatch\datawatch.exe" -Force
# Remove the directory if it only contained datawatch
Remove-Item "$env:LOCALAPPDATA\datawatch" -Recurse -Force
```

**Step 3: Remove from PATH**

Remove `%LOCALAPPDATA%\datawatch` from your user PATH:
- System Properties → Environment Variables → User Variables → Path → Edit

**Step 4: Remove data (optional)**

```powershell
Remove-Item "$env:USERPROFILE\.datawatch" -Recurse -Force
```

---

## Removing Signal account data

> **Warning:** Removing signal-cli data unlinks your Signal account from this device.
> You will need to re-link if you reinstall.

```bash
# Remove all signal-cli account data (keys, messages, etc.)
rm -rf ~/.local/share/signal-cli

# Optionally, in the Signal app on your phone:
# Settings → Account → Linked Devices → remove the datawatch device
```

---

## What each path contains

| Path | Contents | Safe to delete? |
|---|---|---|
| `~/.local/bin/datawatch` | Binary | Yes |
| `/usr/local/bin/datawatch` | Binary (root install) | Yes |
| `~/.datawatch/config.yaml` | Configuration | Yes (loses config) |
| `~/.datawatch/sessions.json` | Session history | Yes |
| `~/.datawatch/logs/` | Session output logs | Yes |
| `~/.datawatch/tls/` | Auto-generated TLS certs | Yes |
| `~/.local/share/signal-cli/` | Signal keys and account | Yes (unlinks Signal device) |
| `~/.config/systemd/user/datawatch.service` | User systemd service | Yes |
| `/etc/systemd/system/datawatch.service` | System systemd service | Yes |
| `/etc/datawatch/` | Root install config | Yes |
| `/var/lib/datawatch/` | Root install data | Yes |
| `/var/log/datawatch/` | Root install logs | Yes |
