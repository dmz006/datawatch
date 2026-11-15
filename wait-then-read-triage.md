# Wait-then-read triage — lifecycle peer/forwarding tests

Task: find, in the four `internal/autonomous/` lifecycle test files, assertions on
`outcomes()`/`alertTexts()`/`hits.Load()`/receiver records that run immediately
after a wait that does not guarantee the record exists, and replace those waits
with explicit condition polls (like `peerDownWaitUntil`).

**Result: no real instances found. No test code was changed.**

Every async assertion in the four files is either synchronous, gated on a channel
receive, or preceded by a poll of the condition that implies the asserted record:

- `lifecycle_peer_down_test.go`
  - `TestPeerDownDoesNotBlockLocalDelivery / hanging and refusing peers while a request is in flight` — safe: waits on `hang.entered==1` and on the refused forward's drop alert; production order in `lifecycle_forward.go` is `attemptRecord` (audit) before `drop` (alert), so the alert implies the audits and the retry sleep it asserts. `requireLocal` and `queueEventIDs` assertions are behind the `requireLocal(3)` condition poll; the `exited==0` check is an absence bound (the hang peer holds the request until timeout or release).
  - `TestPeerDownDoesNotBlockLocalDelivery / a hanging peer is bounded by the timeout and the daemon keeps delivering` — safe: waits on `alertTexts()==1` / `==2`; the alert is raised after both timed-out attempts returned, which implies `hang.entered==2` and the alert text it reads.
  - `TestPeerDownDoesNotBlockLocalDelivery / a refusing peer is retried on the fake clock and dropped once` — safe: waits on the drop alert; with 3 attempts the alert implies 2 recorded fake-clock sleeps and the `[retry retry failed]` audit sequence.
  - `TestPeerDownRecovers / a forward retry succeeds when the peer returns during the wait` — already fixed in a prior pass: waits on `recv.hits==1`, then explicitly waits for the `[retry delivered]` outcome record before asserting it (the receiver records its hit before its response, so a hit does not imply the outcome exists yet).
  - `TestPeerDownRecovers / an event after a dropped forward reaches the peer once it is back` — safe: waits on the drop alert, then on `recv.hits==1`; `peerDownReceiver.ServeHTTP` appends the event id under its mutex before `hits.Add(1)`, so the hit implies the id list it asserts. `store.List` and `alertTexts` reads are mutex-guarded and no further forward can alert after a successful delivery.
  - `TestPeerDownRecovers / reading a peer resumes when it returns and local events flow throughout` — safe: every assertion is behind a `peerDownWaitUntil` on the exact condition it proves (`retries==1`, `pulled==1`, `builtin-alert==2`); the subscriber records retries, alert, accepted and publish sequentially in one goroutine, so publish implies all the audits and both backoff sleeps it checks.
- `lifecycle_forward_test.go` — safe: direct `Forward()` tests are fully synchronous (the attempt loop runs in the caller's goroutine; `fwdPeer` appends the record before answering). Engine tests poll explicit conditions (`waitFor` on `requests()==N`, `alertCount()==1`, `store.List("")==0`, `settingsCallCount()>=1`) before asserting.
- `lifecycle_forward_receive_test.go` — safe: delivery assertions are channel receives with a timeout; `ForwardedEventNeverForwardedOn` reads `calls.Load()` after the subscriber callback (which runs `Forward` synchronously) has delivered. The two `time.Sleep` calls (200 ms) precede *absence* assertions (no coordinator run, no extra forward) — opposite failure direction from the defect, and out of scope.
- `lifecycle_peer_subscription_test.go` — safe: all async subtests poll through `peerSubWaitFor` on the record count they then assert (`len(evs)`, `len(a)`, `requests()`, `sub.Running()`); the recorder appends under a mutex. `time.Sleep` at L137 is inside the injected `sleep` seam (test double), L702 is a 30 ms grace before an absence assertion.

Gates: `go test -race -count=10 -run 'TestPeerDown|TestForward|TestLifecyclePeer' ./internal/autonomous/` and `gofmt -l` on the package (no files changed).
