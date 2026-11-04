"""Restricted, manually approved CD receiver. Install outside the Git checkout.

Only updates API/web images; changed database migrations require a separate plan.
No shell commands, arbitrary paths, git pull, database restore or volume deletion.
"""
import copy
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import time
import urllib.request
import uuid

ROOT = Path('/root/projects/TaskFlow')
PROJECT = 'taskflow-server'
SERVICES = ('api', 'migrate', 'web')
REGISTRY = 'ghcr.io/pluto-han/taskflow-'


def run(args, *, timeout=180):
    result = subprocess.run(args, cwd=ROOT, text=True, stdout=subprocess.PIPE,
                            stderr=subprocess.PIPE, timeout=timeout)
    if result.returncode:
        # Never dump process output: config errors can contain sensitive values.
        raise RuntimeError(f'{args[0]} operation failed (exit {result.returncode}); inspect locally')
    return result.stdout.strip()


def compose(config, *args, timeout=180):
    return run(['docker', 'compose', '--project-directory', str(ROOT),
                '-p', PROJECT, '-f', str(config), *args], timeout=timeout)


def validate_request(data):
    if not isinstance(data, dict) or set(data) != {'revision', 'images'}:
        raise ValueError('expected revision and images only')
    if not isinstance(data['revision'], str) or not re.fullmatch(r'[0-9a-f]{40}', data['revision']):
        raise ValueError('revision must be a full commit SHA')
    images = data['images']
    if not isinstance(images, dict) or set(images) != set(SERVICES):
        raise ValueError('exactly api, migrate and web images required')
    for service in SERVICES:
        expected = re.escape(REGISTRY + service) + r'@sha256:[0-9a-f]{64}'
        if not isinstance(images[service], str) or not re.fullmatch(expected, images[service]):
            raise ValueError('only fixed TaskFlow packages with immutable digests are accepted')
    return data


def migration_hashes(image):
    """Copy SQL from a stopped disposable container, never execute its entrypoint."""
    container = run(['docker', 'create', '--network', 'none', '--entrypoint', '/unused', image])
    try:
        with tempfile.TemporaryDirectory(prefix='taskflow-migrations-') as tmp:
            run(['docker', 'cp', container + ':/migrations/.', tmp])
            paths = sorted(Path(tmp).rglob('*'))
            if any(p.is_symlink() for p in paths):
                raise RuntimeError('migration symlinks are not permitted')
            files = [p for p in paths if p.is_file()]
            if not files or any(p.suffix != '.sql' for p in files):
                raise RuntimeError('unexpected migration directory')
            return {str(p.relative_to(tmp)): hashlib.sha256(p.read_bytes()).hexdigest() for p in files}
    finally:
        run(['docker', 'rm', container])


def validate_config(config):
    if set(config.get('services', {})) != {'postgres', *SERVICES}:
        raise RuntimeError('unexpected services in deployment config')
    for service in ('postgres', 'api', 'migrate'):
        if config['services'][service].get('ports'):
            raise RuntimeError('database/API/migrate must not publish host ports')
    ports = config['services']['web'].get('ports', [])
    if len(ports) != 1 or ports[0].get('host_ip') != '127.0.0.1' or str(ports[0].get('published')) != '8082':
        raise RuntimeError('web must publish only 127.0.0.1:8082')
    if config['services']['api'].get('environment', {}).get('TASKFLOW_PUBLIC_ORIGIN') != 'https://taskflow.hancongzhang.com':
        raise RuntimeError('unexpected public origin')


def snapshot(config, path):
    """Record actual current container image IDs, not mutable tags in an old file."""
    old = copy.deepcopy(config)
    for service in SERVICES:
        cid = compose(path, 'ps', '-a', '-q', service)
        if not cid or '\n' in cid:
            raise RuntimeError(f'expected one existing {service} container')
        old['services'][service]['image'] = run(['docker', 'inspect', '--format', '{{.Image}}', cid])
        old['services'][service].pop('build', None)
    return old


def write_json(path, data):
    path.write_text(json.dumps(data, indent=2) + '\n')
    path.chmod(0o600)


def probe():
    # Local gateway only: no redirects to an arbitrary host.
    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, *args, **kwargs):
            return None
    client = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    for path, content_type in (('/', 'text/html'), ('/api/health/ready', 'application/json')):
        request = urllib.request.Request('http://127.0.0.1:8082' + path,
                                         headers={'Host': 'taskflow.hancongzhang.com'})
        with client.open(request, timeout=10) as response:
            if response.status != 200 or content_type not in response.headers.get('Content-Type', ''):
                raise RuntimeError('gateway health check failed')


