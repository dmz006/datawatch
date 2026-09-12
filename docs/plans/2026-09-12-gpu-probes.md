# Plan: GPU Observer Probes for Shape B (v8.25.3)

**Status:** Completed 2026-09-12
**Release:** v8.25.3

## Problem

The `datawatch-stats` Shape B observer running on the NVIDIA Thor/GB10 SoC (compute node `datawatch`)
had no GPU visibility. `snap.GPU` was always empty. DCGM (Shape C) is not available on bare-metal
Tegra devices. The datawatch primary could not report GPU temperature, power draw, or memory usage
for the compute node.

## Approach

Added two probes to `internal/observer/`:

1. **`gpu_tegrastats.go`** — Runs `tegrastats --interval 500` in a background goroutine, takes the
   first parseable line with a 4s context timeout. Handles two output formats:
   - Classic Jetson: `GR3D_FREQ X%@Y GPU@T.TC RAM X/YMB`
   - NVIDIA Thor/GB10 SoC: timestamp prefix, no `GR3D_FREQ`, lowercase `gpu@T.TC`, `VDD_GPU XmW/YmW`

2. **`gpu_smi.go`** — Polls `nvidia-smi --query-gpu=index,name,utilization.gpu,memory.total,memory.used,temperature.gpu --format=csv,noheader,nounits` every 5s. Converts `[N/A]` fields to 0 (Tegra unified memory has no discrete GPU memory).

Wired via `Collector.SetGPUFn` (new pluggable hook added to `internal/observer/collector.go`).
`cmd/datawatch-stats/main.go` tries tegrastats first, falls back to nvidia-smi.

## Files Changed

- `internal/observer/gpu_tegrastats.go` (new)
- `internal/observer/gpu_smi.go` (new)
- `internal/observer/collector.go` — added `gpuFn` field and `SetGPUFn` method; wired into `collect()`
- `cmd/datawatch-stats/main.go` — probe selection logic (tegrastats > nvidia-smi > none)

## Discovery: NVIDIA Thor Format

Initial implementation used `GR3D_FREQ` as the validity signal and `GPU@` (uppercase). Live testing
on Thor revealed:
- No `GR3D_FREQ` field (unified memory architecture, no discrete GPU util%)
- Lowercase `gpu@35.25C` (not `GPU@35.25C`)
- Power via `VDD_GPU 2376mW/2376mW` (not present on classic Jetson)
- Timestamp prefix: `09-12-2026 18:08:03 RAM 69178/125772MB ...`

Fixed by: removing GR3D_FREQ as validity requirement, using `(?i)gpu@` (case-insensitive), adding
`VDD_GPU` power regex.

## Live Validation

```json
{
  "gpu": [{
    "name": "Tegra GPU",
    "vendor": "nvidia",
    "util_pct": 0,
    "mem_used_bytes": 72524759040,
    "mem_total_bytes": 131881500672,
    "power_w": 2.376,
    "temp_c": 35.187
  }]
}
```

Confirmed via `compute_node_detail("datawatch")` MCP tool, 2026-09-12.

## Remaining Gap

Unit tests for `parseTegraStatsLine` (both formats) and `parseSMIOutput` are not yet written.
See `docs/testing-tracker.md` §v8.25.3 for the full test matrix.
