#!/usr/bin/env python3
"""Reject skipped, missing, failing, or incomplete go test executions."""
import argparse
import json
import sys
from pathlib import Path

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--allowlist', type=Path)
parser.add_argument('--require', default='', help='comma-separated test names that must run and pass')
parser.add_argument('logs', nargs='+', type=Path)
args = parser.parse_args()
required = set(args.require.split(',')) if args.require else set()
if any(not name.strip() or name != name.strip() for name in required):
    parser.error('--require must contain nonempty test names without surrounding spaces')
allowed = set()
if args.allowlist:
    for number, line in enumerate(args.allowlist.read_text().splitlines(), 1):
        entry, _, reason = line.partition('#')
        if not entry.strip():
            continue
        fields = entry.split()
        if len(fields) != 2 or not reason.strip() or 'postgres' in fields[1].casefold():
            sys.exit(f'{args.allowlist}:{number}: invalid skip allowance')
        allowed.add(tuple(fields))
errors, seen, passed = [], set(), set()
for log in args.logs:
    started, completed, no_tests, ran = set(), set(), set(), set()
    outcomes = {}
    events = 0
    for number, line in enumerate(log.read_text().splitlines(), 1):
        try:
            event = json.loads(line)
        except json.JSONDecodeError:
            errors.append(f'{log}:{number}: invalid test JSON')
            continue
        if not isinstance(event, dict):
            errors.append(f'{log}:{number}: test event must be an object')
            continue
        events += 1
        action = event.get('Action')
        package, test = event.get('Package', ''), event.get('Test')
        key = (package, test)
        if action == 'run' and test:
            ran.add(key)
            seen.add(key)
        if action == 'pass' and test and key in ran:
            passed.add(test)
        if action == 'output' and test is None and '[no test files]' in event.get('Output', ''):
            no_tests.add(package)
        if action == 'start' and package:
            started.add(package)
        if action in ('pass', 'fail', 'skip') and test is None:
            completed.add(package)
            outcomes[package] = action
        if action == 'fail':
            errors.append(f'{package} {test or "(package)"}: failed')
        if action == 'skip' and not (test is None and package in no_tests):
            if key not in allowed or 'postgres' in (test or '').casefold():
                errors.append(f'{package} {test or "(package)"}: unexpected skip')
    if not events or not started or started != completed:
        errors.append(f'{log}: empty or incomplete test stream')
    ran_packages = {package for package, _ in ran}
    for package in sorted(started - ran_packages):
        if package not in no_tests or outcomes.get(package) != 'skip':
            errors.append(f'{package}: no test-level run events')
for package, test in sorted(allowed - seen):
    errors.append(f'{package} {test}: unused skip allowance')
for test in sorted(required - passed):
    errors.append(f'{test}: required test did not run and pass')
if errors:
    sys.exit('\n'.join(errors))
print('test skip guard: passed')
