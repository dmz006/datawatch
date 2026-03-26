# Building the RPM Package

## Requirements

```bash
# RHEL/CentOS/Fedora
sudo dnf install rpm-build golang java-17-openjdk-devel tmux

# Set up rpmbuild tree
mkdir -p ~/rpmbuild/{BUILD,RPMS,SOURCES,SPECS,SRPMS}
```

## Build

```bash
# Copy spec file
cp packaging/rpm/claude-signal.spec ~/rpmbuild/SPECS/

# Download source tarball
spectool -g -R ~/rpmbuild/SPECS/claude-signal.spec

# Build
rpmbuild -ba ~/rpmbuild/SPECS/claude-signal.spec

# Find output
ls ~/rpmbuild/RPMS/x86_64/

# Install
sudo rpm -ivh ~/rpmbuild/RPMS/x86_64/claude-signal-0.1.0-1.*.rpm
```

## Or with fpm:

```bash
gem install fpm
make build
fpm -s dir -t rpm -n claude-signal -v 0.1.0 \
  --depends "java-17-openjdk-headless" \
  --depends "tmux" \
  --rpm-service install/systemd/claude-signal.service \
  bin/claude-signal=/usr/bin/claude-signal
```
