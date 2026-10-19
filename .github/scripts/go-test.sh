#!/usr/bin/env bash
set -euo pipefail
allowlist=$1
log=$2
shift 2
guard_args=(.github/scripts/check-test-skips.py)
if [[ "${1:-}" == --require ]]; then
  guard_args+=(--require "$2")
  shift 2
fi
mkdir -p "$(dirname "$log")"
"$@" | tee "$log"
if [[ "$allowlist" != - ]]; then
  guard_args+=(--allowlist "$allowlist")
fi
python3 "${guard_args[@]}" "$log"
