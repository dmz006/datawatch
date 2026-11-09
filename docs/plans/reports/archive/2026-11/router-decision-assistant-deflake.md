## Reproduction

command: GOMAXPROCS=1 timeout 480 go test -race -count=60 -v -run '^TestAutomationCommDecisionAssistantUsage$' ./internal/router/
busy loops: 48, GOMAXPROCS=1, runs: 60, race: yes
failures: 0 of 60
data race reports: 0
stray request: not observed
cause class: not-determined
notes: A second run with the fake daemon handler temporarily logging method, path and sequence number completed 175 more runs (cut off by the 420 s cap) with 0 failures, 0 race reports and no request logged at all.
notes: The "clear=maybe reached the daemon" failure was not reproduced, so no stray request was seen and nothing here shows a late recording or a background goroutine.
notes: Instrumentation was reverted with git checkout and git status was clean; all 48 busy loops were confirmed gone by PID.

## Fix

fix: the fake daemon's log now counts requests that reached its handler and reset() waits until each is recorded, so the fixed 200 ms sleep no longer alone separates one input's window from the next.
cause class: not-determined
notes: No stray request was observed, so this closes the fixture-side window (a request still being handled when the next input resets the log). The router's own HandleTestMessage is unchanged, and no production file or version was touched.

## Regression test

