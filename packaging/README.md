# Package Builds

This directory contains packaging configurations for common Linux distributions.

| Format | Directory | Distros |
|--------|-----------|---------|
| Debian | `debian/` | Ubuntu, Debian, Linux Mint, Pop!_OS |
| RPM | `rpm/` | RHEL, CentOS Stream, Fedora, AlmaLinux, Rocky Linux |
| Arch | `arch/` | Arch Linux, Manjaro, EndeavourOS |

## Quick Build with fpm

The simplest cross-distribution packaging uses [fpm](https://fpm.readthedocs.io/):

```bash
gem install fpm
make build

# .deb (Debian/Ubuntu)
fpm -s dir -t deb -n claude-signal -v 0.1.0 \
  --depends "default-jre-headless" --depends "tmux" \
  --deb-systemd install/systemd/claude-signal.service \
  bin/claude-signal=/usr/bin/claude-signal

# .rpm (RHEL/Fedora)
fpm -s dir -t rpm -n claude-signal -v 0.1.0 \
  --depends "java-17-openjdk-headless" --depends "tmux" \
  bin/claude-signal=/usr/bin/claude-signal

# .pkg.tar.zst (Arch)
fpm -s dir -t pacman -n claude-signal -v 0.1.0 \
  bin/claude-signal=/usr/bin/claude-signal
```

See each subdirectory's BUILD.md for native packaging instructions.
