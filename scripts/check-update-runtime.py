#!/usr/bin/env python3
"""Linux supervisor regression smoke using two prebuilt fixture versions.

Does not publish releases or use RouterOS. Enqueues already-verified candidates
locally to exercise the installation half; download trust is covered by Go tests.
Run as an unprivileged user: python3 scripts/check-update-runtime.py --old ... --new ...
"""
import argparse
import http.cookiejar
import json
import os
from pathlib import Path
import shutil
import signal
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.request


def wait_for(fn, label, timeout=60):
    deadline = time.monotonic() + timeout
    last = None
    while time.monotonic() < deadline:
        try:
            value = fn()
            if value:
                return value
        except (OSError, ValueError, urllib.error.URLError) as error:
            last = error
        time.sleep(0.2)
    raise AssertionError(f"timeout waiting for {label}: {last}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--old', required=True)
    parser.add_argument('--new', required=True)
    parser.add_argument('--check-github', action='store_true', help='also check the official release API over the network')
    args = parser.parse_args()
    old, new = Path(args.old).resolve(), Path(args.new).resolve()
    old_version = json.loads(subprocess.check_output([old, 'version']))['version']
    new_version = json.loads(subprocess.check_output([new, 'version']))['version']
    assert old_version != new_version
    with tempfile.TemporaryDirectory(prefix='rosboard-update-smoke-') as temp:
        root = Path(temp)
        binary, supervisor = root / 'rosboard', root / 'rosboard-supervisor'
        shutil.copy2(old, binary)
        shutil.copy2(old, supervisor)
        with socket.socket() as sock:
            sock.bind(('127.0.0.1', 0))
            port = sock.getsockname()[1]
        config = root / 'config.yaml'
        config.write_text(f'listen_address: "127.0.0.1:{port}"\ndata_dir: "{root / "data"}"\nallowed_cidrs: ["127.0.0.0/8"]\n')
        config.chmod(0o600)
        base = f'http://127.0.0.1:{port}'
        opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))

        def request(path, data=None):
            headers = {'Origin': base}
            if data is not None:
                headers['Content-Type'] = 'application/json'
            req = urllib.request.Request(base + path, data=json.dumps(data).encode() if data is not None else None, headers=headers)
            with opener.open(req, timeout=25 if path.endswith('/check') else 2) as response:
                payload = response.read()
                return json.loads(payload) if payload else {}

        log = (root / 'runtime.log').open('w+')
        proc = None

        def start():
            return subprocess.Popen([supervisor, 'supervise', '-binary', binary, '-config', config], cwd=root, stdout=log, stderr=log, start_new_session=True, env={**os.environ, 'ROSBOARD_LISTEN_ADDRESS': f'127.0.0.1:{port}'})

        def stop():
            if proc and proc.poll() is None:
                os.killpg(proc.pid, signal.SIGTERM)
                proc.wait(timeout=40)

        def state():
            return request('/api/settings/update')

        def child_pid():
            children = [pid for task in Path(f'/proc/{proc.pid}/task').glob('*/children') for pid in task.read_text().split()]
            assert len(children) == 1, children
            return int(children[0])

        def enqueue(candidate, job_id, target):
            state_dir = root / '.rosboard-update'
            shutil.copy2(candidate, state_dir / 'candidate')
            journal = {'id': job_id, 'from': state()['current']['version'], 'to': target, 'stage': 'pending', 'startedAt': '2026-09-09T00:00:00Z'}
            tmp = state_dir / 'job.tmp'
            tmp.write_text(json.dumps(journal))
            tmp.replace(state_dir / 'job.json')
            os.kill(child_pid(), signal.SIGTERM)

        try:
            proc = start()
            wait_for(lambda: request('/api/health')['ok'], 'initial health')
            request('/api/setup/admin', {'username': 'update-smoke', 'password': 'fixture-only-1234', 'passwordConfirmation': 'fixture-only-1234'})
            request('/api/setup/complete', {'skipRouterOS': True})
            assert state()['current']['version'] == old_version
            if args.check_github:
                checked = request('/api/settings/update/check', {})
                assert not checked['checkError'], checked['checkError']
                assert checked['latest'] and checked['checkedAt']
                print('PASS: authenticated official GitHub check, latest=' + checked['latest']['version'])
            # The server remains supervised by the original, stable process.
            supervisor_pid = proc.pid
            (root / 'data' / 'fixture-history').write_text('preserve this data')
            enqueue(new, 'success', new_version)
            wait_for(lambda: state()['current']['version'] == new_version and state()['job']['stage'] == 'succeeded', 'successful update')
            assert proc.pid == supervisor_pid and proc.poll() is None
            assert (root / 'data' / 'fixture-history').read_text() == 'preserve this data'
            assert request('/api/bootstrap')['authenticated']
            print('PASS: install, new version health, persistent session/data, stable supervisor')

            broken = root / 'broken'
            broken.write_text('not an executable\n')
            broken.chmod(0o700)
            enqueue(broken, 'bad-start', '99.0.0')
            wait_for(lambda: state()['job']['id'] == 'bad-start' and state()['job']['stage'] == 'rolled_back', 'failed startup rollback')
            assert state()['current']['version'] == new_version
            assert (root / 'data' / 'fixture-history').read_text() == 'preserve this data'
            assert request('/api/bootstrap')['authenticated']
            print('PASS: failed candidate restores binary, data and session')

            # A later download interrupted by a process exit must not leave the
            # next child permanently locked in an active update state.
            journal = root / '.rosboard-update' / 'job.json'
            journal.write_text(json.dumps({'id': 'interrupted-download', 'stage': 'downloading', 'from': new_version, 'to': '99.0.0'}))
            os.kill(child_pid(), signal.SIGTERM)
            wait_for(lambda: state()['job']['stage'] == 'failed', 'interrupted download')
            print('PASS: interrupted subsequent download releases update lock')

            stop()
            # Simulate a power loss after replace: stable supervisor must run
            # even when the main executable and data are unusable.
            shutil.copy2(broken, binary)
            (root / 'data' / 'fixture-history').write_text('candidate changed data')
            journal.write_text(json.dumps({'id': 'power-loss', 'stage': 'verifying_startup', 'from': new_version, 'to': '99.0.0'}))
            proc = start()
            wait_for(lambda: state()['job']['id'] == 'power-loss' and state()['job']['stage'] == 'rolled_back', 'reboot recovery')
            assert state()['current']['version'] == new_version
            assert (root / 'data' / 'fixture-history').read_text() == 'preserve this data'
            print('PASS: supervisor restart recovers interrupted installation')
            request('/api/settings/full-reset', {'confirmed': True})
            wait_for(lambda: request('/api/bootstrap')['phase'] == 'needs_admin', 'full reset')
            assert not (root / '.rosboard-update' / 'backup' / 'previous').exists()
            assert not (root / '.rosboard-update' / 'job.json').exists()
            print('PASS: full reset removes private recovery snapshots')
        except Exception:
            log.flush()
            log.seek(0)
            print(log.read()[-6000:])
            raise
        finally:
            stop()
            log.close()


if __name__ == '__main__':
    main()
