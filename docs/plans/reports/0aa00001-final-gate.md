# 0aa00001 final gate

Overall: FAIL (1 of 14 checks failed; no files were fixed).

## Commands
| Check | Result |
|---|---|
| go build ./... | pass |
| go vet (8 packages) | pass |
| go test (8 packages) | pass (8 of 8 ok) |
| node --test (1 web test file) | pass |
| make lint | FAIL: 13 issues (10 errcheck, 3 staticcheck) |
| check-no-internal-refs.sh | pass |
| check-docs-sync.sh | pass |

## Read-through confirmations
| Item | Result |
|---|---|
| Fake diff builds a record | pass |
| Five templates render | pass (5 of 5) |
| Gate report holds counts only | pass |
| Planner drop removes gate-report, changelog, parity, tracker tasks with a reason | pass |
| Planner keeps the task when the artefact is switched off | pass |
| Combined documentation lint fires | pass |
| Edited generated file is not overwritten | pass |
| Review step lists generated artefacts | pass |
| Six surfaces and five locale files carry the settings | pass (6 of 6, 5 of 5) |
