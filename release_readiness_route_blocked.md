# Blocked: release-readiness route

Task: GET /api/autonomous/release-readiness (handler, route, openapi, tests).

Blocked because `ReleaseReadinessRecordsFromComparisons` does not exist in this
tree. The previous task (see release_readiness_source_blocked.md) stopped as
blocked: the shadow merge comparison store lives only on the unmerged branch
`automaton/d2f10001`, so there is no comparison source to read from.
`ReleaseReadiness` and `ReleaseReadinessBar` also have no JSON tags yet.

Nothing was invented. To unblock: land d2f10001, then re-run the adapter task,
then this one. The handler shape is simple once the adapter exists: validate
`size`, build records, call ComputeReleaseReadiness with DefaultReleaseReadinessBar,
return the structure plus the bar values; 400 JSON error naming the size on a bad one.
