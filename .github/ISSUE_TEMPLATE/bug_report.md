---
name: Bug report
about: Report a bug in datawatch
title: '[BUG] '
labels: bug
assignees: ''
---

**Describe the bug**
A clear description of what the bug is.

**To Reproduce**
Steps to reproduce:
1. ...
2. ...

**Expected behavior**
What you expected to happen.

**Environment**
- OS and version:
- datawatch version (`datawatch version`):
- signal-cli version (`signal-cli --version`):
- Go version (`go version`):
- tmux version (`tmux -V`):
- Install method: source / binary / deb / rpm / arch

**Logs**
```
paste relevant logs here
journalctl -u datawatch --since "10 minutes ago"
```

**Config** (redact account_number and group_id)
```yaml
paste config.yaml here (remove sensitive values)
```
