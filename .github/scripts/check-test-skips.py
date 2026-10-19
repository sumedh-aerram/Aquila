#!/usr/bin/env python3
"""Reject unexpected skips and incomplete or failing go test JSON streams."""
import argparse
import json
import sys
from pathlib import Path

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--allowlist', type=Path)
parser.add_argument('logs', nargs='+', type=Path)
args = parser.parse_args()
allowed = set()
if args.allowlist:
    for number, line in enumerate(args.allowlist.read_text().splitlines(), 1):
        entry, _, reason = line.partition('#')
        if not entry.strip():
            continue
        fields = entry.split()
        if len(fields) != 2 or not reason.strip() or 'Postgres' in fields[1]:
            sys.exit(f'{args.allowlist}:{number}: invalid skip allowance')
        allowed.add(tuple(fields))
errors = []
for log in args.logs:
    started, completed, no_tests = set(), set(), set()
    events = 0
    for number, line in enumerate(log.read_text().splitlines(), 1):
        try:
            event = json.loads(line)
        except json.JSONDecodeError:
            errors.append(f'{log}:{number}: invalid test JSON')
            continue
        events += 1
        action = event.get('Action')
        package, test = event.get('Package', ''), event.get('Test')
        if action == 'output' and test is None and '[no test files]' in event.get('Output', ''):
            no_tests.add(package)
        if action == 'start' and package:
            started.add(package)
        if action in ('pass', 'fail', 'skip') and test is None:
            completed.add(package)
        if action == 'fail':
            errors.append(f'{package} {test or "(package)"}: failed')
        if action == 'skip' and not (test is None and package in no_tests):
            key = (package, test or '(package)')
            if key not in allowed or 'Postgres' in (test or ''):
                errors.append(f'{package} {test or "(package)"}: unexpected skip')
    if not events or not started or started != completed:
        errors.append(f'{log}: empty or incomplete test stream')
if errors:
    sys.exit('\n'.join(errors))
print('test skip guard: passed')
