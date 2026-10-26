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
# Output: one line per waiter (holder, backend, model, bound node, candidate
# nodes), then a summary of waiters per node. The backend is the llm:<name> pool.
#
# Bound node: the daemon picks a node for a multi-node request only at dispatch
# time, so a queued waiter is bound only when the response names exactly one
# node for it: a single node:<name> pool, or no node pool and a single entry in
# its candidate list ("nodes"). A waiter with several candidate nodes is not
# bound yet; it prints node=- and lists its candidates, and is counted as
# "unbound" in the summary. It is never assumed to sit on the first candidate.
#
# Model: queued requests carry no model in the response. The column shows the
# waiter's own model when present, else the model of a running lease held by the
# same holder, else "-".
#
# Exit status: 0 normally, 2 when, for some backend whose waiters name two or
# more candidate nodes in all, every waiter is bound and all to the same node
# while another candidate node has free capacity (the original symptom). A
# backend with any unbound waiter is never flagged. 1 on a usage or fetch error.

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

# analyse_jq and analyse_py read the capacity JSON on stdin, print the report
# and finish with an @@SYMPTOMS=<n> line. They implement the same rules.
analyse_jq() {
  jq -r '
    def pick($p): [.pools[]? | select(startswith($p)) | sub("^" + $p; "")];
    def free($n): ([.pools[]? | select(.name == "node:" + $n)] | first) as $p
      | if $p == null then false
        else ($p.limit // 0) == 0 or (($p.held // 0) + ($p.external // 0)) < $p.limit end;
    . as $root
    | ([.leases[]? | select((.model // "") != "") | {key: .holder, value: .model}] | from_entries) as $lm
    | [.waiting[]? | pick("node:") as $np | (.nodes // []) as $cn
        | (($cn + $np) | unique) as $cands
        | {holder: (.holder // "-"),
           backend: ((pick("llm:") | first) // "-"),
           model: ((if (.model // "") != "" then .model else $lm[.holder // ""] end) // "-"),
           node: (if ($np | length) == 1 then $np[0]
                  elif ($np | length) == 0 and ($cn | length) == 1 then $cn[0]
                  else "-" end),
           cands: $cands}] as $w
    | ($w[] | "waiter holder=\(.holder) backend=\(.backend) model=\(.model) node=\(.node)"
              + (if .node == "-" and (.cands | length) > 0 then " candidates=\(.cands | join(","))" else "" end)),
      "summary: \($w | length) waiting",
      ($w | group_by(.node)[] | "  node \(if .[0].node == "-" then "unbound" else .[0].node end): \(length)"),
      ( [ $w | group_by(.backend)[]
          | . as $g
          | ($g | map(.cands[]) | unique) as $all
          | ($g | map(.node) | unique) as $bn
          | select(($all | length) >= 2 and ($bn | length) == 1 and $bn[0] != "-")
          | ($all | map(select(. != $bn[0])) | map(select(. as $n | $root | free($n)))) as $fo
          | select($fo | length > 0)
          | {line: "SYMPTOM: every \($g[0].backend) waiter is bound to \($bn[0]) while \($fo | join(",")) has free capacity"}
        ] as $hits
        | ($hits[] | .line), "@@SYMPTOMS=\($hits | length)")'
}

analyse_py() {
  python3 -I -c '
import json, sys
d = json.load(sys.stdin)
def pick(pools, prefix):
    return [p[len(prefix):] for p in pools or [] if p.startswith(prefix)]
pool = {p.get("name"): p for p in d.get("pools") or []}
lm = {l.get("holder"): l.get("model") for l in d.get("leases") or [] if l.get("model")}
def free(n):
    p = pool.get("node:" + n)
    if p is None:
        return False
    lim = p.get("limit") or 0
    return lim == 0 or (p.get("held") or 0) + (p.get("external") or 0) < lim
w = []
for x in d.get("waiting") or []:
    np_ = pick(x.get("pools"), "node:")
    cn = x.get("nodes") or []
    if len(np_) == 1:
        node = np_[0]
    elif not np_ and len(cn) == 1:
        node = cn[0]
    else:
        node = "-"
    llm = pick(x.get("pools"), "llm:")
    w.append(dict(holder=x.get("holder") or "-", backend=llm[0] if llm else "-",
                  model=x.get("model") or lm.get(x.get("holder")) or "-",
                  node=node, cands=sorted(set(cn) | set(np_))))
for x in w:
    line = "waiter holder=%(holder)s backend=%(backend)s model=%(model)s node=%(node)s" % x
    if x["node"] == "-" and x["cands"]:
        line += " candidates=" + ",".join(x["cands"])
    print(line)
print("summary: %d waiting" % len(w))
per = {}
for x in w:
    per[x["node"]] = per.get(x["node"], 0) + 1
for n in sorted(per):
    print("  node %s: %d" % ("unbound" if n == "-" else n, per[n]))
hits = 0
for b in sorted({x["backend"] for x in w}):
    g = [x for x in w if x["backend"] == b]
    allc = sorted({n for x in g for n in x["cands"]})
    bn = {x["node"] for x in g}
    if len(allc) < 2 or len(bn) != 1 or "-" in bn:
        continue
    bn = next(iter(bn))
    fo = [n for n in allc if n != bn and free(n)]
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
