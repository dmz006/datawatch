# Start/Stop audit of the autonomous package

Each line names a start/stop pair, the file, whether Stop waits for its loop to exit, and the loop. Audit only; no code was changed.

```
pair=Coordinator.Stop file=internal/autonomous/automation_coordinator.go waits=yes loop=idleLoop (per-tick idle/budget/triage/question/CI-watch passes) + laneFillLoop (per-poll-second lane fill passes) note=Stop closes both stop channels and blocks on both idleDone and laneFillDone before returning — the earlier fix task has already landed, so the idle loop is awaited today, not just the lane fill loop.
pair=Manager.Stop file=internal/autonomous/manager.go waits=no loop=go m.run(m.ctx) main background run loop, plus go r.Run(m.ctx) hourly progress-snapshot runner note=Stop only calls m.cancel() and returns (comment says "does not block"); the run() goroutine and the snapshot runner are not waited on.
pair=ProgressSnapshotRunner.Run file=internal/autonomous/progress_snapshot_job.go waits=no loop=first EnsureDaily after Delay, then re-check every interval until ctx done note=no Stop method of its own: spawned from Manager.Start, ctx-cancelled by Manager.Stop, never joined.
pair=LifecycleSubscriptionEngine.Close file=internal/autonomous/lifecycle_session_push.go waits=no loop=janitor pruning expired subscriptions on a fixed cadence note=Close closes e.stop so the janitor exits, but there is no done channel or waitgroup, so Close can return while the janitor is still unwinding.
pair=CommFanoutSink.Close file=internal/autonomous/comm_fanout.go waits=no loop=waitAndFlush quiet-window flush waiters, one goroutine per lifecycle event bucket note=Close wakes waiters via close(c.stop) and flushes buckets inline, but does not wait for in-flight waitAndFlush goroutines (no waitgroup).
pair=Coordinator.laneFillDecompose file=internal/autonomous/lane_fill_coordinator.go waits=no loop=one background Decompose call per lane fill decision note=only tracked in c.lf.wg for the laneFillWait test helper; neither laneFillLoop stop nor Manager.Stop waits for in-flight decomposes.
pair=Manager.startQuestionResume file=internal/autonomous/question_tick.go waits=no loop=one resume goroutine per parked question task note=guarded one-at-a-time per task and tracked by questionTick.wg, but waitQuestionResumes is test-only; no shutdown path blocks on it.
pair=Manager.callAssistantModel file=internal/autonomous/decision_assistant_engine.go waits=no loop=one model call in a goroutine under per-call timeout note=calls return at timeout via select; a slow model goroutine may outlive the call (bounded by the request context, not by the return).
pair=Manager.ciWatchPending file=internal/autonomous/ci_watch.go waits=no loop=one post-timeout CI follow-up check (sleep then re-list runs, record result) note=fire-and-forget on context.Background with a follow-up budget; nothing waits for its completion at any Stop.
```
