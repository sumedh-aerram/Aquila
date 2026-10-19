#!/usr/bin/env bash
set -euo pipefail
allowlist=$1
log=$2
shift 2
mkdir -p "$(dirname "$log")"
"$@" | tee "$log"
if [[ "$allowlist" == - ]]; then
  python3 .github/scripts/check-test-skips.py "$log"
else
  python3 .github/scripts/check-test-skips.py --allowlist "$allowlist" "$log"
fi
