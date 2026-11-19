### Fixed

- The verification-script pre-flight no longer panics on a shell `||`. It read an empty segment and aborted the whole daemon whenever a decompose ran it (a crash loop on 2026-11-18). `||` is now treated as the logical or, not a pipe, and empty segments are skipped.
