#!/usr/bin/env bash
set -euo pipefail

# Runs the repo bootstrap once after a fresh clone.
# Guarded by a sentinel file so subsequent calls are no-ops.
# Use --force to re-run.

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
sentinel="${repo_root}/.git/.crawbl-bootstrapped"

force=0
for arg in "$@"; do [[ "$arg" == "--force" ]] && force=1; done

if [[ "${force}" == "0" && -f "${sentinel}" ]]; then
  exit 0
fi

cd "${repo_root}"
echo "crawbl: running post-clone bootstrap..."
make setup
touch "${sentinel}"
