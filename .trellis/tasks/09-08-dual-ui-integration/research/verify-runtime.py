"""Read-only deployed asset/auth-boundary verification. No credentials required."""
import hashlib
import json
import sys
from pathlib import Path
from urllib.error import HTTPError
from urllib.request import urlopen

base = sys.argv[1].rstrip('/')
dist = Path(__file__).resolve().parents[4] / 'internal/ui/dist'

def get(path):
    try:
        with urlopen(base + path, timeout=15) as response:
            return response.status, response.read(), response.headers.get('Content-Type', '')
    except HTTPError as error:
        return error.code, error.read(), error.headers.get('Content-Type', '')

status, body, _ = get('/api/health')
assert status == 200, (status, body)
status, body, _ = get('/api/bootstrap')
phase = json.loads(body)['phase']
assert status == 200 and phase in ['needs_admin', 'needs_login', 'needs_routeros', 'ready']
for query in ['/', '/?ui=compact', '/?ui=aurora']:
    status, body, content_type = get(query)
    assert status == 200 and 'text/html' in content_type
    assert body == (dist / 'index.html').read_bytes(), query
checked = 0
for file in sorted(dist.rglob('*')):
    if not file.is_file() or any(part.startswith('.') for part in file.relative_to(dist).parts):
        continue
    path = '/' + file.relative_to(dist).as_posix()
    status, body, content_type = get(path)
    assert status == 200 and hashlib.sha256(body).digest() == hashlib.sha256(file.read_bytes()).digest(), path
    if file.suffix == '.js': assert 'javascript' in content_type, (path, content_type)
    if file.suffix == '.css': assert 'text/css' in content_type, (path, content_type)
    checked += 1
for path in ['/api/settings', '/api/devices', '/api/terminals?device=missing', '/api/target-lists?device=missing', '/api/policy-routing/overview?device=missing', '/api/access-control/devices/missing']:
    status, body, _ = get(path)
    assert status in [401, 403, 409], (path, status)
print(json.dumps({'base': base, 'health': 'ok', 'bootstrapPhase': phase, 'matchedEmbeddedAssets': checked, 'uiEntries': ['compact', 'aurora'], 'unauthenticatedApiBoundary': 'pass'}))
