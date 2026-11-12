#!/usr/bin/env bash
# Verification for the CodeQL gate documentation and plan: the same short
# paragraph in the CodeQL local scan doc and the CI watch doc says when the
# gate runs and why documents-only pushes and unrelated pull requests are
# skipped; in the CI watch doc it sits after the section's first paragraph;
# the plan records the branch protection consequence in the exact lowercase
# phrases; no tracker id or PRD word in the two how-to docs; no literal
# version number in the plan.
unset TMUX TMUX_TMPDIR
cd "$(dirname "$0")/.." || exit 1
rc=0
ok()  { echo "ok   $1"; }
bad() { echo "FAIL $1"; rc=1; }

A=docs/howto/codeql-local-scan.md
B=docs/howto/automation-repo-ci-watch.md
for f in "$A" "$B"; do
  [ -f "$f" ] || { bad "$f is missing"; continue; }
  if grep -n -E '(^|[^A-Za-z0-9])(BL|GH|SEC|B)[0-9]{2,}([^0-9]|$)|PRD' "$f" >/dev/null; then
    bad "$f names a tracker id or the word PRD"
  else
    ok "$f has no tracker id or PRD word"
  fi
done

pa="$(awk 'BEGIN{RS=""} tolower($0) ~ /documents[- ]only/ {print; exit}' "$A" 2>/dev/null)"
pb="$(awk 'BEGIN{RS=""} tolower($0) ~ /documents[- ]only/ {print; exit}' "$B" 2>/dev/null)"

if [ -z "$pa" ] || [ -z "$pb" ]; then
  bad "a how-to doc is missing its paragraph on when the gate runs"
else
  if [ "$pa" = "$pb" ]; then ok "both how-to paragraphs are word for word identical";
  else bad "the two how-to paragraphs differ"; fi

  words="$(printf '%s' "$pa" | wc -w)"
  if [ "$words" -lt 150 ] && [ "$words" -gt 0 ]; then ok "paragraph is under 150 words ($words)";
  else bad "paragraph word count out of range ($words)"; fi

  need() { printf '%s' "$pa" | grep -q "$1" && ok "paragraph says: $1" || bad "paragraph lacks: $1"; }
  need 'documents-only'
  need 'pull request'
  need 'weekly'
  need 'no setting'
  if printf '%s' "$pa" | grep -q 'dependabot pull requests are skipped'; then
    bad "paragraph wrongly claims dependabot pull requests are skipped"
  else
    ok "paragraph does not claim dependabot pull requests are skipped"
  fi
  if printf '%s' "$pa" | grep -q 'go.mod.*go.sum\|go.sum'; then
    ok "paragraph keeps go.mod / go.sum changes in scope"
  else
    bad "paragraph does not say go.mod and go.sum changes still run the gate"
  fi

  # Placement in the CI watch doc: the paragraph must come after the section's
  # first paragraph and therefore before that section's first table.
  sect=$(awk '/^## What the watch sees$/{c++; if (c == 1) {f=1; next}} /^## /{if (f) exit} f' "$B")
  pos_para=$(printf '%s\n' "$sect" | awk 'tolower($0) ~ /documents-only/ {print NR; exit}')
  pos_table=$(printf '%s\n' "$sect" | awk '/^\|/ {print NR; exit}')
  if [ -n "$pos_para" ] && [ -n "$pos_table" ] && [ "$pos_para" -lt "$pos_table" ]; then
    ok "in the CI watch doc the paragraph sits after the first paragraph, before the table"
  else
    bad "in the CI watch doc the paragraph is not after the section's first paragraph"
  fi
fi

p=docs/plans/2026-11-07-codeql-gate-trigger-narrowing.md
if [ ! -f "$p" ]; then
  bad "$p is missing"
else
  phrase() { grep -q "$1" "$p" && ok "plan says (exact): $1" || bad "plan lacks exact phrase: $1"; }
  phrase 'protected path'
  phrase 'branch protection'
  phrase 'required check'
  phrase 'next patch above main'
  phrase 'Monday at 04:17 UTC'
  if grep -q '\\\\' "$p"; then bad "plan contains an escape backslash"
  else ok "plan has no backslashes"; fi

  if grep -q -F 'I approve the change but I'"'"'m questioning the way it is working' "$p"; then
    ok "plan quotes the operator's first remark"
  else
    bad "plan does not quote the operator's first remark verbatim"
  fi
  grep -q -F '"Those changes are good, make them"' "$p" \
    && ok "plan quotes the operator's second remark" \
    || bad "plan does not quote the operator's second remark verbatim"

  if grep -q -E '[0-9]+\.[0-9]+\.[0-9]+' "$p"; then
    bad "plan contains a literal X.Y.Z version number"
  else
    ok "plan has no literal version number"
  fi

  if grep -n -E '(^|[^A-Za-z0-9])(BL|GH|SEC|B)[0-9]{2,}([^0-9]|$)|PRD' "$p" >/dev/null; then
    bad "plan names a tracker id or the word PRD"
  else
    ok "plan has no tracker id or PRD word"
  fi
fi
exit $rc
