"""Safety checks for the disposable load runner; no Docker daemon required."""
import unittest
from pathlib import Path
import tempfile
from unittest.mock import patch
from unittest.mock import Mock
from types import SimpleNamespace

from scripts import test_load_stack as load


class LoadSafetyTests(unittest.TestCase):
    def fixture(self, marker='a' * 20):
        return {'Config': {'Labels': {load.LABEL: marker,
                'com.docker.compose.project': 'taskflow-load-' + marker}},
                'NetworkSettings': {'Ports': {'8443/tcp': [
                    {'HostIp': '127.0.0.1', 'HostPort': '24567'}]}}}

    def test_target_requires_ownership_and_exact_loopback_binding(self):
        marker = 'a' * 20
        project = 'taskflow-load-' + marker
        self.assertEqual(load.verify_target(self.fixture(), marker, project, 24567),
                         'https://localhost:24567')
        for field, value in [('HostIp', '0.0.0.0'), ('HostPort', '8443')]:
            fixture = self.fixture()
            fixture['NetworkSettings']['Ports']['8443/tcp'][0][field] = value
            with self.assertRaises(RuntimeError):
                load.verify_target(fixture, marker, project, 24567)
        with self.assertRaises(RuntimeError):
            load.verify_target(self.fixture('b' * 20), marker, project, 24567)

    def test_remote_docker_is_rejected(self):
        with patch.dict(load.os.environ, {}, clear=True), patch.object(load, 'run', return_value='ssh://host'):
            with self.assertRaises(RuntimeError):
                load.local_docker()

    def test_host_override_is_checked(self):
        with patch.dict(load.os.environ, {'DOCKER_HOST': 'tcp://remote:2375'}, clear=True), \
                patch.object(load, 'run', return_value='unix:///var/run/docker.sock'):
            with self.assertRaises(RuntimeError):
                load.local_docker()

    def test_cleanup_refuses_unowned_container_before_down(self):
        import json
        with patch.object(load, 'run', side_effect=['container', json.dumps([self.fixture('b' * 20)])]) as run:
            with self.assertRaises(RuntimeError):
                load.cleanup(['docker', 'compose'], 'a' * 20, 'taskflow-load-' + 'a' * 20)
            self.assertEqual(run.call_count, 2)

    def test_fixture_survives_failed_cleanup_but_success_removes_it(self):
        with tempfile.TemporaryDirectory() as tmp:
            with load.fixture_directory(Path(tmp), 'failed') as directory:
                (directory / 'compose.json').write_text('{}')
            self.assertTrue((directory / 'compose.json').is_file())
            with load.fixture_directory(Path(tmp), 'success') as successful:
                (successful / 'cleanup-complete').touch()
            self.assertFalse(successful.exists())

    def test_dynamic_addresses_do_not_overlap_reserved_static_ips(self):
        import json
        production = {'services': {
            'postgres': {'command': ['exec docker-entrypoint.sh postgres -c ssl=on\n']},
            'api': {'networks': {'edge': {}}, 'secrets': [], 'depends_on': {}},
            'web': {'networks': {'edge': {}}}},
            'networks': {'edge': {'ipam': {}}, 'database': {}, 'ingress': {}},
            'volumes': {}, 'secrets': {}}
        cache = {'services': {'redis': {}}}
        monitoring = {'services': {'prometheus': {'command': []}, 'grafana': {}}}
        responses = [json.dumps(production), '', json.dumps(cache), json.dumps(monitoring)]
        with patch.object(load, 'run', side_effect=responses):
            model = load.compose_model(Path('/fixture'), 'a' * 20,
                                       {'web': 12345, 'prometheus': 12346, 'grafana': 12347})
        import ipaddress
        pool = ipaddress.ip_network(model['networks']['edge']['ipam']['config'][0]['ip_range'])
        for name in ('api', 'web'):
            address = model['services'][name]['networks']['edge']['ipv4_address']
            self.assertNotIn(ipaddress.ip_address(address), pool)
        env = model['services']['api']['environment']
        self.assertEqual(env['TASKFLOW_MAIL_ENABLED'], 'false')
        self.assertEqual(env['TASKFLOW_DISCORD_SENDER_ENABLED'], 'false')
        command = model['services']['postgres']['command'][-1]
        self.assertIn('ssl=on -c shared_preload_libraries=pg_stat_statements', command)
        self.assertNotIn('\n -c', command)
        for mode in ('off', 'warm'):
            with patch.object(load, 'run', side_effect=responses):
                candidate = load.compose_model(Path('/fixture'), 'a' * 20,
                    {'web': 12345, 'prometheus': 12346, 'grafana': 12347}, mode)
            self.assertEqual(candidate['services']['api']['environment']['TASKFLOW_CACHE_ENABLED'],
                             'false' if mode == 'off' else 'true')

    def test_resource_stop_budgets(self):
        self.assertFalse(load.resource_guard([{'MemPerc': '10.5%'}], 'MemAvailable: 2097152 kB', 6 * 1024**3))
        self.assertTrue(load.resource_guard([{'MemPerc': '91.0%'}], 'MemAvailable: 2097152 kB', 6 * 1024**3))
        for meminfo, disk in [('MemAvailable: 100 kB', 6 * 1024**3),
                              ('MemAvailable: 2097152 kB', 1024)]:
            with self.assertRaises(RuntimeError):
                load.resource_guard([], meminfo, disk)

    def test_database_query_rechecks_ownership_before_any_sql(self):
        import json
        with patch.object(load, 'run', side_effect=['postgres-id', json.dumps([self.fixture('b' * 20)])]) as run:
            with self.assertRaises(RuntimeError):
                load.database_query(['compose'], 'a' * 20, 'taskflow-load-' + 'a' * 20, 'UPDATE tasks SET completed=true')
            self.assertEqual(run.call_count, 2)

    def test_full_preparation_contract_and_incomplete_history_fail_closed(self):
        calls = []

        class Client:
            count = 0

            def __init__(self, *args):
                self.index = Client.count
                Client.count += 1
                self.jar = [SimpleNamespace(name='__Host-taskflow_session', value='test-cookie')]
                self.csrf = None

            def request(self, method, path, data=None, expected=200):
                calls.append((method, path, data))
                if path == '/api/auth/register':
                    return {}
                if path == '/api/auth/browser/login':
                    return {'csrf_token': 'csrf', 'user': {'id': str(self.index)}}
                if method == 'GET' and path.startswith('/api/tasks/today'):
                    return {'plan_date': '2026-11-15', 'next_cursor': 'cursor'}
                if method == 'POST' and path in ('/api/tasks', '/api/tasks/today'):
                    return {'id': 'task-' + str(len(calls)), 'version': 1}
                if path == '/api/recurrences':
                    return {}
                if path == '/api/projects':
                    return {'id': 'project-' + str(self.index)}
                if path.endswith('/invitations'):
                    return {'token': 'invite'}
                if path == '/api/invitations/accept':
                    return {}
                if path.startswith('/api/tasks?plan_date='):
                    return [{'plan_date': '2026-11-14'}] * 10
                if path.startswith('/api/tasks?project_id='):
                    return [{'project_id': path.split('=')[1]}] * 20
                raise AssertionError((method, path))

        with patch.object(load, 'FixtureClient', Client), patch.object(load.ssl, 'create_default_context'), \
                patch.object(load.time, 'sleep'):
            query = Mock(side_effect=['100', '10'])
            users, today, history, _ = load.prepare_users('https://localhost:1234', Path('/ca'), query)
            self.assertEqual((len(users), today, history), (10, '2026-11-15', '2026-11-14'))
            self.assertTrue(all(len(user['update_targets']) == 20 for user in users))
            self.assertEqual(sum(path == '/api/recurrences' for _, path, _ in calls), 10)
            self.assertEqual(sum(path.endswith('/invitations') for _, path, _ in calls), 8)
            self.assertEqual(sum(path == '/api/invitations/accept' for _, path, _ in calls), 8)
            self.assertEqual(sum(method == 'POST' and path in ('/api/tasks', '/api/tasks/today')
                                 for method, path, _ in calls), 440)
            self.assertIn('recurrence_id IS NULL', query.call_args_list[0].args[0])
            Client.count = 0
            with self.assertRaisesRegex(RuntimeError, 'Historical fixture row count'):
                load.prepare_users('https://localhost:1234', Path('/ca'), Mock(return_value='99'))

    def test_report_uses_failure_rate_and_keeps_actual_status_counts(self):
        import json
        with tempfile.TemporaryDirectory() as tmp:
            report = Path(tmp)
            metadata = {'status': 'passed', 'profile': 'smoke', 'duration_seconds': 30,
                        'cleanup': 'removed'}
            (report / 'summary.json').write_text(json.dumps({'metrics': {
                'http_req_failed': {'values': {'rate': 0, 'passes': 0, 'fails': 30}}}}))
            (report / 'samples.json').write_text(json.dumps({'type': 'Point', 'metric': 'business_status',
                'data': {'value': 1, 'tags': {'operation': 'today', 'status': '200'}}}) + '\n')
            load.write_summary(report, metadata)
            self.assertIn('| HTTP failure rate (0–1) | 0 |', (report / 'summary.md').read_text())
            counts = json.loads((report / 'operations.json').read_text())
            self.assertEqual(counts['operations'], {'today': 1})
            self.assertEqual(counts['http_statuses'], {'200': 1})

    def counters(self, enabled=True, hits=0, fills=0):
        return {'enabled': enabled, 'reads': {'hit': hits, 'miss': 0, 'bypass': 0, 'error': 0, 'other': 0},
                'fills': {'success': fills, 'error': 0, 'skipped': 0, 'other': 0}}

    def test_cache_boundaries_exclude_preparation_and_refuse_resets(self):
        before, after = self.counters(hits=100, fills=10), self.counters(hits=120, fills=12)
        delta = load.cache_delta(before, after)
        self.assertEqual((delta['reads']['hit'], delta['fills']['success']), (20, 2))
        with self.assertRaises(RuntimeError):
            load.cache_delta(after, before)
        with self.assertRaises(RuntimeError):
            load.cache_delta(before, self.counters(enabled=False))

    def test_metric_parser_rejects_missing_evidence_instead_of_claiming_zero(self):
        text = 'taskflow_today_cache_enabled 1\n'
        text += ''.join(f'taskflow_today_cache_reads_total{{outcome="{key}"}} {value}\n'
                        for key, value in self.counters(hits=12)['reads'].items())
        text += ''.join(f'taskflow_today_cache_fills_total{{outcome="{key}"}} {value}\n'
                        for key, value in self.counters(fills=4)['fills'].items())
        self.assertEqual(load.parse_cache_counters(text), self.counters(hits=12, fills=4))
        with self.assertRaises(RuntimeError):
            load.parse_cache_counters('taskflow_today_cache_enabled 1\n')

    def test_cache_clear_checks_owner_and_never_deletes_outside_namespace(self):
        import json
        marker, project = 'a' * 20, 'taskflow-load-' + 'a' * 20
        with patch.object(load, 'run', side_effect=['redis', json.dumps([self.fixture()]),
                'taskflow:cache:one\ntaskflow:cache:two', '2', '']) as run:
            self.assertEqual(load.clear_owned_cache(['compose'], marker, project), 2)
            self.assertEqual(run.call_args_list[3].args[0][-3:], ['DEL', 'taskflow:cache:one', 'taskflow:cache:two'])
        with patch.object(load, 'run', side_effect=['redis', json.dumps([self.fixture()]), 'foreign:key']) as run:
            with self.assertRaises(RuntimeError):
                load.clear_owned_cache(['compose'], marker, project)
            self.assertEqual(run.call_count, 3)
        with patch.object(load, 'run', side_effect=['redis', json.dumps([self.fixture('b' * 20)])]) as run:
            with self.assertRaises(RuntimeError):
                load.clear_owned_cache(['compose'], marker, project)
            self.assertEqual(run.call_count, 2)

    def test_warm_requires_confirmed_fills_and_off_requires_runtime_disabled(self):
        import io
        import json
        users = [{'cookie': str(i), 'title_prefix': f'Account {i} '} for i in range(10)]
        def response(request, **kwargs):
            index = int(request.get_header('Cookie').split('=')[1])
            return io.BytesIO(json.dumps({'items': [{'title': users[index]['title_prefix'] + 'task'}] * 20}).encode())
        with patch.object(load, 'clear_owned_cache', return_value=0), \
                patch.object(load.ssl, 'create_default_context'), patch.object(load.PreparationPacer, 'wait'), \
                patch.object(load.urllib.request, 'urlopen', side_effect=response) as http:
            with patch.object(load, 'cache_counters', side_effect=[self.counters(fills=4), self.counters(fills=14)]):
                prepared = load.prepare_cache([], 'a' * 20, 'project', 'warm', 'https://localhost:123', Path('/ca'), users)
                self.assertEqual(prepared['prewarmed_accounts'], 10)
                self.assertEqual(http.call_count, 10)
            with patch.object(load, 'cache_counters', side_effect=[self.counters(fills=4), self.counters(fills=13)]):
                with self.assertRaisesRegex(RuntimeError, 'fills incomplete'):
                    load.prepare_cache([], 'a' * 20, 'project', 'warm', 'https://localhost:123', Path('/ca'), users)
            with patch.object(load, 'cache_counters', return_value=self.counters()):
                with self.assertRaisesRegex(RuntimeError, 'Runtime cache setting'):
                    load.prepare_cache([], 'a' * 20, 'project', 'off', 'https://localhost:123', Path('/ca'), users)


if __name__ == '__main__':
    unittest.main()
