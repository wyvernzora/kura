#!/usr/bin/env bash
set -euo pipefail

# Conventional Commits v1.0.0 subject lint with the closed monorepo scope
# enum. Same interface as before: a commit-msg file path, or "-" for a
# single subject line on stdin (used by CI for PR titles / pushed commits).

msg_file="${1:?missing commit message file}"
if [[ "$msg_file" == "-" ]]; then
  IFS= read -r subject || true
else
  subject="$(head -n 1 "$msg_file")"
fi

# Exemptions: merge commits and rebase autosquash markers.
case "$subject" in
  "Merge "*|"fixup! "*|"squash! "*) exit 0 ;;
esac

types='feat|fix|docs|refactor|test|build|ci|chore|perf|revert'
scopes='library|indexer|backup|webui|repo|deps|release|n8n|deploy|cli'

if [[ ! "$subject" =~ ^($types)\(($scopes)\)\!?:\ .+ ]]; then
  cat >&2 <<MSG
Commit rejected: subject must be Conventional Commits with a known scope:
  <type>(<scope>): <description>
  types:  ${types//|/, }
  scopes: ${scopes//|/, }
MSG
  exit 1
fi

if (( ${#subject} > 72 )); then
  echo "Commit rejected: subject exceeds 72 characters." >&2
  exit 1
fi
