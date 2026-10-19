#!/usr/bin/env python3
"""Exercise the guard's allowlist and fail-closed behavior."""
import json
import subprocess
import sys
import tempfile
from pathlib import Path

checker = Path(__file__).with_name('check-test-skips.py')
with tempfile.TemporaryDirectory() as directory:
    root = Path(directory)
    log, allowlist = root / 'tests.json', root / 'allowlist.txt'
    package = 'example.com/test'
    start = {'Action': 'start', 'Package': package}
    passed = {'Action': 'pass', 'Package': package}
    skipped = {'Action': 'skip', 'Package': package, 'Test': 'TestDocker'}
    cases = [
        ('pass', [start, passed], '', True),
        ('unknown skip', [start, skipped, passed], '', False),
        ('allowed skip', [start, skipped, passed], f'{package} TestDocker # integration covers it', True),
        ('no integration skips', [start, skipped, passed], None, False),
        ('postgres cannot be allowed', [start, dict(skipped, Test='TestPostgres'), passed], f'{package} TestPostgres # forbidden', False),
        ('failed package', [start, dict(passed, Action='fail')], '', False),
        ('truncated stream', [start], '', False),
        ('empty stream', [], '', False),
        ('no test files', [start, {'Action': 'output', 'Package': package, 'Output': '? example.com/test [no test files]'}, dict(passed, Action='skip')], '', True),
        ('unexplained package skip', [start, dict(passed, Action='skip')], '', False),
    ]
    for name, events, entries, expected in cases:
        log.write_text(''.join(json.dumps(event) + '\n' for event in events))
        args = [sys.executable, str(checker)]
        if entries is not None:
            allowlist.write_text(entries)
            args += ['--allowlist', str(allowlist)]
        result = subprocess.run(args + [str(log)], capture_output=True, text=True)
        if (result.returncode == 0) != expected:
            sys.exit(f'{name}: exit {result.returncode}: {result.stdout}{result.stderr}')
        print(f'{name}: passed')
