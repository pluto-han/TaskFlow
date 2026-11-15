"""Run bounded k6 smoke/baseline profiles against a disposable local stack."""
import argparse
from collections import Counter
from contextlib import contextmanager
from datetime import date, datetime, timedelta, timezone
import http.cookiejar
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import platform
import re
import secrets
import shutil
import signal
import socket
import ssl
import subprocess
import tempfile
import time
import urllib.request
import urllib.error

from scripts import init_cache, init_local_stack

ROOT = Path(__file__).resolve().parents[1]
K6_IMAGE = 'grafana/k6:1.3.0@sha256:3ddc8b1a33a2c3d8edc6e99b6a762ae36cba08788463458f5e6a7703e14eb77d'
LABEL = 'taskflow.load-run'
PROFILES = {'smoke': 30, 'baseline': 300, 'step': 120}
CACHE_MODES = ('off', 'cold', 'warm')


@contextmanager
def fixture_directory(local, project):
    directory = Path(tempfile.mkdtemp(prefix=project + '-', dir=local))
    directory.chmod(0o700)
    try:
        yield directory
    finally:
        if (directory / 'cleanup-complete').exists():
            shutil.rmtree(directory)
        else:
            # Keep bind sources and Compose material if cleanup is interrupted.
            print('Fixture retained for cleanup: ' + str(directory), flush=True)


def run(args, *, timeout=240):
    result = subprocess.run(args, cwd=ROOT, capture_output=True, text=True, timeout=timeout)
    if result.returncode:
        # Never print subprocess arguments: future commands may contain secrets.
        raise RuntimeError(f'{args[0]} failed (exit {result.returncode}); credentials omitted')
    return result.stdout.strip()


def local_docker():
    context = run(['docker', 'context', 'inspect', '--format', '{{.Endpoints.docker.Host}}'])
    endpoint = context if os.environ.get('DOCKER_CONTEXT') else os.environ.get('DOCKER_HOST') or context
    if not endpoint.startswith('unix://'):
        raise RuntimeError('Only a local Docker Unix socket is allowed')


def free_port():
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        return sock.getsockname()[1]


def assert_owned(details, marker, project):
    labels = details['Config'].get('Labels') or {}
    if labels.get(LABEL) != marker or labels.get('com.docker.compose.project') != project:
        raise RuntimeError('Container ownership mismatch; refusing operation')


def verify_target(details, marker, project, port):
    assert_owned(details, marker, project)
    bindings = details['NetworkSettings']['Ports'].get('8443/tcp')
    if bindings != [{'HostIp': '127.0.0.1', 'HostPort': str(port)}]:
        raise RuntimeError('Unexpected load target port binding')
    return f'https://localhost:{port}'


def resource_guard(rows, meminfo, free_disk):
    available = int(next(line.split()[1] for line in meminfo.splitlines()
                         if line.startswith('MemAvailable:'))) * 1024
    if available < 1024 ** 3 or free_disk < 5 * 1024 ** 3:
        raise RuntimeError('Host memory/disk safety budget reached')
    return any(float(row['MemPerc'].rstrip('%')) > 90 for row in rows)


