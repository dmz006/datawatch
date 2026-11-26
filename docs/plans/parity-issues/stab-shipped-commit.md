# Mobile parity draft: checks on a merge_step shipped record

No new screen, setting or field. If the mobile app can record shipped state
with source `merge_step`, it must show the daemon's refusal text: the commit is
not a commit id, does not contain the Automaton's branch, or is not on the
remote. An abbreviated id (7 or more hex characters) is accepted and the full
id comes back in `merge_commit`.
