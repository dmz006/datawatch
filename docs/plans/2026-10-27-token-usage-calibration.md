# Claude token usage calibrated against the usage readings (2026-10-27)

**Purpose, operator, 2026-10-27, quoted: "The token usage isn't for api cost calculation but to normalize the % we've been calculating so we can normalize and track pool better, not cost or api usage."** Tokens are a normalizing unit for the weekly allowance percentage. The dollar columns below are only a weighting assumption used to combine token types; the normalized unit in the next section replaces them.

Operator, 2026-10-27, quoted: "all sessions should have token details, make sure we have tokens for the last week or more of work, that is the reason i stressed the calculator, tokens tie it all together".

## Where the tokens come from

Datawatch's per-session counters (`tokens_in`, `tokens_out`) are lifetime totals and leave out cache reads and cache writes, so they cannot give a per-window figure. The complete record is the Claude transcript files under `~/.claude/projects/<project>/<session>.jsonl`: every assistant message carries `message.usage` (input, output, cache-creation and cache-read tokens), `message.model` and a timestamp. Each message id is counted once. Since the 2026-10-25 08:00 reset there are 12,297 messages (the last 9 days hold 114,216 messages in 1,884 transcript files).

Cost-equivalent weighting used, dollars per million tokens (input, output, cache write, cache read): Sonnet 3, 15, 3.75, 0.30; Haiku 1, 5, 1.25, 0.10; Opus 15, 75, 18.75, 1.50. These are API list-price ratios and are an assumption about how the plan meter weights usage; the calibration below tests it.

## Calibration against the operator's readings (times in UTC, hour resolution)

| Window | Cost-equivalent $ | Points | $ per point |
|---|---|---|---|
| Sun 08:00 reset to Sun 19:45 EDT | 286.0 | +15 | 19.1 |
| to Mon 06:25 | 189.9 | +7 | 27.1 |
| to Mon 13:35 | 41.9 | +2 | 21.0 |
| to Mon 19:30 | 56.9 | +2 | 28.5 |
| to Tue 04:00 | 33.5 | +1 | 33.5 |
| to Tue 07:40 | 37.7 | +3 | 12.6 |
| to Tue 10:55 | 50.1 | +3 | 16.7 |
| Total | 696 | 33 | 21.1 |

Output tokens alone are a much worse fit (0.12 to 0.52 million output tokens per point); the cost-weighted figure, which includes the cache reads that dominate, stays within a factor of about 2.5 even though the readings are whole percentages (a 1 to 3 point window carries up to 50 percent rounding error). Working figure: **one point is about $21 of cost-equivalent usage, so 100 percent is about $2,100 a week**. More readings will tighten it.

## Who used it (cost-equivalent $, since the reset, $721 in total)

| Consumer | $ | Share |
|---|---|---|
| The supervisor session itself (transcript 82c25a3a, project workspace/datawatch) | 235 | 33 percent |
| PRD ead20001 | 68 | 9 |
| PRD 3d9f0001 | 39 | 5 |
| PRD 12520001 | 35 | 5 |
| PRD 07630001 (the calculator) | 33 | 5 |
| every other PRD worker, decomposer and verifier | about 310 | 43 |

The supervisor session is the largest single consumer, larger than any PRD. Most of the workers are Haiku verifiers and decomposers.

## Forecast on this calibration

33 points used, 2.15 days into the week, the week average burn is 15.4 points a day; to the Sunday 2026-11-01 08:00 reset that lands near 108 percent at the average rate. Recent windows run from 5 to 22 points a day. Staying within budget needs a burn at or below about 13.7 points a day, or about $290 a day.

## Method (rerun any time)

Walk `~/.claude/projects/**/*.jsonl`, keep type assistant messages with `message.usage`, de-duplicate by (message id, request id), bucket by timestamp hour, weight per model with the rates above, and compare window sums with the readings. The supervisor watch also snapshots the datawatch counters each run into `/tmp/sv/usage-ledger.jsonl` as a cross-check. The BL472 calculator should read transcripts (or an equivalent per-message usage feed) instead of the lifetime counters.

## Normalized unit (fitted, replaces the dollar weighting)

Windows are the seven intervals between the operator's readings. For each candidate weighting the units per percentage point were computed per window; a good normalization gives nearly the same figure in every window (low spread, shown as the coefficient of variation).

| Weighting | Spread | Units per point |
|---|---|---|
| output tokens only | 0.47 | 345 thousand |
| all four token types equal | 0.34 | 70 million |
| output plus cache writes equal | 0.44 | 2.2 million |
| list-price ratios (Sonnet and Haiku) | 0.30 | 22.6 million |
| best grid fit | 0.25 | 1.12 million output-equivalent tokens |

Best fit: **one output token = 1 unit; one cache-write token = 0.5 unit; one cache-read token = 0.01 unit; input tokens are negligible; Haiku tokens count 0.2 of the same Sonnet token.** On that unit one percentage point is about **1.1 million units**, so the weekly allowance is about 112 million units. Caveats: only seven windows and three fitted weights (overfit risk), whole-percent readings carry up to 50 percent rounding error on a 1 to 3 point window, no Opus use appears in the data, and the weights should be refit as readings accumulate (the grid and the windows are in the method below). The BL472 calculator should store the unit definition as data so it can be refit.