def compose_model(directory, marker, ports, cache_mode='cold'):
    if cache_mode not in CACHE_MODES:
        raise ValueError('Unsupported cache mode')
    model = json.loads(run(['docker', 'compose', '-f', 'compose.production.yaml', 'config', '--format', 'json']))
    project = 'taskflow-load-' + marker
    model['name'] = project
    network_ids = run(['docker', 'network', 'ls', '-q']).split()
    networks = json.loads(run(['docker', 'network', 'inspect', *network_ids])) if network_ids else []
    used = [ipaddress.ip_network(item['Subnet']) for net in networks
            for item in net.get('IPAM', {}).get('Config') or [] if item.get('Subnet')]
    subnet = next((ipaddress.ip_network(f'10.204.{i}.0/24') for i in range(256)
                   if not any(ipaddress.ip_network(f'10.204.{i}.0/24').overlaps(old) for old in used)), None)
    if subnet is None:
        raise RuntimeError('No disposable subnet available')
    for net in model['networks'].values():
        net.pop('name', None)
    # Dynamic monitoring addresses must not take the reserved web/API addresses
    # while those services are waiting for migration/health dependencies.
    model['networks']['edge']['ipam']['config'] = [
        {'subnet': str(subnet), 'ip_range': str(list(subnet.subnets(new_prefix=25))[1])}]
    for volume in model['volumes'].values():
        volume.pop('name', None)
    files = {'db_cert': 'db.crt', 'db_key': 'db.key', 'db_ca': 'ca.crt',
             'web_cert': 'web.crt', 'web_key': 'web.key'}
    for key in model['secrets']:
        model['secrets'][key]['file'] = str(directory / 'secrets' / files.get(key, key))
    api, web = model['services']['api'], model['services']['web']
    web['networks']['edge']['ipv4_address'] = str(subnet[2])
    api['networks']['edge']['ipv4_address'] = str(subnet[3])
    # Replace interpolated environment completely: no Discord/mail credentials survive.
    api['environment'] = {
        'DATABASE_URL_FILE': '/run/secrets/app_url', 'TASKFLOW_ENV': 'production',
        'TASKFLOW_HTTP_ADDR': '0.0.0.0:9090', 'GIN_MODE': 'release',
        'TASKFLOW_PUBLIC_ORIGIN': f'https://localhost:{ports["web"]}',
        'TASKFLOW_TRUSTED_PROXIES': f'{subnet[2]}/32,127.0.0.1/32',
        'TASKFLOW_RECURRENCE_ENABLED': 'true', 'TASKFLOW_DISCORD_SENDER_ENABLED': 'false',
        'TASKFLOW_MAIL_ENABLED': 'false', 'TASKFLOW_CACHE_ENABLED': str(cache_mode != 'off').lower(),
        'REDIS_ADDR': 'redis:6379', 'REDIS_USERNAME': 'taskflow_cache',
        'REDIS_PASSWORD_FILE': '/run/secrets/redis_password',
        'TASKFLOW_METRICS_ADDR': f'{subnet[3]}:9091',
    }
    cache = json.loads(run(['docker', 'compose', '-f', 'compose.cache.yaml', 'config', '--format', 'json']))
    model['services']['redis'] = cache['services']['redis']
    model['services']['redis']['networks'] = {'database': {}}
    for key, filename in [('redis_acl', 'users.acl'), ('redis_password', 'redis_password')]:
        model['secrets'][key] = {'file': str(directory / 'cache' / filename)}
    api['secrets'].append({'source': 'redis_password', 'target': 'redis_password'})
    api['depends_on']['redis'] = {'condition': 'service_healthy'}
    # Contrib instrumentation belongs only to this disposable candidate.
    postgres = model['services']['postgres']
    postgres['command'][-1] = postgres['command'][-1].rstrip() + ' -c shared_preload_libraries=pg_stat_statements -c pg_stat_statements.track=top'
    web['ports'] = [{'host_ip': '127.0.0.1', 'target': 8443, 'published': str(ports['web']), 'protocol': 'tcp'}]
    monitoring = json.loads(run(['docker', 'compose', '-f', 'compose.monitoring.yaml', 'config', '--format', 'json']))
    for name in ('prometheus', 'grafana'):
        service = monitoring['services'][name]
        service['networks'] = {'edge': {}, 'ingress': {}}
        model['services'][name] = service
    prometheus = model['services']['prometheus']
    prometheus['command'].append('--web.enable-remote-write-receiver')
    prometheus['volumes'] = [
        {'type': 'bind', 'source': str(directory / 'prometheus.json'), 'target': '/etc/prometheus/prometheus.yml', 'read_only': True},
        {'type': 'volume', 'source': 'prometheus_data', 'target': '/prometheus'},
    ]
    prometheus['ports'] = [{'host_ip': '127.0.0.1', 'target': 9090, 'published': str(ports['prometheus']), 'protocol': 'tcp'}]
    grafana = model['services']['grafana']
    grafana['ports'] = [{'host_ip': '127.0.0.1', 'target': 3000, 'published': str(ports['grafana']), 'protocol': 'tcp'}]
    model['secrets']['grafana_password'] = {'file': str(directory / 'grafana_password')}
    model['volumes'].update({'prometheus_data': {}, 'grafana_data': {}})
    for service in model['services'].values():
        service['labels'] = {LABEL: marker}
        service['restart'] = 'no'
        service['logging'] = {'driver': 'json-file', 'options': {'max-size': '5m', 'max-file': '2'}}
    return model


class PreparationPacer:
    """One shared clock across accounts: preparation stays below gateway limits."""
    def __init__(self):
        self.next_request = 0

    def wait(self):
        time.sleep(max(0, self.next_request - time.monotonic()))
        self.next_request = time.monotonic() + 0.35


class FixtureClient:
    def __init__(self, origin, tls, pacer):
        self.origin = origin
        self.pacer = pacer
        self.jar = http.cookiejar.CookieJar()
        self.client = urllib.request.build_opener(
            urllib.request.HTTPSHandler(context=tls), urllib.request.HTTPCookieProcessor(self.jar))
        self.csrf = None

    def request(self, method, path, data=None, expected=200):
        # Read retries are unnecessary here. A 429 means the request was rejected
        # before processing, so this is the sole preparation retry case.
        for attempt in range(4):
            self.pacer.wait()
            headers = {'Origin': self.origin, 'Content-Type': 'application/json'}
            if self.csrf:
                headers['X-CSRF-Token'] = self.csrf
            req = urllib.request.Request(self.origin + path, method=method, headers=headers,
                                         data=json.dumps(data).encode() if data is not None else None)
            try:
                response = self.client.open(req, timeout=10)
            except urllib.error.HTTPError as error:
                if error.code == 429 and attempt < 3:
                    retry = error.headers.get('Retry-After', '6')
                    error.close()
                    delay = int(retry) if retry.isdigit() else 6
                    print('Preparation rate limited; waiting within retry budget...', flush=True)
                    time.sleep(min(60, max(6, delay)))
                    continue
                code = error.code
                error.close()
                raise RuntimeError(f'Preparation {method} {path.split("?")[0]} failed with HTTP {code}; body omitted') from None
            with response:
                if response.status != expected:
                    raise RuntimeError('Unexpected preparation status; response omitted')
                return json.load(response)
        raise RuntimeError('Preparation retry budget exhausted')


