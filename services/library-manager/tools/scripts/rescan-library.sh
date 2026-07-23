#!/usr/bin/env bash
# Re-scan every tracked series via the running kura REST server, four
# at a time. macOS ships bash 3.2 (no `wait -n`) so concurrency is
# gated by a tiny FIFO-backed token pool — push N tokens, each worker
# reads one before it starts and writes one back when it's done.
#
# Usage: tools/scripts/rescan-library.sh [extra kura scan flags...]
#   Any extra args are passed verbatim to each `kura scan` invocation,
#   e.g. tools/scripts/rescan-library.sh --json
#
# Concurrency is fixed at 4 — TVDB's unwritten RPS ceiling sits in
# the single digits, and 4 keeps a healthy margin while still
# halving wall-clock on a typical library.
#
# Requires:
#   - kura on PATH (its CLI reads KURA_SERVER_URL + KURA_TOKEN itself)
#   - jq
#   - KURA_TOKEN  — bearer token; matches the server's gate
#   - KURA_SERVER_URL  (optional; defaults to http://127.0.0.1:8080)
#
# Server-side: the kura serve target needs KURA_LIBRARY_ROOT +
# KURA_TVDB_KEY in its own environment. Those are not read here.

set -euo pipefail

CONCURRENCY=4

if ! command -v jq >/dev/null 2>&1; then
  echo "jq is required but not on PATH" >&2
  exit 127
fi
if [ -z "${KURA_TOKEN:-}" ]; then
  echo "KURA_TOKEN is not set — export the server's bearer token before running this script." >&2
  exit 64
fi
# Ensure child `kura` invocations inherit the bearer + server URL.
# `set -a` would also work but explicit `export` keeps the script's
# requirements visible.
export KURA_TOKEN
[ -n "${KURA_SERVER_URL:-}" ] && export KURA_SERVER_URL

# Pull the metadataRef of every tracked (non-untracked, non-error) row
# so each scan invocation hits an unambiguous selector. Stick to the
# while-read pattern (rather than mapfile) so the script runs on
# macOS's bundled bash 3.2.
refs=()
while IFS= read -r line; do
  refs+=("$line")
done < <(kura list --json | jq -r '.[] | select(.status != "untracked" and .status != "error") | .metadataRef')

total=${#refs[@]}
if [ "$total" -eq 0 ]; then
  echo "No tracked series in the server index."
  exit 0
fi

# Per-run scratch dir for the FIFO + the results log. Trap cleans it
# up on any exit (success, failure, or signal).
workdir="$(mktemp -d -t kura-rescan-XXXXXX)"
trap 'rm -rf "$workdir"' EXIT
fifo="$workdir/pool"
results="$workdir/results"
mkfifo "$fifo"
: > "$results"

# Bind fd 3 to the FIFO for read + write. Pre-load CONCURRENCY tokens
# so the first batch can start immediately.
exec 3<>"$fifo"
i=0
while [ "$i" -lt "$CONCURRENCY" ]; do
  printf '.' >&3
  i=$((i + 1))
done

started=0
for ref in "${refs[@]}"; do
  # Acquire a token (blocks until a worker releases one). The pool
  # only ever holds single dots, so a 1-byte read is safe.
  IFS= read -r -n 1 _token <&3
  started=$((started + 1))
  printf '=== [%d/%d] start %s ===\n' "$started" "$total" "$ref"
  (
    if kura scan "$@" "$ref"; then
      printf 'ok\t%s\n' "$ref" >> "$results"
    else
      printf 'fail\t%s\n' "$ref" >> "$results"
      echo "  scan failed: $ref" >&2
    fi
    # Release the token even on failure so the pool stays full.
    printf '.' >&3
  ) &
done

# Drain remaining workers.
wait

ok=$(grep -c '^ok' "$results" || true)
fail=$(grep -c '^fail' "$results" || true)
printf '\n--- done: %d ok, %d failed ---\n' "$ok" "$fail"
exit $((fail == 0 ? 0 : 1))
