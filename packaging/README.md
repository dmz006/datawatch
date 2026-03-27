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
fpm -s dir -t deb -n datawatch -v 0.1.0 \
  --depends "default-jre-headless" --depends "tmux" \
  --deb-systemd install/systemd/datawatch.service \
  bin/datawatch=/usr/bin/datawatch

# .rpm (RHEL/Fedora)
fpm -s dir -t rpm -n datawatch -v 0.1.0 \
  --depends "java-17-openjdk-headless" --depends "tmux" \
  bin/datawatch=/usr/bin/datawatch

# .pkg.tar.zst (Arch)
fpm -s dir -t pacman -n datawatch -v 0.1.0 \
  bin/datawatch=/usr/bin/datawatch
```

See each subdirectory's BUILD.md for native packaging instructions.