def database_query(base, marker, project, sql):
    container = run(base + ['ps', '-q', 'postgres'])
    details = json.loads(run(['docker', 'inspect', container]))[0]
    assert_owned(details, marker, project)
    return run(base + ['exec', '-T', 'postgres', 'psql', '-X', '-U', 'taskflow_migrator',
        '-d', 'taskflow', '-At', '-v', 'ON_ERROR_STOP=1', '-c', sql])


def database_counts(base, marker, project):
    return json.loads(database_query(base, marker, project,
        "SELECT json_build_object('users',(SELECT count(*) FROM users),"
        "'tasks',(SELECT count(*) FROM tasks),'projects',(SELECT count(*) FROM projects),"
        "'memberships',(SELECT count(*) FROM project_memberships),"
        "'rules',(SELECT count(*) FROM recurrence_rules),"
        "'occurrences',(SELECT count(*) FROM recurrence_occurrences),"
        "'schema',(SELECT version FROM schema_migrations));"))


def sql_counts(base, marker, project):
    # SQL comments are emitted by sqlc. Calibration below refuses to proceed if
    # the server's representative statement text does not preserve these names.
    return json.loads(database_query(base, marker, project, """
        SELECT json_build_object(
          'task_list_calls', COALESCE(sum(calls) FILTER (WHERE query LIKE '-- name: ListTasks :many%'),0),
          'browser_session_calls', COALESCE(sum(calls) FILTER (WHERE query LIKE '-- name: GetActiveBrowserSession :one%'),0),
          'task_list_exec_ms', COALESCE(sum(total_exec_time) FILTER (WHERE query LIKE '-- name: ListTasks :many%'),0),
          'browser_session_exec_ms', COALESCE(sum(total_exec_time) FILTER (WHERE query LIKE '-- name: GetActiveBrowserSession :one%'),0),
          'all_application_calls', COALESCE(sum(calls),0))
        FROM pg_stat_statements
        WHERE dbid=(SELECT oid FROM pg_database WHERE datname=current_database())
          AND userid=(SELECT oid FROM pg_roles WHERE rolname='taskflow_app');
    """))


def clear_owned_cache(base, marker, project):
    container = run(base + ['ps', '-q', 'redis'])
    assert_owned(json.loads(run(['docker', 'inspect', container]))[0], marker, project)
    cli = base + ['exec', '-T', 'redis', 'sh', '-ec',
        'export REDISCLI_AUTH="$(cat /run/secrets/redis_password)"; exec redis-cli --user taskflow_load_observer "$@"',
        'load-fixture']
    keys = run(cli + ['--scan', '--pattern', 'taskflow:cache:*']).splitlines()
    if any(not key.startswith('taskflow:cache:') for key in keys):
        raise RuntimeError('Unexpected cache key namespace; refusing deletion')
    if keys:
        run(cli + ['DEL', *keys])
    if run(cli + ['--scan', '--pattern', 'taskflow:cache:*']):
        raise RuntimeError('Cold cache preparation did not empty the owned namespace')
    return len(keys)


def parse_cache_counters(text):
    counters = dict((outcome, float(value)) for outcome, value in re.findall(
        r'^taskflow_today_cache_reads_total\{outcome="([a-z]+)"\} ([0-9.eE+\-]+)$', text, re.M))
    fills = dict((outcome, float(value)) for outcome, value in re.findall(
        r'^taskflow_today_cache_fills_total\{outcome="([a-z]+)"\} ([0-9.eE+\-]+)$', text, re.M))
    enabled = re.search(r'^taskflow_today_cache_enabled ([01])$', text, re.M)
    if enabled is None or set(counters) != {'hit', 'miss', 'bypass', 'error', 'other'} or set(fills) != {'success', 'error', 'skipped', 'other'}:
        raise RuntimeError('Cache instrumentation missing from private scrape')
    return {'enabled': bool(int(enabled[1])), 'reads': counters, 'fills': fills}


def cache_counters(base):
    # Scrape through the existing container, avoiding Prometheus interval lag.
    return parse_cache_counters(run(base + ['exec', '-T', 'prometheus', 'wget', '-q', '-O', '-', 'http://api:9091/metrics']))


