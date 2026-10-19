#!/usr/bin/env python3
"""Exercise execution, skip, allowlist, and required-test policies."""
import json
import subprocess
import sys
import tempfile
from pathlib import Path

checker = Path(__file__).with_name('check-test-skips.py')
package = 'example.com/test'
start = {'Action': 'start', 'Package': package}
passed = {'Action': 'pass', 'Package': package}
run = {'Action': 'run', 'Package': package, 'Test': 'TestDocker'}
test_pass = dict(run, Action='pass')
skipped = dict(run, Action='skip')
normal = [start, run, test_pass, passed]
no_files = [start, {'Action': 'output', 'Package': package, 'Output': '? example.com/test [no test files]'}, dict(passed, Action='skip')]
allow = f'{package} TestDocker # integration covers it'
other = 'example.com/other'
other_normal = [dict(event, Package=other, **({'Test': 'TestEcho'} if 'Test' in event else {})) for event in normal]
cases = [
    ('pass', [normal], '', '', True, ''),
    ('zero tests', [[start, passed]], '', '', False, 'no test-level run events'),
    ('no matching tests', [[start, {'Action': 'output', 'Package': package, 'Output': 'testing: warning: no tests to run'}, passed]], '', '', False, 'no test-level run events'),
    ('empty package among running packages', [normal, [dict(start, Package=other), dict(passed, Package=other)]], '', '', False, f'{other}: no test-level run events'),
    ('unknown skip', [[start, run, skipped, passed]], '', '', False, 'unexpected skip'),
    ('allowed skip', [[start, run, skipped, passed]], allow, '', True, ''),
    ('allowlisted test runs and passes', [normal], allow, '', True, ''),
    ('no integration skips', [[start, run, skipped, passed]], None, '', False, 'unexpected skip'),
    ('unused allowance', [normal], f'{package} TestGone # renamed test', '', False, 'unused skip allowance'),
    ('allowance seen across logs', [other_normal, normal], allow, '', True, ''),
    ('postgres cannot be allowed', [normal], f'{package} TestPostgres # forbidden', '', False, 'invalid skip allowance'),
    ('uppercase postgres cannot be allowed', [normal], f'{package} TestPOSTGRESx # forbidden', '', False, 'invalid skip allowance'),
    ('lowercase postgres cannot be allowed', [normal], f'{package} Testpostgres # forbidden', '', False, 'invalid skip allowance'),
    ('required pass', [normal], '', 'TestDocker', True, ''),
    ('required missing', [normal], '', 'TestGone', False, 'TestGone: required test did not run and pass'),
    ('required skip', [[start, run, skipped, passed]], allow, 'TestDocker', False, 'required test did not run and pass'),
    ('required pass without run', [[start, test_pass, passed]], '', 'TestDocker', False, 'required test did not run and pass'),
    ('required failed', [[start, run, dict(test_pass, Action='fail'), dict(passed, Action='fail')]], '', 'TestDocker', False, 'required test did not run and pass'),
    ('multiple required passes', [normal, other_normal], '', 'TestDocker,TestEcho', True, ''),
    ('one of multiple required missing', [normal], '', 'TestDocker,TestEcho', False, 'TestEcho: required test did not run and pass'),
    ('failed package', [[start, run, test_pass, dict(passed, Action='fail')]], '', '', False, '(package): failed'),
    ('truncated stream', [[start, run]], '', '', False, 'empty or incomplete test stream'),
    ('empty stream', [[]], '', '', False, 'empty or incomplete test stream'),
    ('no test files', [no_files], '', '', True, ''),
    ('no test files cannot satisfy requirement', [no_files], '', 'TestDocker', False, 'required test did not run and pass'),
    ('misleading no test files output', [[*no_files[:-1], passed]], '', '', False, 'no test-level run events'),
    ('unexplained package skip', [[start, dict(passed, Action='skip')]], '', '', False, 'unexpected skip'),
    ('nonobject JSON', [[None]], '', '', False, 'test event must be an object'),
]
with tempfile.TemporaryDirectory() as directory:
    root = Path(directory)
    allowlist = root / 'allowlist.txt'
    for name, streams, entries, required, expected, diagnostic in cases:
        logs = []
        for number, events in enumerate(streams):
            log = root / f'tests-{number}.json'
            log.write_text(''.join(json.dumps(event) + '\n' for event in events))
            logs.append(str(log))
        args = [sys.executable, str(checker)]
        if entries is not None:
            allowlist.write_text(entries)
            args += ['--allowlist', str(allowlist)]
        if required:
            args += ['--require', required]
        result = subprocess.run(args + logs, capture_output=True, text=True)
        output = result.stdout + result.stderr
        if (result.returncode == 0) != expected or diagnostic not in output:
            sys.exit(f'{name}: exit {result.returncode}: {output}')
        print(f'{name}: passed')