test: TestAutomationCommFixtureDrainsLateRequests
fails without the fix: yes
notes: A request held in the fake daemon handler for 300 ms (longer than HandleTestMessage's 200 ms wait) is in flight when the next input starts. With the controlLog.reset wait removed the late request lands in the next window and the test fails; with the guard restored it passes (-race -count=3). The fixture file diff is empty.

## Load run

command: env -u TMUX -u TMUX_TMPDIR GOMAXPROCS=1 timeout 480 go test -race -count=60 -v -run '^TestAutomationCommDecisionAssistantUsage$' ./internal/router/
busy loops: 48, GOMAXPROCS=1, runs: 60, race: yes
failures: 0 of 60
data race reports: 0
notes: Run on the fixed code with one busy loop per CPU started by PID and killed by a trap on exactly those PIDs; the go test run took 133.7 s and exited 0. All 48 busy loop PIDs were confirmed gone afterwards.

## Unlocked readers

non-exhaustive survey: a best-effort list, not a complete audit. Survey grep: `grep -lE 'len\((calls|reqs|got|seen|bodies)\)|HandleTestMessage' internal/router/*_test.go` matched 77 files; entries below come from reading those files' fake-daemon handlers and are not guaranteed to cover every one. No code or test was changed and no test was run.

- internal/router/audit_query_filters_test.go:62 TestAuditQuery_CommFilters: handler sets captured query and hits++ with no lock; test reads *got and *hits after HandleTestMessage
- internal/router/automation_budget_cmd_test.go:60 TestAutomationSetBudgetComm: handler appends to captured calls unlocked; test does len(calls) and calls[0] after HandleTestMessage
- internal/router/automation_budget_cmd_test.go:134 TestAutomationSetBudgetCommRejectsBadInput: handler does calls++ unlocked; test compares calls to before after HandleTestMessage
- internal/router/automation_cmd_test.go:35 TestAutomationActivateHitsParent: handler does calls++ unlocked; test reads calls after HandleTestMessage
- internal/router/automation_control_cmd_test.go:67 TestAutomationCommControl: handler appends to captured calls unlocked; test reads len(calls) and calls[0] (also lines 94 and 113)
- internal/router/automation_forkjoin_cmd_test.go:40 TestAutomationCmdListAndGetVerbs: handler appends to captured calls unlocked; test joins calls after HandleTestMessage
- internal/router/autonomous_events_test.go:85 TestPendingEventsCommReadsOwnQueue: handler appends through *reqs unlocked; test reads len(reqs) and reqs[i]
- internal/router/autonomous_events_test.go:107 TestPendingEventsCommDefaultsToChannelQueue: handler appends through *reqs unlocked; test reads len(reqs) and reqs[1]
- internal/router/autonomous_events_test.go:121 TestPendingEventsCommCannotReadAnotherChannelsQueue: handler appends through *reqs unlocked; test ranges over reqs
- internal/router/autonomous_plan_review_test.go:62 TestAutonomousPlanReview_PostsPlanReviewEndpoint: handler appends through *reqs unlocked; test reads len(reqs) and reqs[0]
- internal/router/autonomous_plan_review_test.go:119 TestAutonomousConfigSet_PlanReviewAndSmokeKeys: handler appends through *reqs unlocked; test ranges over reqs
- internal/router/autonomous_plan_review_test.go:158 TestAutonomousResume_PostsResumeEndpoint: handler appends through *reqs unlocked; test reads len(reqs) and reqs[0]
- internal/router/autonomous_subscription_update_test.go:179 TestSubscriptionSurface_CommUpdateMergesAndPuts: handler appends to d.reqs unlocked; test reads len(d.reqs) and d.reqs[i]
- internal/router/autonomous_subscription_update_test.go:228 TestSubscriptionSurface_CommUpdateExpiresZeroClears: handler appends to d.reqs unlocked; test reads len(d.reqs) and d.reqs[1]
- internal/router/autonomous_subscription_update_test.go:254 TestSubscriptionSurface_CommUpdateOwnerScoping: handler appends to d.reqs unlocked; test ranges over d.reqs
- internal/router/autonomous_subscription_update_test.go:299 TestSubscriptionSurface_CommTestPostsAfterOwnerCheck: handler appends to d.reqs unlocked; test reads len(d.reqs) and d.reqs[i]
- internal/router/autonomous_subscription_update_test.go:335 TestSubscriptionSurface_CommTestOwnerScoping: handler appends to d.reqs unlocked; test ranges over d.reqs
- internal/router/autonomous_subscriptions_sink_test.go:102 TestCommSubscribeWebhookSendsSinkAndReference: handler appends through *reqs unlocked; test reads len(reqs) and reqs[0]
- internal/router/autonomous_subscriptions_sink_test.go:127 TestCommSubscribePeerSendsNoReference: handler appends through *reqs unlocked; test reads len(reqs) and reqs[0]
- internal/router/autonomous_subscriptions_sink_test.go:161 TestCommSubscribeRefusesBeforeSending: handler appends through *reqs unlocked; test reads len(reqs)
- internal/router/autonomous_subscriptions_sink_test.go:174 TestCommUpdateKeepsWebhookSinkAndReference: handler appends through *reqs unlocked; test reads len(reqs) and reqs[1]
- internal/router/autonomous_subscriptions_sink_test.go:192 TestCommUpdateSwitchesSinkAndDropsReference: handler appends through *reqs unlocked; test reads len(reqs) and reqs[1]
- internal/router/autonomous_subscriptions_sink_test.go:210 TestCommUpdateBackToCommPinsChannel: handler appends through *reqs unlocked; test reads len(reqs) and reqs[1]
- internal/router/autonomous_subscriptions_sink_test.go:242 TestCommUpdateSinkRulesRefuseBeforeSending: handler appends through *reqs unlocked; test ranges over reqs
- internal/router/autonomous_subscriptions_sink_test.go:260 TestCommUpdateRefusesAnotherChannelsWebhook: handler appends through *reqs unlocked; test ranges over reqs
- internal/router/autonomous_subscriptions_test.go:349 TestAutonomousSubs_SubscribeRoundTrip: handler appends through *reqs unlocked; test reads len(reqs) and reqs[0]
- internal/router/autonomous_subscriptions_test.go:412 TestAutonomousSubs_ListIsOwnerScoped: handler appends through *reqs unlocked; test reads len(reqs) and reqs[0]
- internal/router/autonomous_subscriptions_test.go:442 TestAutonomousSubs_ListEmptyForOtherChannel: handler appends through *reqs unlocked; test reads reqs[len(reqs)-1]
- internal/router/autonomous_subscriptions_test.go:458 TestAutonomousSubs_UnsubscribeOwnSubscription: handler appends through *reqs unlocked; test reads len(reqs) and reqs[0], reqs[1]
- internal/router/autonomous_subscriptions_test.go:481 TestAutonomousSubs_UnsubscribeRefusesForeignSubscription: handler appends through *reqs unlocked; test ranges over reqs and reads len(reqs)
- internal/router/autonomous_tags_registry_test.go:118 TestTagRegistryCommDispatch: handler appends to captured got unlocked; test reads len(got) and got[0]
- internal/router/autonomous_tags_surface_test.go:94 TestTagSurfaceDispatch: handler appends to captured got unlocked; test reads len(got) and got[0]
- internal/router/budget_test.go:140 TestBudgetCommRoundTrip: handler appends to statusQueries, llmBodies and llmMethods unlocked; test reads them after HandleTestMessage
- internal/router/failure_escalation_comm_test.go:101 TestFailureEscalationCommRefusesBadSettings: handler does rec.hits++ unlocked; test reads rec.hits (also line 149 in TestFailureEscalationCommReviewTaskRefusals)
- internal/router/held_questions_comm_test.go:136 TestQuestionHeldList: handler appends to h.reqs unlocked; test reads len(h.reqs) and h.reqs[0]
- internal/router/held_questions_comm_test.go:180 TestQuestionHeldFilters: handler appends to h.reqs unlocked; test reads len(h.reqs) and h.reqs[i]
- internal/router/held_questions_comm_test.go:253 TestQuestionHeldIsReadOnly: handler appends to h.reqs unlocked; test ranges over h.reqs
- internal/router/identity_test.go:46 TestIdentityComm_Backups: handler appends to captured seen unlocked; test reads len(*seen) and (*seen)[0]
- internal/router/identity_test.go:72 TestIdentityComm_Restore: handler appends to captured seen unlocked; test reads len(*seen) and (*seen)[0] (also lines 83, 103 for the no-call subtests and TestIdentityComm_ResetIsNotAChatVerb)
- internal/router/infra_retry_comm_test.go:282 TestInfraRetryComm_ConfigureRoundTripSendsKeysUnchanged: handler appends to bodies unlocked; test reads len(*bodies) and (*bodies)[before]
- internal/router/infra_retry_comm_test.go:291 TestInfraRetryComm_ConfigureRefusalIsShown: handler appends to bodies unlocked; test reads len(*bodies)
- internal/router/lane_fill_comm_test.go:91 TestLaneFillComm_BadLimitShowsUsageAndSendsNothing: handler does log.calls++ unlocked; test reads log.calls (also line 130 in TestLaneFillComm_UnknownPreferenceShowsUsageAndSendsNothing)
- internal/router/merge_pipeline_comm_test.go:124 TestMergePipelineComm_BadInputShowsUsageAndSendsNothing: handler does rec.calls++ unlocked; test reads rec.calls
- internal/router/peers_test.go:97 TestPeers_List_HitsParent: handler does calls++ unlocked; test reads calls after HandleTestMessage
- internal/router/peers_test.go:173 TestPeers_Delete_HitsParent: handler does calls++ unlocked; test reads calls after HandleTestMessage
- internal/router/plan_coverage_filters_comm_test.go:95 TestPlanCoverageFilters_CommHitsReadPath: handler appends through *reqs unlocked; test reads len(reqs) and reqs[0]
- internal/router/plan_coverage_filters_comm_test.go:120 TestPlanCoverageFilters_CommAliasReachesPath: handler appends through *reqs unlocked; test reads len(reqs) and reqs[0]
- internal/router/progress_kind_comm_test.go:36 TestProgressKindComm_BadKindShowsUsageAndSendsNothing: handler does calls++ unlocked; test reads calls
- internal/router/questions_clarity_test.go:65 TestQuestionQuality_Comm: handler appends through *paths unlocked; test reads len(paths) and paths[0]
- internal/router/questions_comm_test.go:267 TestQuestionList: handler appends to f.reqs unlocked; test reads len(f.reqs) and f.reqs[0]
- internal/router/questions_comm_test.go:302 TestQuestionShow: handler appends to f.reqs unlocked; test reads len(f.reqs) and f.reqs[0]; line 316 also appends to f.answers from the test goroutine while the handler reads it
- internal/router/questions_comm_test.go:342 TestQuestionAnswerByNumber: handler appends to f.reqs and f.answers unlocked; test reads both after HandleTestMessage
- internal/router/questions_comm_test.go:382 TestQuestionAnswerByOptionID: handler appends to f.answers unlocked; test reads len(f.answers) and f.answers[0]
- internal/router/questions_comm_test.go:401 TestQuestionAnswerQuoteVerbatim: handler appends to f.answers unlocked; test reads len(f.answers) and f.answers[0]
- internal/router/questions_comm_test.go:421 TestQuestionAnswerUnknownOption: handler appends to f.answers unlocked; test reads len(f.answers)
- internal/router/retry_profiles_test.go:62 TestRetry_ProfileList_HitsParent: handler does calls++ unlocked; test reads calls after HandleTestMessage
- internal/router/retry_profiles_test.go:130 TestRetry_ProfileDelete_HitsParent: handler does calls++ unlocked; test reads calls after HandleTestMessage
- internal/router/roles_test.go:40 TestRoleParityCommAddUpdateAndList: handler appends to bodies unlocked in two mux handlers; test reads len(bodies) and bodies[i]
- internal/router/scope_lint_comm_test.go:47 TestScopeLintComm_ConfigSetAcceptsEachMode: handler appends to d.puts unlocked; test reads len(d.puts) and d.puts[0]
- internal/router/scope_lint_comm_test.go:73 TestScopeLintComm_ConfigSetAcceptsConfigureSpelling: handler appends to d.puts unlocked; test reads len(d.puts) and d.puts[0]
- internal/router/scope_lint_comm_test.go:90 TestScopeLintComm_ConfigSetRejectsBadValue: handler appends to d.puts unlocked; test reads len(d.puts)
- internal/router/security_failure_comm_test.go:92 TestSecurityFailureCommListsOpenReports: handler writes calls.path under calls.mu but the test reads calls.path directly without the lock (counts() is locked)
- internal/router/shipped_comm_test.go:34 TestShippedComm_PrdAndAutomation: handler appends to captured calls unlocked; test reads len(calls) and calls[0], and resets calls = nil
- internal/router/shipped_comm_test.go:86 TestShippedComm_UsageWithoutNetwork: handler appends to captured calls unlocked; test reads len(calls)
- internal/router/shipped_comm_test.go:115 TestShippedComm_ShippedCounts: handler appends to captured calls unlocked; test reads len(calls) and calls[0]
- internal/router/shipped_comm_test.go:154 TestShippedComm_ListShippedSelection: handler appends to captured uris unlocked; test reads len(uris) and uris[i]
- internal/router/sx2_parity_shipped_backfill_test.go:33 TestShippedBackfillComm_DefaultWrites: handler appends to captured calls unlocked; test reads len(calls) and calls[0]
- internal/router/sx2_parity_shipped_backfill_test.go:62 TestShippedBackfillComm_DryRunAndRefusals: handler appends to captured calls unlocked; test reads len(calls) and calls[0], and resets calls = nil
- internal/router/sx2_parity_test.go:276 TestNotifyEvents_Set: handler appends through *reqs unlocked; test reads len(reqs) and reqs[0]
- internal/router/sx2_parity_test.go:309 TestNotifyEvents_SetWarningKinds: handler appends through *reqs unlocked; test reads len(reqs) and reqs[0]
- internal/router/usage_calculator_test.go:245 TestUsageCalcAdviceHandler: handler appends to recs unlocked; test reads len(*recs) and (*recs)[0]
- internal/router/usage_calculator_test.go:265 TestUsageCalcAdviceOverrides: handler appends to recs unlocked; test reads len(*recs) and (*recs)[0]
- internal/router/usage_calculator_test.go:292 TestUsageCalcReadingHandler: handler appends to recs unlocked; test reads len(*recs) and (*recs)[i]
- internal/router/usage_calculator_test.go:331 TestUsageCalcReadingLanesParsedNotSent: handler appends to recs unlocked; test reads len(*recs) and (*recs)[0]
- internal/router/usage_calculator_test.go:350 TestUsageCalcAdviceLanesWhatIfIncludingZero: handler appends to recs unlocked; test reads len(*recs) and (*recs)[0]
- internal/router/autonomous_create_project_dir_test.go:85 TestAutonomousCreate_PostsSpecOnly_NoProjectDir: handler writes cap.method, cap.path and cap.body unlocked (lines 32-34); test reads them at lines 85-91 after HandleTestMessage
- internal/router/autonomous_create_project_dir_test.go:110 TestPRDAlias_Create_PostsSpecOnly_NoProjectDir: handler writes cap.method, cap.path and cap.body unlocked (lines 32-34); test reads them at lines 110-116 after HandleTestMessage
- internal/router/failure_escalation_surfaces_test.go:128 TestFailureEscalationCommReviewRetryChangesWorkerAndScope: handler writes d.resolved unlocked (line 51); test reads d.resolved at lines 128-133 after HandleTestMessage
- internal/router/fingerprint_args_test.go:25 TestRouterFederationPeerFingerprintSync: handler writes method and path unlocked (line 18); test reads them after HandleTestMessage (same pattern at line 44 in the off subtest)
- internal/router/fingerprint_args_test.go:57 TestRouterFederationPeerFingerprintSync: handler does calls++ unlocked (line 54); test reads calls after HandleTestMessage
- internal/router/fingerprint_args_test.go:66 TestRouterLLMBillingGroupKey: method, path and body come from commLLMBody, whose handler writes them unlocked; test reads the returned values
- internal/router/fingerprint_cmd_test.go:53 TestRouterFingerprintList: matchReplier handler writes *call unlocked (line 25); test reads call after HandleTestMessage (same at line 63)
- internal/router/fingerprint_cmd_test.go:75 TestRouterFingerprintList: handler does count++ unlocked (line 72); test reads count after HandleTestMessage
- internal/router/fingerprint_cmd_test.go:88 TestRouterFingerprintConfirmReject: matchReplier handler writes *call unlocked; test reads call after HandleTestMessage (same at line 99)
- internal/router/fingerprint_cmd_test.go:117 TestRouterFingerprintUnknownID: handler writes call unlocked; test reads call after HandleTestMessage
- internal/router/guardrail_set_llm_test.go:25 TestCommSetLLM_LegacyUnchanged: commSetLLMBody handler writes path unlocked (line 18) and returns it after HandleTestMessage; every caller (lines 29-71) reads the unlocked value
- internal/router/plan_tier_comm_test.go:26 TestCommLLMUpdate_PlanTierAndMaxInflight: commLLMBody handler writes method and path unlocked (lines 18-19) and returns them after HandleTestMessage; every caller (lines 30, 46 and later) reads the unlocked values
- internal/router/usage_backoff_test.go:129 TestUsageBackoffComm_RoundTrip: handler appends to fake.methods and fake.bodies under f.mu, but the test reads len(fake.bodies), fake.methods and fake.bodies without taking f.mu (also lines 132, 146, 151, 159)
- internal/router/usage_backoff_test.go:234 TestUsageBackoffComm_Recheck: handler writes method and path unlocked (line 225); test reads them after HandleTestMessage
- internal/router/verifier_policy_comm_test.go:41 TestVerifierPolicyCommShowsGlobalPolicy: commVerifierPolicyCall handler writes got.method, got.path and got.body unlocked (lines 27-32); test reads them after HandleTestMessage (same in every TestVerifierPolicyComm* test, e.g. lines 55, 68, 76)
- internal/router/automation_import_graph_cmd_test.go:29 TestAutomationImportGraphComm: handler writes gotMethod, gotPath and gotBody unlocked (line 18); the test closure reads them after HandleTestMessage (also lines 39 and 53)
- internal/router/autonomous_tags_test.go:86 TestTagVerbDispatch: handler writes gotMethod, gotPath and gotBody unlocked (line 77); test reads them after HandleTestMessage
- internal/router/compute_tls_test.go:136 TestComputeNodeTLSAddAndUpdateSendKeys: handler writes d.method, d.path and d.body under d.mu but the test reads them without taking the lock (also line 139)
- internal/router/compute_tls_test.go:155 TestComputeNodeTLSSkipVerifyIsBoolInBody: handler writes d.body under d.mu but the test reads d.body without the lock
- internal/router/progress_cmd_test.go:102 TestProgressCommReplyIsTheRenderedText: fake daemon handler writes *query unlocked (line 40); test reads query after captureSends (also line 120 in TestProgressCommRejectsUnknownArgument and line 132 in TestProgressCommTrend)

## Final gate

- gofmt and vet: `gofmt -l` on the three changed Go files printed nothing; `go vet ./internal/router/` was clean: PASS
- golangci-lint: `golangci-lint run ./internal/router/` reported 0 issues: PASS
- race tests: `TestAutomationCommDecisionAssistant`, `TestAutomationCommDecisionAssistantUsage`, `TestAutomationCommDecisionAssistantVerbListed` and `TestAutomationCommFixtureDrainsLateRequests` all passed under `go test -race -count=1 -v`: PASS
- testing entry: bug entry added to `docs/testing.md` naming `TestAutomationCommDecisionAssistantUsage` and `TestAutomationCommFixtureDrainsLateRequests`, marked PASS: PASS
- result: PASS