def cache_delta(before, after):
    if before['enabled'] != after['enabled']:
        raise RuntimeError('Cache mode changed during measurement')
    result = {group: {key: after[group][key] - value for key, value in before[group].items()}
              for group in ('reads', 'fills')}
    if any(value < 0 for group in result.values() for value in group.values()):
        raise RuntimeError('Cache counters reset during measurement')
    return result


def prepare_cache(base, marker, project, mode, origin, ca, users):
    removed = clear_owned_cache(base, marker, project)
    initial = cache_counters(base)
    warmed = 0
    if mode == 'warm':
        tls = ssl.create_default_context(cafile=str(ca))
        pacer = PreparationPacer()
        for user in users:
            pacer.wait()
            request = urllib.request.Request(origin + '/api/tasks/today?limit=20', headers={
                'Origin': origin, 'Cookie': '__Host-taskflow_session=' + user['cookie']})
            with urllib.request.urlopen(request, context=tls, timeout=10) as response:
                data = json.load(response)
            if len(data['items']) != 20 or any(not item['title'].startswith(user['title_prefix']) for item in data['items']):
                raise RuntimeError('Warmup account scope check failed')
            warmed += 1
    before = cache_counters(base)
    if before['enabled'] != (mode != 'off'):
        raise RuntimeError('Runtime cache setting differs from requested mode')
    if mode == 'warm' and cache_delta(initial, before)['fills']['success'] != len(users):
        raise RuntimeError('Prewarm cache fills incomplete; refusing warm label')
    return {'mode': mode, 'removed_keys': removed, 'prewarmed_accounts': warmed,
            'boundary_counters': before, 'prepared_at': datetime.now(timezone.utc).isoformat()}


def write_summary(report, metadata):
    summary_file = report / 'summary.json'
    if not summary_file.exists():
        (report / 'summary.md').write_text(
            '# Isolated load test\n\nStatus: ' + metadata['status'] +
            '\n\nNo completed k6 summary. Inspect environment.json and k6.log if present.\n')
        return
    metrics = json.loads(summary_file.read_text())['metrics']
    operations, statuses = Counter(), Counter()
    samples = report / 'samples.json'
    incomplete = 0
    if samples.exists():
        with samples.open() as source:
            for line in source:
                try:
                    item = json.loads(line)
                except json.JSONDecodeError:
                    incomplete += 1
                    continue
                if item.get('type') == 'Point' and item.get('metric') == 'business_status':
                    point = item['data']
                    operations[point['tags']['operation']] += int(point['value'])
                    statuses[point['tags']['status']] += int(point['value'])
    (report / 'operations.json').write_text(json.dumps({
        'operations': operations, 'http_statuses': statuses, 'incomplete_sample_lines': incomplete}, indent=2))
    def value(name, key):
        return metrics.get(name, {}).get('values', {}).get(key, 'unavailable')
    lines = ['# Isolated load test', '', f'Status: {metadata["status"]}',
             f'Profile: {metadata["profile"]}; measurement: {metadata["duration_seconds"]} seconds.', '',
             'Initial cache mode: ' + metadata.get('cache_mode', 'not recorded'), '',
             'Workload: ' + metadata.get('workload', 'mixed'), '',
             'Target aggregate RPS: ' + str(metadata.get('rps', 1)), '',
             '| Metric | Value |', '| --- | --- |',
             f'| Iterations | {value("iterations", "count")} |',
             f'| Business success rate (0–1) | {value("business_success", "rate")} |',
             f'| HTTP failure rate (0–1) | {value("http_req_failed", "rate")} |',
             f'| Dropped iterations | {value("dropped_iterations", "count")} |',
             f'| Successful read p95 (ms) | {value("read_duration", "p(95)")} |',
             f'| All read attempts p95 (ms) | {value("read_attempt_duration", "p(95)")} |', '',
             '## Actual operations', '', '| Operation | Requests |', '| --- | ---: |']
    lines += [f'| {name} | {count} |' for name, count in sorted(operations.items())]
    if 'cache_measurement' in metadata:
        cache = metadata['cache_measurement']
        sql = metadata['measurement_sql']
        lines += ['', '## Cache comparison', '',
                  'Requested initial mode: ' + metadata['cache_mode'],
                  'Prewarmed accounts: ' + str(metadata['cache_preparation']['prewarmed_accounts']),
                  'Measurement cache read outcomes: ' + json.dumps(cache['reads']),
                  'Measurement cache fill outcomes: ' + json.dumps(cache['fills']),
                  'Task list SQL calls (all list scopes): ' + str(sql['task_list_calls']),
                  'Browser session SQL calls: ' + str(sql['browser_session_calls']),
                  'All application SQL calls (includes background/monitoring): ' + str(sql['all_application_calls']),
                  'SQL execution milliseconds are server execution time, not end-to-end request latency.',
                  'Warm means prewarmed at the start; 12–15s TTL and writes still cause misses.',
                  'Preparation, login and prewarm are excluded from the boundary counter/SQL counts.']
    lines += ['', 'HTTP status counts: ' + json.dumps(statuses),
              'Seed counts: ' + json.dumps(metadata.get('seed_counts')),
              'Final counts: ' + json.dumps(metadata.get('final_database_counts')),
              'Cleanup: ' + metadata.get('cleanup', 'not confirmed'), '',
              'Preparation/login are excluded from measurement. HTTP duration excludes DNS, connection and TLS setup.',
              'This is a local controlled-load test, not a VPS capacity measurement.',
              'Missing or partial evidence is not a passing result. See environment.json for execution status.']
    (report / 'summary.md').write_text('\n'.join(lines) + '\n')


