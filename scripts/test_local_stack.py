"""Validate the local container stack using only a unique disposable Compose project."""
import json
import ipaddress
import socket
from pathlib import Path
import secrets
import signal
import sys
import ssl
import subprocess
import time
import urllib.request
import urllib.error

ROOT = Path(__file__).resolve().parents[1]
NAME = 'taskflow-smoke-' + secrets.token_hex(5)
ARTIFACTS = ROOT / 'artifacts' / 'lesson23'
SECRETS = ROOT / '.local' / 'stack'
BASE = ['docker', 'compose', '-p', NAME, '-f', str(ROOT / 'compose.production.yaml')]


def run(args, check=True):
    result = subprocess.run(args, cwd=ROOT, capture_output=True, text=True, timeout=240)
    if check and result.returncode:
        # Compose config contains only secret paths, never secret values.
        print(result.stdout[-2000:])
        print(result.stderr[-2000:])
        raise RuntimeError('container verification command failed')
    return result


def main():
    global BASE
    signal.signal(signal.SIGTERM, lambda *_: sys.exit(143))
    if not SECRETS.exists():
        raise SystemExit('Run python3 scripts/init_local_stack.py first')
    ARTIFACTS.mkdir(parents=True, exist_ok=True)
    bad = ARTIFACTS / NAME
    bad.mkdir()
    # Isolate ports and subnet too: do not stop the user's existing local stack.
    model = json.loads(run(BASE + ['config', '--format', 'json']).stdout)
    ids = run(['docker', 'network', 'ls', '-q']).stdout.split()
    networks = json.loads(run(['docker', 'network', 'inspect', *ids]).stdout) if ids else []
    used = [ipaddress.ip_network(c['Subnet']) for network in networks
            for c in (network.get('IPAM', {}).get('Config') or []) if c.get('Subnet')]
    subnet = next((ipaddress.ip_network(f'10.203.{n}.0/24') for n in range(256)
                   if not any(ipaddress.ip_network(f'10.203.{n}.0/24').overlaps(old) for old in used)), None)
    if subnet is None:
        raise RuntimeError('no isolated test subnet available')
    with socket.socket() as reservation:
        reservation.bind(('127.0.0.1', 0))
        port = reservation.getsockname()[1]
    origin = f'https://localhost:{port}'
    for network in model['networks'].values(): network.pop('name', None)
    for volume in model['volumes'].values(): volume.pop('name', None)
    model['networks']['edge']['ipam']['config'] = [{'subnet':str(subnet)}]
    web_ip, api_ip = str(subnet[2]), str(subnet[3])
    model['services']['web']['networks']['edge']['ipv4_address'] = web_ip
    model['services']['api']['networks']['edge']['ipv4_address'] = api_ip
    model['services']['api']['environment']['TASKFLOW_TRUSTED_PROXIES'] = web_ip+'/32,127.0.0.1/32'
    model['services']['api']['environment']['TASKFLOW_PUBLIC_ORIGIN'] = origin
    model['services']['web']['ports'] = [{'host_ip':'127.0.0.1','published':str(port),'target':8443,'protocol':'tcp'}]
    isolated = bad / 'compose.json'
    isolated.write_text(json.dumps(model))
    BASE = ['docker', 'compose', '-p', NAME, '-f', str(isolated)]
    (bad / '000001_fail.up.sql').write_text('THIS IS AN INTENTIONAL INVALID MIGRATION;\n')
    override = bad / 'override.json'
    override.write_text(json.dumps({'services': {'migrate': {'volumes': [str(bad)+':/migrations:ro']}}}))
    failed = BASE + ['-f', str(override)]
    try:
        result = run(failed + ['up', '-d', '--no-build'], check=False)
        assert result.returncode != 0, 'invalid migration unexpectedly succeeded'
        rows = run(failed + ['ps', '-a', '--format', 'json']).stdout
        # Compose emits either a JSON array or newline-delimited objects.
        objects = json.loads(rows) if rows.lstrip().startswith('[') else [json.loads(x) for x in rows.splitlines() if x]
        migration = next((x for x in objects if x['Service']=='migrate'), None)
        if migration is None:
            print(result.stderr[-3000:])
            raise RuntimeError('migration container was not created; inspect startup prerequisites')
        if migration['ExitCode'] == 0:
            print(result.stderr[-3000:])
            print(rows)
        assert migration['ExitCode'] != 0, 'migration must fail'
        assert not any(x['Service']=='api' and x['State']=='running' for x in objects), 'API started after failed migration'
        print('PASS: failed migration blocks API startup', flush=True)
    finally:
        run(failed + ['down', '--volumes', '--remove-orphans'])
    try:
        run(BASE + ['up', '-d', '--no-build', '--wait', '--wait-timeout', '180'])
        tls = ssl.create_default_context(cafile=str(SECRETS / 'ca.crt'))
        def request(method, path, body=None, token=None, expected=200):
            headers = {'Content-Type': 'application/json', 'Origin': origin}
            if token:
                headers['Authorization'] = 'Bearer '+token
            req = urllib.request.Request(origin+path,
                    data=None if body is None else json.dumps(body).encode(), headers=headers, method=method)
            try:
                response = urllib.request.urlopen(req, context=tls, timeout=15)
            except urllib.error.HTTPError as error:
                response = error
            with response:
                assert response.status == expected, f'{method} {path}: status {response.status}, expected {expected}'
                raw = response.read()
                return json.loads(raw) if raw else None
        request('GET', '/api/health/ready')
        with urllib.request.urlopen(origin+'/tasks', context=tls) as page:
            assert b'<div id="root">' in page.read(), 'SPA fallback failed'
            assert page.headers['X-Frame-Options']=='DENY'
            assert "frame-ancestors 'none'" in page.headers['Content-Security-Policy']
        run(BASE + ['run', '--rm', '--no-deps', 'migrate'])
        print('PASS: repeated migration/grants are idempotent', flush=True)
        credentials = {'email': 'stack-smoke@example.com', 'password': 'local container smoke passphrase'}
        request('POST','/api/auth/register',credentials,expected=201)
        token = request('POST','/api/auth/login',credentials)['access_token']
        today = request('GET', '/api/tasks/today', token=token)
        assert today['timezone'] == 'Europe/Amsterdam'
        from datetime import datetime
        from zoneinfo import ZoneInfo
        accepted = datetime.fromisoformat(today['server_time'].replace('Z', '+00:00'))
        assert accepted.astimezone(ZoneInfo('Europe/Amsterdam')).date().isoformat() == today['plan_date']
        planned = request('POST', '/api/tasks/today', {
            'plan_date': today['plan_date'], 'title': 'Planned smoke task', 'priority': 2}, token, 201)
        assert planned['plan_date'] == today['plan_date'] and planned['project_id'] is None and not planned['can_move']
        assert any(x['id'] == planned['id'] for x in request('GET', '/api/tasks/today', token=token)['items'])
        request('PATCH', '/api/tasks/' + planned['id'], {'version': 1, 'completed': True}, token)
        print('PASS: scratch Amsterdam calendar and today CRUD using runtime role', flush=True)
        project = request('POST','/api/projects',{'name':'Persistent project'},token,201)
        task = request('POST','/api/tasks',{'title':'Persistent task','priority':2,'project_id':project['id']},token,201)
        run(BASE + ['restart', 'postgres', 'api', 'web'])
        for attempt in range(90):
            try:
                request('GET','/api/health/ready')
                break
            except Exception:
                time.sleep(1)
        else:
            raise RuntimeError('stack did not recover after restart')
        stored = request('GET','/api/tasks/'+task['id'],token=token)
        assert stored['title']=='Persistent task' and stored['version']==1
        from test_deploy_drill import drill
        drill(BASE, NAME, lambda: request('GET', '/api/tasks/'+task['id'], token=token))
        # Runtime role was checked at startup, migrations use a different login.
        role = run(BASE + ['exec','-T','postgres','psql','-X','-U','taskflow_migrator','-d','taskflow','-At','-c',
            "SELECT rolname||':'||rolsuper||':'||rolcreatedb FROM pg_roles WHERE rolname='taskflow_app'"]).stdout.strip()
        assert role == 'taskflow_app:false:false'
        for service in ('postgres','api'):
            cid=run(BASE+['ps','-q',service]).stdout.strip()
            bindings=json.loads(run(['docker','inspect',cid]).stdout)[0]['HostConfig']['PortBindings']
            assert not bindings, service+' exposed a host port'
        run(BASE+['stop','api'])
        log=run(BASE+['logs','--no-color','api']).stdout
        assert 'TaskFlow stopped' in log, 'missing graceful shutdown completion'
        cid=run(BASE+['ps','-a','-q','api']).stdout.strip()
        state=json.loads(run(['docker','inspect',cid]).stdout)[0]['State']
        assert state['ExitCode']==0 and not state['OOMKilled']
        print('PASS: HTTPS, runtime role, CRUD, restart persistence, private ports, graceful shutdown',flush=True)
        images=[]
        for image in ('taskflow-api:local','taskflow-migrate:local','taskflow-web:local'):
            details=json.loads(run(['docker','image','inspect',image]).stdout)[0]
            images.append({'image':image,'bytes':details['Size'],'user':details['Config']['User']})
        (ARTIFACTS/'images.json').write_text(json.dumps(images,indent=2)+'\n')
        print(json.dumps(images,ensure_ascii=False),flush=True)
    finally:
        run(BASE+['down','--volumes','--remove-orphans'])
    print('Only disposable test project removed; existing development volume untouched.')


if __name__ == '__main__':
    main()
