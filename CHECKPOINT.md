# CHECKPOINT — managed-service opt-in + Registry supervision
- Timestamp: 2026-10-11 18:21
- Done: Config.Managed (plugins + config + main.go passthrough + docs row), Registry SetManaged/StartManaged/StopAll/ServiceStatus/SetServiceEventFn (registry_services.go), Discover/SetEnabled reconcile without orphaning, api.go `service` status object, tests (8 new, real svchelper), gofmt/vet/gosec checked, committed
- Now: complete
- Remaining: daemon wiring (call StartManaged/StopAll + SetServiceEventFn in main.go) and status surface — later phase
