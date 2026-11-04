"""Prepare files only; never start containers or overwrite server configuration."""
import json
import os
from pathlib import Path
import secrets
import subprocess

ROOT = Path(__file__).resolve().parents[1]


def main():
    os.umask(0o077)
    base = ROOT / 'compose.server.yaml'
    if not base.is_file():
        raise SystemExit('Requires the existing server compose.server.yaml; run on the server after code review')
    result = subprocess.run(['docker', 'compose', '-f', str(base), 'config', '--format', 'json'],
                            cwd=ROOT, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    if result.returncode:
        raise SystemExit('Cannot validate server configuration; inspect it locally')
    data = json.loads(result.stdout)
    api = data['services']['api']
    if data.get('name') != 'taskflow-server' or api.get('networks', {}).get('edge', {}).get('ipv4_address') != '172.30.23.3':
        raise SystemExit('Unexpected project/network: review configuration before enabling monitoring')
    if api.get('ports'):
        raise SystemExit('API must not publish host ports')
    directory = ROOT / '.local/monitoring'
    directory.mkdir(mode=0o700, parents=True, exist_ok=True)
    directory.chmod(0o700)
    candidate = directory / 'api.compose.json'
    if candidate.exists():
        raise SystemExit('Candidate exists; refusing to overwrite. Review existing files.')
    password = directory / 'grafana_password'
    if not password.exists():
        with password.open('x') as output:
            output.write(secrets.token_urlsafe(32) + '\n')
        # Compose bind secret must be readable by Grafana UID 472; parent stays 0700.
        password.chmod(0o644)
    elif not password.read_text().strip():
        raise SystemExit('Existing Grafana password is empty')
    api.setdefault('environment', {})['TASKFLOW_METRICS_ADDR'] = '172.30.23.3:9091'
    with candidate.open('x') as output:
        json.dump(data, output, indent=2)
        output.write('\n')
    print('Prepared .local/monitoring/api.compose.json and Grafana password file; no services changed')


if __name__ == '__main__':
    main()
