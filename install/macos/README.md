# macOS Installation

## Prerequisites

```bash
# Install Homebrew if not present
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"

# Install Java 17
brew install openjdk@17
echo 'export PATH="/opt/homebrew/opt/openjdk@17/bin:$PATH"' >> ~/.zshrc
source ~/.zshrc

# Install tmux
brew install tmux

# Install Git (usually pre-installed)
brew install git
```

## Install signal-cli

```bash
# Download latest signal-cli
SIGNAL_CLI_VERSION="0.13.4"
curl -fsSL -o /tmp/signal-cli.tar.gz \
  "https://github.com/AsamK/signal-cli/releases/download/v${SIGNAL_CLI_VERSION}/signal-cli-${SIGNAL_CLI_VERSION}.tar.gz"

tar -xzf /tmp/signal-cli.tar.gz -C ~/.local/opt/ 2>/dev/null || \
  (mkdir -p ~/.local/opt && tar -xzf /tmp/signal-cli.tar.gz -C ~/.local/opt/)

mkdir -p ~/.local/bin
ln -sf ~/.local/opt/signal-cli-${SIGNAL_CLI_VERSION}/bin/signal-cli ~/.local/bin/signal-cli

echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc
source ~/.zshrc

signal-cli --version
```

## Install datawatch

### From source (recommended)
```bash
# Requires Go 1.22+
brew install go
go install github.com/dmz006/datawatch/cmd/datawatch@latest
```

### Download binary
```bash
# Apple Silicon (M1/M2/M3)
curl -fsSL -o ~/.local/bin/datawatch \
  "https://github.com/dmz006/datawatch/releases/latest/download/datawatch-darwin-arm64"
chmod +x ~/.local/bin/datawatch

# Intel Mac
curl -fsSL -o ~/.local/bin/datawatch \
  "https://github.com/dmz006/datawatch/releases/latest/download/datawatch-darwin-amd64"
chmod +x ~/.local/bin/datawatch
```

## Setup

```bash
# Link Signal account (scan QR with Signal app)
datawatch link

# Configure
datawatch config init

# Test run
datawatch start
```

## Run as a macOS Service (LaunchAgent)

Create `~/Library/LaunchAgents/com.dmz006.datawatch.plist`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>com.dmz006.datawatch</string>
  <key>ProgramArguments</key>
  <array>
    <string>/Users/YOUR_USERNAME/.local/bin/datawatch</string>
    <string>start</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>/tmp/datawatch.log</string>
  <key>StandardErrorPath</key>
  <string>/tmp/datawatch-error.log</string>
  <key>EnvironmentVariables</key>
  <dict>
    <key>HOME</key>
    <string>/Users/YOUR_USERNAME</string>
    <key>PATH</key>
    <string>/Users/YOUR_USERNAME/.local/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin</string>
  </dict>
</dict>
</plist>
```

Replace `YOUR_USERNAME` with your actual username (`echo $USER`).

```bash
# Load the service
launchctl load ~/Library/LaunchAgents/com.dmz006.datawatch.plist

# Start it
launchctl start com.dmz006.datawatch

# View logs
tail -f /tmp/datawatch.log

# Stop
launchctl stop com.dmz006.datawatch

# Unload (disable autostart)
launchctl unload ~/Library/LaunchAgents/com.dmz006.datawatch.plist
```
