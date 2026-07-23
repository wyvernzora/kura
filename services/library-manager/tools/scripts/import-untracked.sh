#!/usr/bin/env bash
# Find untracked series from the running Kura server, take the first N,
# and run `kura import` + `kura scan` for each.
#
# Usage: tools/scripts/import-untracked.sh [LIMIT]
#   LIMIT defaults to 10.
#
# Requires: kura on PATH, jq, and access to the running Kura REST server.
# The server needs its library root and metadata provider configured.

set -euo pipefail

LIMIT=${1:-10}

if ! command -v jq >/dev/null 2>&1; then
  echo "jq is required but not on PATH" >&2
  exit 127
fi

dirs=()
while IFS= read -r line; do
  dirs+=("$line")
done < <(kura list --status untracked --json | jq -r '.[].title' | head -n "$LIMIT")

if [ "${#dirs[@]}" -eq 0 ]; then
  echo "No untracked series in the server index."
  exit 0
fi

ok=0
fail=0
for dir in "${dirs[@]}"; do
  printf '\n=== %s ===\n' "$dir"
  # Capture import's JSON to pull the resolved metadataRef and pass
  # it to scan; otherwise an ambiguous dirname re-prompts for
  # disambiguation on the second command.
  if ! import_json=$(kura import "$dir" --json); then
    printf '  import failed; skipping scan\n'
    fail=$((fail + 1))
    continue
  fi
  ref=$(printf '%s' "$import_json" | jq -r '.metadataRef')
  if [ -z "$ref" ] || [ "$ref" = "null" ]; then
    printf '  import returned no metadataRef; skipping scan\n'
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
