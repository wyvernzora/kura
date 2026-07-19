#!/usr/bin/env bash
set -euo pipefail

msg_file="${1:?missing commit message file}"
if [[ "$msg_file" == "-" ]]; then
  IFS= read -r subject || true
else
  subject="$(head -n 1 "$msg_file")"
fi

if [[ ! "$subject" =~ ^[a-z0-9][a-z0-9_-]*:\ .+ ]]; then
  cat >&2 <<'MSG'
Commit rejected: subject must use the "<scope>: <message>" format.
MSG
  exit 1
fi