def prepare_users(origin, ca, query):
    users, clients = [], []
    tls = ssl.create_default_context(cafile=str(ca))
    plan_date = None
    started = time.monotonic()
    pacer = PreparationPacer()
    for index in range(10):
        client = FixtureClient(origin, tls, pacer)
        credentials = {'email': f'load-{index}@example.com', 'password': secrets.token_urlsafe(24)}
        client.request('POST', '/api/auth/register', credentials, expected=201)
        login = client.request('POST', '/api/auth/browser/login', credentials)
        client.csrf = login['csrf_token']
        today = client.request('GET', '/api/tasks/today')
        current_date = date.fromisoformat(today['plan_date']).isoformat()
        if plan_date is not None and plan_date != current_date:
            raise RuntimeError('Date changed during preparation; rerun')
        plan_date = current_date
        prefix = f'Load account {index} '
        targets = []
        # All tasks go through application validation. Historical fixtures are
        # re-dated below only in the ownership-verified disposable database.
        for item in range(40):
            category = 'today' if item < 20 else 'history' if item < 30 else 'ordinary'
            path = '/api/tasks' if category == 'ordinary' else '/api/tasks/today'
            payload = {'title': f'{prefix}{category} {item}', 'priority': item % 3 + 1}
            if category != 'ordinary':
                payload['plan_date'] = plan_date
            task = client.request('POST', path, payload, expected=201)
            if category == 'today':
                targets.append({'id': task['id'], 'version': task['version']})
        client.request('POST', '/api/recurrences', {'title': prefix + 'recurring',
            'priority': 2, 'anchor_date': plan_date, 'interval_days': 3,
            'reminder_enabled': False}, expected=201)
        users.append({'cookie': next(c.value for c in client.jar if c.name == '__Host-taskflow_session'),
                      'csrf': client.csrf, 'title_prefix': prefix, 'id': login['user']['id'],
                      'update_targets': targets})
        clients.append(client)
        print(f'Prepared account {index + 1}/10 (40 tasks, 1 recurrence rule)', flush=True)

    yesterday = (date.fromisoformat(plan_date) - timedelta(days=1)).isoformat()
    # There is intentionally no public API for creating a past Today task.
    changed = query("WITH moved AS (UPDATE tasks SET plan_date=DATE '" + yesterday +
        "' WHERE title ~ '^Load account [0-9] history [0-9]+$' AND recurrence_id IS NULL "
        "RETURNING id) SELECT count(*) FROM moved;")
    if int(changed) != 100:
        raise RuntimeError('Historical fixture row count mismatch')
    for group in range(2):
        owner_index = group * 5
        owner = clients[owner_index]
        project = owner.request('POST', '/api/projects', {'name': f'Load project {group}'}, expected=201)
        for index in range(owner_index + 1, owner_index + 5):
            invite = owner.request('POST', f'/api/projects/{project["id"]}/invitations',
                                   {'user_id': users[index]['id']}, expected=201)
            clients[index].request('POST', '/api/invitations/accept', {'token': invite['token']})
        prefix = f'Load project {group} '
        for item in range(20):
            owner.request('POST', '/api/tasks', {'title': prefix + str(item),
                'priority': item % 3 + 1, 'project_id': project['id']}, expected=201)
        for index in range(owner_index, owner_index + 5):
            users[index].update({'project_id': project['id'], 'project_prefix': prefix})
        print(f'Prepared shared project {group + 1}/2 (owner, 4 members, 20 tasks)', flush=True)

    deadline = time.monotonic() + 65
    while int(query('SELECT count(*) FROM recurrence_occurrences;')) != 10:
        if time.monotonic() > deadline:
            raise RuntimeError('Scheduler did not generate all ten occurrences')
        time.sleep(3)
    for user, client in zip(users, clients):
        today = client.request('GET', '/api/tasks/today?limit=10')
        if today['plan_date'] != plan_date or not today['next_cursor']:
            raise RuntimeError('Date changed or pagination fixture is incomplete')
        user['today_cursor'] = today['next_cursor']
        history = client.request('GET', '/api/tasks?plan_date=' + yesterday)
        if len(history) != 10 or any(item['plan_date'] != yesterday for item in history):
            raise RuntimeError('Historical fixture visibility mismatch')
        shared = client.request('GET', '/api/tasks?project_id=' + user['project_id'])
        if len(shared) != 20 or any(item['project_id'] != user['project_id'] for item in shared):
            raise RuntimeError('Shared fixture visibility mismatch')
    return users, plan_date, yesterday, time.monotonic() - started


