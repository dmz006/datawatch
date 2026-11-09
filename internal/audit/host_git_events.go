// Git host audit action (AGENT.md's Audit Logging Rule).
//
// Every host-facing git operation the daemon performs through the host facade
// (merge/change request, issue, release, tag) produces one ActionHostGitOperation
// entry. The entry carries the host name, host kind, operation, repo, ref and
// credential source only; the token has no path into it.

package audit

// ActionHostGitOperation — a git host operation (change request, issue,
// release or tag) completed. Details: host, kind, operation (mr, issue,
// release or tag), repo, ref, credential_source.
const ActionHostGitOperation = "host_git_operation"
