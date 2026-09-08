#!/usr/bin/env bash
# Print the CHANGELOG.md section for a version (e.g. 0.2.0) — used as the GitHub release body.
set -euo pipefail
v="${1:?usage: release-notes.sh <version>}"
cd "$(dirname "$0")/.."
awk -v v="$v" '
  /^## \[/ { if (found) exit; found = ($0 ~ "^## \\[" v "\\]") ; next }
  /^\[[^]]+\]: / { if (found) exit }
  found { print }
' CHANGELOG.md | sed -e '1{/^$/d}' -e '${/^$/d}'