def cleanup(base, marker, project):
    # Verify every existing container before deleting any resources.
    if not re.fullmatch(r'[a-f0-9]{20}', marker) or project != 'taskflow-load-' + marker:
        raise RuntimeError('Invalid disposable project identity')
    ids = run(['docker', 'ps', '-aq', '--filter', f'label=com.docker.compose.project={project}']).split()
    if ids:
        for details in json.loads(run(['docker', 'inspect', *ids])):
            assert_owned(details, marker, project)
    run(base + ['down', '--volumes', '--remove-orphans'], timeout=120)


def execute():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--profile', choices=PROFILES, default='smoke')
    parser.add_argument('--cache-mode', choices=CACHE_MODES, default='cold')
    parser.add_argument('--workload', choices=('mixed', 'today-repeat'), default='mixed')
    parser.add_argument('--rps', type=int, choices=(1, 2, 4, 8, 16), default=1)
    args = parser.parse_args()
    profile, cache_mode = args.profile, args.cache_mode
    workload = args.workload
    rps = args.rps
    if profile != 'step' and rps != 1:
        parser.error('Higher rates require the bounded step profile')
    duration = PROFILES[profile]
    if platform.system() != 'Linux':
        raise RuntimeError('This first fixture requires Linux host networking for k6')
    for tool in ('docker', 'openssl'):
        if not shutil.which(tool):
            raise RuntimeError('Missing tool: ' + tool)
    local_docker()
    marker = secrets.token_hex(10)
    project = 'taskflow-load-' + marker
    report = ROOT / 'artifacts' / 'lesson72' / marker
    report.mkdir(parents=True, mode=0o700)
    os.chmod(report, 0o700)
    local = ROOT / '.local'
    local.mkdir(exist_ok=True, mode=0o700)
    metadata = {'run_id': marker, 'profile': profile, 'cache_mode': cache_mode, 'workload': workload,
                'rps': rps, 'duration_seconds': duration,
                'started_at': datetime.now(timezone.utc).isoformat(),
                'revision': run(['git', 'rev-parse', 'HEAD']), 'dirty': run(['git', 'status', '--short']),
                'host': platform.platform(), 'cpu_count': os.cpu_count(), 'k6_image': K6_IMAGE,
                'status': 'preparing', 'scope': '10 accounts, personal/history/shared/recurring tasks; TLS gateway; no capacity claim'}
    base = None
    k6_name = project + '-k6'
    process = None
    previous = signal.signal(signal.SIGTERM, lambda *_: sys.exit(143))
    try:
        with fixture_directory(local, project) as directory:
            init_local_stack.prepare(directory / 'secrets')
            init_cache.prepare(directory / 'cache')
            # Test-only namespace maintenance account; application ACL unchanged.
            password = (directory / 'cache' / 'redis_password').read_text().strip()
            with (directory / 'cache' / 'users.acl').open('a') as acl:
                acl.write('user taskflow_load_observer reset on #' + hashlib.sha256(password.encode()).hexdigest() +
                          ' ~taskflow:cache:* -@all +hello +ping +scan +del\n')
            (directory / 'grafana_password').write_text(secrets.token_urlsafe(24))
            (directory / 'grafana_password').chmod(0o644)
            ports = {name: free_port() for name in ('web', 'prometheus', 'grafana')}
            (directory / 'prometheus.json').write_text(json.dumps({
                'global': {'scrape_interval': '5s'}, 'scrape_configs': [
                    {'job_name': 'taskflow', 'static_configs': [{'targets': ['api:9091']}]}]}))
            (directory / 'prometheus.json').chmod(0o644)
            candidate = directory / 'compose.json'
            model = compose_model(directory, marker, ports, cache_mode)
            candidate.write_text(json.dumps(model))
            base = ['docker', 'compose', '--project-directory', str(ROOT), '-p', project, '-f', str(candidate)]
            try:
                run(base + ['config', '--quiet'])
                print('Building the current worktree in isolated image tags...', flush=True)
                for name in ('api', 'migrate', 'web'):
                    model['services'][name]['image'] = f'{project}-{name}:test'
                candidate.write_text(json.dumps(model))
                run(base + ['build'], timeout=600)
                run(['docker', 'pull', K6_IMAGE], timeout=180)
                run(base + ['up', '-d', '--no-build', '--wait', '--wait-timeout', '180'])
                ids = run(base + ['ps', '-aq']).split()
                containers = json.loads(run(['docker', 'inspect', *ids]))
                for item in containers:
                    assert_owned(item, marker, project)
                web = next(item for item in containers if item['Config']['Labels']['com.docker.compose.service'] == 'web')
                origin = verify_target(web, marker, project, ports['web'])
                metadata['images'] = {item['Config']['Labels']['com.docker.compose.service']: item['Image'] for item in containers}
                metadata['limits'] = {item['Config']['Labels']['com.docker.compose.service']:
                                      {'memory': item['HostConfig']['Memory'], 'nano_cpus': item['HostConfig']['NanoCpus']} for item in containers}
                print('Isolated stack healthy; preparing ten Cookie/CSRF accounts (separate from measurement)...', flush=True)
                query_db = lambda sql: database_query(base, marker, project, sql)
                query_db('CREATE EXTENSION IF NOT EXISTS pg_stat_statements;')
                users, plan_date, history_date, elapsed = prepare_users(
                    origin, directory / 'secrets' / 'ca.crt', query_db)
                counts = database_counts(base, marker, project)
                if any(counts[key] != value for key, value in
                       {'users': 10, 'tasks': 450, 'projects': 2, 'rules': 10, 'occurrences': 10}.items()):
                    raise RuntimeError('Seeded database counts differ from workload contract')
                metadata.update({'preparation_seconds': elapsed, 'plan_date': plan_date,
                                 'history_date': history_date, 'seed_counts': counts})
                calibrated = sql_counts(base, marker, project)
                if calibrated['task_list_calls'] <= 0 or calibrated['browser_session_calls'] <= 0:
                    raise RuntimeError('SQL fingerprint calibration failed; refusing unsupported SQL counts')
                metadata['sql_calibration'] = calibrated
                print(f'Preparing cache mode: {cache_mode}; login/seed/prewarm are outside measurement...', flush=True)
                cache_preparation = prepare_cache(base, marker, project, cache_mode, origin,
                                                   directory / 'secrets' / 'ca.crt', users)
                metadata['cache_preparation'] = cache_preparation
                query_db('SELECT pg_stat_statements_reset();')
                zero = sql_counts(base, marker, project)
                if zero['task_list_calls'] != 0 or zero['browser_session_calls'] != 0:
                    raise RuntimeError('SQL counters were not reset before measurement')
                (directory / 'manifest.json').write_text(json.dumps({'run_id': marker, 'profile': profile,
                    'cache_mode': cache_mode, 'workload': workload, 'rps': rps, 'origin': origin, 'users': users,
                    'plan_date': plan_date, 'history_date': history_date}))
                for filename in ('manifest.json', 'prometheus.json'):
                    (directory / filename).chmod(0o644)
                # Only this fixture directory and public CA are mounted, never host env/secrets.
                command = ['docker', 'run', '--name', k6_name, '--network', 'host', '--label', f'{LABEL}={marker}',
                    '--user', f'{os.getuid()}:{os.getgid()}', '--read-only', '--cap-drop', 'ALL',
                    '--security-opt', 'no-new-privileges:true', '--memory', '256m', '--cpus', '1', '--pids-limit', '64',
                    '-v', f'{directory / "manifest.json"}:/fixture/manifest.json:ro',
                    '-v', f'{directory / "secrets" / "ca.crt"}:/fixture/ca.crt:ro',
                    '-v', f'{ROOT / "test/load/k6/tasks.js"}:/scripts/tasks.js:ro',
                    '-v', f'{ROOT / "test/load/k6/workload.mjs"}:/scripts/workload.mjs:ro',
                    '-v', f'{report}:/results', '-e', 'SSL_CERT_FILE=/fixture/ca.crt',
                    '-e', f'K6_PROMETHEUS_RW_SERVER_URL=http://127.0.0.1:{ports["prometheus"]}/api/v1/write',
                    '-e', 'K6_PROMETHEUS_RW_TREND_STATS=p(50),p(95),p(99)', K6_IMAGE,
                    'run', '--out', 'experimental-prometheus-rw', '--out', 'json=/results/samples.json', '/scripts/tasks.js']
                print(f'Running k6 {profile}: {duration} seconds, aggregate {rps} requests/second...', flush=True)
                with (report / 'k6.log').open('w') as log, (report / 'resources.jsonl').open('w') as samples:
                    process = subprocess.Popen(command, stdout=log, stderr=subprocess.STDOUT, cwd=ROOT)
                    measured_start = time.monotonic()
                    deadline = measured_start + duration + 60
                    progress_at = measured_start + 30
                    memory_high_since = None
                    while process.poll() is None:
                        if time.monotonic() > deadline:
                            raise RuntimeError('Load measurement exceeded duration plus 60-second watchdog')
                        generator_ids = run(['docker', 'ps', '-aq', '--filter', f'name=^/{k6_name}$']).split()
                        stats = run(['docker', 'stats', '--no-stream', '--format', '{{json .}}', *ids, *generator_ids], timeout=15)
                        current = json.loads(run(['docker', 'inspect', *ids]))
                        if any(item['State'].get('OOMKilled') for item in current):
                            raise RuntimeError('Fixture OOM: stopping load')
                        if any(item['State']['Status'] != 'running' for item in current
                               if item['Config']['Labels']['com.docker.compose.service'] != 'migrate'):
                            raise RuntimeError('Fixture service exited: stopping load')
                        rows = [json.loads(row) for row in stats.splitlines()]
                        meminfo = Path('/proc/meminfo').read_text()
                        if resource_guard(rows, meminfo, shutil.disk_usage(report).free):
                            memory_high_since = memory_high_since or time.monotonic()
                            if time.monotonic() - memory_high_since >= 30:
                                raise RuntimeError('Fixture memory above 90% for 30 seconds')
                        else:
                            memory_high_since = None
                        samples.write(json.dumps({'time': datetime.now(timezone.utc).isoformat(),
                            'containers': rows, 'meminfo': meminfo,
                            'vmstat': Path('/proc/vmstat').read_text()}) + '\n')
                        samples.flush()
                        if time.monotonic() >= progress_at:
                            print(f'Measurement progress: {int(time.monotonic() - measured_start)}/{duration} seconds', flush=True)
                            progress_at += 30
                        time.sleep(3)
                    if process.returncode:
                        metadata['threshold_failure'] = process.returncode == 99
                        raise RuntimeError('k6 execution/thresholds failed; see sanitized k6.log and summary.json')
                metadata['measurement_sql'] = sql_counts(base, marker, project)
                metadata['cache_measurement'] = cache_delta(cache_preparation['boundary_counters'], cache_counters(base))
                (report / 'sql-counts.json').write_text(json.dumps(metadata['measurement_sql'], indent=2))
                (report / 'cache-boundaries.json').write_text(json.dumps({
                    'before': cache_preparation['boundary_counters'], 'delta': metadata['cache_measurement']}, indent=2))
                def query(expression):
                    from urllib.parse import urlencode
                    with urllib.request.urlopen(f'http://127.0.0.1:{ports["prometheus"]}/api/v1/query?' + urlencode({'query': expression}), timeout=5) as response:
                        return json.load(response)
                for filename, expression in [('app-metrics', 'taskflow_http_requests_total'),
                                              ('pool-metrics', '{__name__=~"taskflow_db_pool_.+"}'),
                                              ('cache-metrics', 'taskflow_today_cache_reads_total'),
                                              ('k6-metrics', '{__name__=~"k6_.+"}')]:
                    data = query(expression)
                    if not data.get('data', {}).get('result'):
                        raise RuntimeError('Prometheus evidence missing: ' + filename)
                    (report / (filename + '.json')).write_text(json.dumps(data, indent=2))
                metadata['final_database_counts'] = database_counts(base, marker, project)
                metadata['status'] = 'passed'
                print(f'PASS: {profile} and application/k6/pool-cache Prometheus series', flush=True)
            except BaseException:
                existing = run(base + ['ps', '-aq']).split()
                if existing:
                    metadata['failure_services'] = [
                        {'service': item['Config']['Labels']['com.docker.compose.service'],
                         'status': item['State']['Status'], 'exit_code': item['State']['ExitCode'],
                         'runtime_error': item['State'].get('Error', ''),
                         'oom': item['State']['OOMKilled'],
                         'health': item['State'].get('Health', {}).get('Status')}
                        for item in json.loads(run(['docker', 'inspect', *existing]))]
                raise
            finally:
                # Docker CLI termination alone does not stop its running container.
                found = run(['docker', 'ps', '-aq', '--filter', f'name=^/{k6_name}$']).split()
                if found:
                    details = json.loads(run(['docker', 'inspect', *found]))[0]
                    if details['Config'].get('Labels', {}).get(LABEL) != marker:
                        raise RuntimeError('k6 ownership mismatch; refusing removal')
                    run(['docker', 'rm', '-f', *found])
                if process is not None:
                    process.wait(timeout=15)
                cleanup(base, marker, project)
                metadata['cleanup'] = 'owned containers/networks/volumes removed'
                (directory / 'cleanup-complete').touch()
    except BaseException as error:
        metadata['status'] = 'failed'
        metadata['error_type'] = type(error).__name__
        raise
    finally:
        signal.signal(signal.SIGTERM, previous)
        metadata['finished_at'] = datetime.now(timezone.utc).isoformat()
        (report / 'environment.json').write_text(json.dumps(metadata, indent=2) + '\n')
        write_summary(report, metadata)
        print('Report: ' + str(report.relative_to(ROOT)), flush=True)


if __name__ == '__main__':
    execute()
