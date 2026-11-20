### Security

- The agent-base image no longer fails its blocking scan. The GitHub CLI inside it is rebuilt from source, and it was built with Go 1.27.3, whose standard library carries a HIGH vulnerability (CVE-2026-97054, the HTTP/1 request body limit). It is now built with Go 1.27.4, which has the fix. The Go toolchain floor check now rejects 1.27.3 and 1.26.10 as well, so a repository pin cannot slip back.
