# Building the Debian Package

## Requirements
```bash
apt install build-essential devscripts debhelper golang-go
```

## Build
```bash
cd /path/to/claude-signal
dpkg-buildpackage -us -uc -b

# The .deb will be in the parent directory
ls ../*.deb

# Install
sudo dpkg -i ../claude-signal_0.1.0-1_amd64.deb
```

## Or use the simple approach with fpm:
```bash
gem install fpm
make build
fpm -s dir -t deb -n claude-signal -v 0.1.0 \
  --description "Signal to Claude Code bridge" \
  --depends "default-jre-headless" \
  --depends "tmux" \
  --deb-systemd install/systemd/claude-signal.service \
  bin/claude-signal=/usr/bin/claude-signal
```
