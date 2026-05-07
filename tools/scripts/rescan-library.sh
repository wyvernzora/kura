#!/usr/bin/env bash
# Re-scan every tracked series under $KURA_LIBRARY_ROOT.
#
# Usage: tools/scripts/rescan-library.sh [extra kura scan flags...]
#   Any extra args are passed verbatim to each `kura scan` invocation,
#   e.g. tools/scripts/rescan-library.sh --json
#
# Throttled to at most one scan per second so the TVDB API doesn't get
# pounded on a large library.
#
# Requires: kura on PATH, jq, KURA_LIBRARY_ROOT set, KURA_TVDB_KEY set.

set -euo pipefail

if ! command -v jq >/dev/null 2>&1; then
  echo "jq is required but not on PATH" >&2
  exit 127
fi

# Pull the metadataRef of every tracked (non-untracked, non-error) row
# so each scan invocation hits an unambiguous selector. Stick to the
# while-read pattern (rather than mapfile) so the script runs on
# macOS's bundled bash 3.2.
refs=()
while IFS= read -r line; do
  refs+=("$line")
done < <(kura list --json | jq -r '.[] | select(.status != "untracked" and .status != "error") | .metadataRef')

if [ "${#refs[@]}" -eq 0 ]; then
  echo "No tracked series under \$KURA_LIBRARY_ROOT."
  exit 0
fi

ok=0
fail=0
total=${#refs[@]}
for i in "${!refs[@]}"; do
  ref="${refs[$i]}"
  printf '\n=== [%d/%d] %s ===\n' "$((i + 1))" "$total" "$ref"
  if kura scan "$@" "$ref"; then
    ok=$((ok + 1))
  else
    printf '  scan failed\n'
    fail=$((fail + 1))
  fi
  # Throttle: skip the sleep after the last entry.
  if [ "$i" -lt $((total - 1)) ]; then
    sleep 1
  fi
done

printf '\n--- done: %d ok, %d failed ---\n' "$ok" "$fail"
exit $((fail == 0 ? 0 : 1))
