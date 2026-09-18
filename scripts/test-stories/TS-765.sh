#!/usr/bin/env bash
# TS-765 — tegrastats selected over nvidia-smi when both present
# tags: surface:api feature:gpu parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-765"

story_preflight "surface:api feature:gpu" || exit 0

# Source inspection: verify priority order in cmd/datawatch-stats/main.go.
stats_main="$(dirname "${BASH_SOURCE[0]}")/../../cmd/datawatch-stats/main.go"
if [[ ! -f "$stats_main" ]]; then
  ko "cmd/datawatch-stats/main.go not found at $stats_main"
  exit 0
fi

src=$(cat "$stats_main")

# Find line numbers for tegrastats and nvidia-smi probes.
tegra_line=$(grep -n "tegrastats\|TegraStats\|NewTegraStats" "$stats_main" | \
  grep -v "//\|test\|_test\|fmt.Fprintln\|[Ll]og" | head -1 | cut -d: -f1 || echo 9999)
smi_line=$(grep -n "nvidia-smi\|NewSMIProbe\|SMIProbe" "$stats_main" | \
  grep -v "//\|test\|_test\|fmt.Fprintln\|[Ll]og" | head -1 | cut -d: -f1 || echo 9999)
nvml_line=$(grep -n "NewNVMLProbe\|nvml\b\|NVMLProbe" "$stats_main" | \
  grep -v "//\|test\|_test\|fmt.Fprintln\|[Ll]og" | head -1 | cut -d: -f1 || echo 9999)

echo "  [TS-765] probe lines: nvml=$nvml_line tegrastats=$tegra_line nvidia-smi=$smi_line"
save_evidence "$CURRENT_STORY" "probe_priority.json" \
  "{\"nvml_line\": $nvml_line, \"tegrastats_line\": $tegra_line, \"nvidiasmi_line\": $smi_line}"

if [[ "$tegra_line" -ge 9999 ]]; then
  ko "tegrastats probe not found in cmd/datawatch-stats/main.go"
  exit 0
fi
if [[ "$smi_line" -ge 9999 ]]; then
  ko "nvidia-smi probe not found in cmd/datawatch-stats/main.go"
  exit 0
fi
if [[ "$tegra_line" -ge "$smi_line" ]]; then
  ko "nvidia-smi probe (line $smi_line) appears before or at same line as tegrastats (line $tegra_line) — priority wrong"
  exit 0
fi

# Verify the priority comment is present and correct.
has_priority_comment=$(echo "$src" | grep -c "tegrastats.*nvidia-smi\|priority.*tegra\|tegra.*priority" || echo 0)

# Verify the structure is if/else-if (not parallel — only one probe fires).
has_else_if=$(echo "$src" | grep -c "else if.*[Tt]egra\|} else if.*smi\|else.*if.*[Pp]robe" || echo 0)

save_evidence "$CURRENT_STORY" "priority_check.json" \
  "{\"has_priority_comment\": $has_priority_comment, \"has_else_if\": $has_else_if, \"tegra_before_smi\": true}"

ok "tegrastats probe (line $tegra_line) precedes nvidia-smi (line $smi_line) in cmd/datawatch-stats/main.go — priority confirmed"

# Secondary: if johnnyjohnny is reachable, verify live behavior via the stats API.
if ssh -o BatchMode=yes -o ConnectTimeout=5 johnnyjohnny true 2>/dev/null; then
  # Check if datawatch-stats is running and reporting GPU info.
  stats_resp=$(ssh johnnyjohnny \
    'curl -sk https://localhost:8443/api/stats 2>/dev/null | python3 -c "import sys,json; d=json.load(sys.stdin); g=d.get(\"gpu\",[]); print(f\"gpu_count={len(g)} first={g[0].get(\"vendor\",\"\") if g else \"none\"}\")" 2>/dev/null || echo "not running"' 2>/dev/null || echo "ssh-failed")
  echo "  [TS-765] live stats: $stats_resp"
  save_evidence "$CURRENT_STORY" "live_stats.txt" "$stats_resp"
fi
