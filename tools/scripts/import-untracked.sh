#!/usr/bin/env bash
# Find untracked series under $KURA_LIBRARY_ROOT, take the first N,
# and run `kura import` + `kura scan` for each.
#
# Usage: tools/scripts/import-untracked.sh [LIMIT]
#   LIMIT defaults to 10.
#
# Requires: kura on PATH, jq, KURA_LIBRARY_ROOT set, KURA_TVDB_KEY set
# (import resolves metadata via TVDB).

set -euo pipefail

LIMIT=${1:-10}

if ! command -v jq >/dev/null 2>&1; then
  echo "jq is required but not on PATH" >&2
  exit 127
fi

dirs=()
while IFS= read -r line; do
  dirs+=("$line")
done < <(kura list --status untracked --json | jq -r '.[].root' | head -n "$LIMIT")

if [ "${#dirs[@]}" -eq 0 ]; then
  echo "No untracked series under \$KURA_LIBRARY_ROOT."
  exit 0
fi

ok=0
fail=0
for dir in "${dirs[@]}"; do
  printf '\n=== %s ===\n' "$dir"
  if ! kura import "$dir" --json >/dev/null; then
    printf '  import failed; skipping scan\n'
    fail=$((fail + 1))
    continue
  fi
  # AddResult no longer echoes metadataRef; look it up from the
  # library index now that the series is tracked. Pass the resolved
  # ref to scan so an ambiguous dirname doesn't re-prompt.
  ref=$(kura list --json | jq -r --arg root "$dir" '.[] | select(.root == $root) | .metadataRef')
  if [ -z "$ref" ] || [ "$ref" = "null" ]; then
    printf '  could not look up metadataRef after import; skipping scan\n'
    fail=$((fail + 1))
    continue
  fi
  if ! kura scan "$ref"; then
    printf '  scan failed\n'
    fail=$((fail + 1))
    continue
  fi
  ok=$((ok + 1))
done

printf '\n--- done: %d ok, %d failed ---\n' "$ok" "$fail"
