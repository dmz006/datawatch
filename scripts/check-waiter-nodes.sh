#!/usr/bin/env bash
# scripts/check-waiter-nodes.sh — for a running daemon, report the node each
# waiting capacity request is bound to, and flag the case where every waiter of
# a multi-node backend sits on one node while another candidate node is free.
#
# Usage:
#   scripts/check-waiter-nodes.sh [--help]
#
# Environment:
#   DATAWATCH_URL    daemon base URL (default: http://127.0.0.1:8080)
#   DATAWATCH_TOKEN  bearer token, sent when set (default: none)
#
# Data comes from GET /api/capacity, the same endpoint 'datawatch capacity'
# reads. When DATAWATCH_URL is unset and the datawatch CLI is installed, the CLI
# is used ('datawatch capacity --json') so it picks the configured address and
# token itself; otherwise curl is used.
#
# Output: one line per waiter (holder, backend, model, bound node), then a
# summary of waiters per node. A waiter's bound node is the node:<name> pool in
# its pools ("-" when none is bound yet); its backend is the llm:<name> pool.
# The model is shown when the response carries it, "-" otherwise.
#
# Exit status: 0 normally, 2 when, for some backend with two or more candidate
# nodes, every waiter is bound to the same node while another candidate node
# has free capacity (the original symptom), 1 on a usage or fetch error.

set -euo pipefail

usage() { sed -n '2,/^$/p' "${BASH_SOURCE[0]}" | sed -E 's/^# ?//'; }

case "${1:-}" in
  -h | --help)
    usage
    exit 0
    ;;
  "") ;;
  *)
    echo "unknown argument: $1" >&2
    usage >&2
    exit 1
    ;;
esac

run() { env -u TMUX -u TMUX_TMPDIR "$@"; }

fetch() {
  if [ -z "${DATAWATCH_URL:-}" ] && command -v datawatch >/dev/null 2>&1; then
    run datawatch capacity --json
    return
  fi
  local url="${DATAWATCH_URL:-http://127.0.0.1:8080}"
  url="${url%/}"
  local args=(-fsS --max-time 15)
  if [ -n "${DATAWATCH_TOKEN:-}" ]; then
    args+=(-H "Authorization: Bearer ${DATAWATCH_TOKEN}")
  fi
  run curl "${args[@]}" "${url}/api/capacity"
}

if ! body="$(fetch)"; then
  echo "could not read the capacity status from the daemon" >&2
  exit 1
fi

# analyse reads the capacity JSON on stdin and prints the report; it exits 2
# for the same-node symptom, 0 otherwise.
analyse_jq() {
  jq -r '
    def bound: ([.pools[]? | select(startswith("node:")) | sub("^node:"; "")] | first) // "-";
    def backend: ([.pools[]? | select(startswith("llm:")) | sub("^llm:"; "")] | first) // "-";
    def free($n): ([.pools[]? | select(.name == "node:" + $n)] | first) as $p
      | if $p == null then false
        else ($p.limit // 0) == 0 or (($p.held // 0) + ($p.external // 0)) < $p.limit end;
    . as $root
    | [.waiting[]? | {holder: (.holder // "-"), backend: backend, model: (.model // "-"),
                      node: bound, nodes: (.nodes // [])}] as $w
    | ($w[] | "waiter holder=\(.holder) backend=\(.backend) model=\(.model) node=\(.node)"),
      "summary: \($w | length) waiting",
      ($w | group_by(.node)[] | "  node \(.[0].node): \(length)"),
      ( [$w | group_by(.backend)[]
          | select(.[0].nodes | length >= 2)
          | select([.[].node] | unique | length == 1 and .[0] != "-")
          | . as $g | $g[0].node as $bn
          | ($g | map(.nodes[]) | unique | map(select(. != $bn))) as $others
          | ($others | map(select(. as $n | $root | free($n)))) as $freeOthers
          | select($freeOthers | length > 0)
          | "SYMPTOM: every \($g[0].backend) waiter is bound to \($bn) while \($freeOthers | join(",")) has free capacity"
        ] | .[] ),
      "@@SYMPTOMS=" + ([ $w | group_by(.backend)[]
          | select(.[0].nodes | length >= 2)
          | select([.[].node] | unique | length == 1 and .[0] != "-")
          | . as $g | $g[0].node as $bn
          | ($g | map(.nodes[]) | unique | map(select(. != $bn))) as $others
          | select($others | map(select(. as $n | $root | free($n))) | length > 0)
        ] | length | tostring)'
}

analyse_py() {
  python3 -c '
import json, sys
d = json.load(sys.stdin)
def pick(pools, prefix):
    for p in pools or []:
        if p.startswith(prefix):
            return p[len(prefix):]
    return "-"
pool = {p.get("name"): p for p in d.get("pools") or []}
def free(n):
    p = pool.get("node:" + n)
    if p is None:
        return False
    lim = p.get("limit") or 0
    return lim == 0 or (p.get("held") or 0) + (p.get("external") or 0) < lim
w = [dict(holder=x.get("holder") or "-", backend=pick(x.get("pools"), "llm:"),
          model=x.get("model") or "-", node=pick(x.get("pools"), "node:"),
          nodes=x.get("nodes") or []) for x in d.get("waiting") or []]
for x in w:
    print("waiter holder=%(holder)s backend=%(backend)s model=%(model)s node=%(node)s" % x)
print("summary: %d waiting" % len(w))
per = {}
for x in w:
    per[x["node"]] = per.get(x["node"], 0) + 1
for n in sorted(per):
    print("  node %s: %d" % (n, per[n]))
hits = 0
for b in sorted({x["backend"] for x in w}):
    g = [x for x in w if x["backend"] == b]
    cands = sorted({n for x in g for n in x["nodes"]})
    if len(cands) < 2 and not any(len(x["nodes"]) >= 2 for x in g):
        continue
    bound = {x["node"] for x in g}
    if len(bound) != 1 or "-" in bound:
        continue
    bn = next(iter(bound))
    fo = [n for n in cands if n != bn and free(n)]
    if fo:
        hits += 1
        print("SYMPTOM: every %s waiter is bound to %s while %s has free capacity" % (b, bn, ",".join(fo)))
print("@@SYMPTOMS=%d" % hits)
'
}

if command -v jq >/dev/null 2>&1; then
  out="$(printf '%s' "$body" | analyse_jq)"
elif command -v python3 >/dev/null 2>&1; then
  out="$(printf '%s' "$body" | analyse_py)"
else
  echo "need jq or python3 to parse the response" >&2
  exit 1
fi

printf '%s\n' "$out" | sed '/^@@SYMPTOMS=/d'
count="$(printf '%s\n' "$out" | sed -n 's/^@@SYMPTOMS=//p')"
if [ "${count:-0}" -gt 0 ]; then
  exit 2
fi
exit 0