def activate(path):
    # --no-deps deliberately leaves PostgreSQL and the completed migration job alone.
    compose(path, 'up', '-d', '--no-deps', '--no-build', '--pull', 'never',
            '--wait', '--wait-timeout', '120', 'api', 'web', timeout=180)
    compose(path, 'exec', '-T', 'web', 'nginx', '-t')
    probe()



def activate_with_rollback(candidate, previous):
    """Execute the real Compose change and restore prior containers on failure."""
    try:
        activate(candidate)
    except Exception:
        print('Deployment failed; restoring previous API/web images', flush=True)
        try:
            activate(previous)
        except Exception as rollback_error:
            raise RuntimeError(f'ROLLBACK FAILED; inspect {previous.parent} locally') from rollback_error
        raise RuntimeError('deployment failed; previous images restored')


def deploy(data):
    base_path = ROOT / 'compose.server.yaml'
    state = ROOT / '.local/deploy/cd'
    state.mkdir(mode=0o700, parents=True, exist_ok=True)
    with (state / 'deploy.lock').open('a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        if (state / 'pending.json').exists():
            raise RuntimeError('previous deployment interrupted; inspect pending.json before retrying')
        config = json.loads(compose(base_path, 'config', '--format', 'json'))
        validate_config(config)
        old = snapshot(config, base_path)
        # All downloads and compatibility checks happen before any service changes.
        for service, image in data['images'].items():
            print(f'Pulling {service}', flush=True)
            run(['docker', 'pull', image], timeout=600)
            labels = json.loads(run(['docker', 'image', 'inspect', '--format', '{{json .Config.Labels}}', image])) or {}
            if labels.get('org.opencontainers.image.revision') != data['revision']:
                raise RuntimeError('image revision does not match approved commit')
        if migration_hashes(old['services']['migrate']['image']) != migration_hashes(data['images']['migrate']):
            raise RuntimeError('database migrations changed: automatic deployment refused; plan migration separately')
        # Verify the existing stack is healthy before starting the change.
        compose(base_path, 'exec', '-T', 'api', '/api', 'healthcheck')
        probe()
        release = state / (time.strftime('%Y%m%dT%H%M%SZ', time.gmtime()) + '-' + uuid.uuid4().hex[:8])
        release.mkdir(mode=0o700)
        previous = release / 'previous.json'
        candidate = release / 'candidate.json'
        write_json(previous, old)
        new = copy.deepcopy(config)
        for service in ('api', 'web'):
            new['services'][service]['image'] = data['images'][service]
            new['services'][service].pop('build', None)
        # Migration job remains the actually applied baseline, not a newly run job.
        new['services']['migrate']['image'] = old['services']['migrate']['image']
        new['services']['migrate'].pop('build', None)
        write_json(candidate, new)
        write_json(release / 'request.json', data)
        compose(candidate, 'config', '--quiet')
        # Crash marker allows an operator to distinguish an interrupted deployment.
        write_json(state / 'pending.json', {'release': str(release)})
        try:
            activate_with_rollback(candidate, previous)
        except RuntimeError as error:
            if str(error) == 'deployment failed; previous images restored':
                write_json(release / 'result.json', {'status': 'rolled_back'})
                (state / 'pending.json').unlink()
            raise
        # Keep the operator-facing Compose file in sync with the deployed images.
        # JSON is valid YAML, accepted by Compose; files contain paths, never secret values.
        temp = ROOT / 'compose.server.yaml.next'
        write_json(temp, new)
        os.replace(temp, base_path)
        write_json(release / 'result.json', {'status': 'deployed', 'revision': data['revision']})
        (state / 'pending.json').unlink()
        print(f'Deployed {data["revision"]}; rollback record: {previous}', flush=True)


def main():
    if os.environ.get('SSH_ORIGINAL_COMMAND') != 'deploy':
        raise RuntimeError('this SSH key accepts only the deploy command')
    raw = sys.stdin.buffer.read(8193)
    if len(raw) > 8192:
        raise ValueError('request too large')
    os.umask(0o077)
    deploy(validate_request(json.loads(raw)))


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        # Error classes from JSON/HTTP/OS may include sensitive details; sanitize.
        print(str(error) if isinstance(error, RuntimeError) else f'Deployment refused/failed: {type(error).__name__}', file=sys.stderr)
        sys.exit(1)
