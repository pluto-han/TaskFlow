"""CD failure-path tests. No Docker, SSH or production database access."""
import copy
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
from scripts import deploy_release as cd
from scripts import prepare_cache as prep
from test.python.test_prepare_cache import server_config


def request():
    return {'revision': 'a' * 40, 'images': {
        s: cd.REGISTRY + s + '@sha256:' + 'b' * 64 for s in cd.SERVICES}}


def config():
    return {'services': {
        'postgres': {'image': 'postgres:17'},
        'api': {'image': 'old-api', 'environment': {'TASKFLOW_PUBLIC_ORIGIN': 'https://taskflow.hancongzhang.com'}},
        'migrate': {'image': 'old-migrate'},
        'web': {'image': 'old-web', 'ports': [{'host_ip': '127.0.0.1', 'published': '8082'}]},
    }}


class Validation(unittest.TestCase):
    def test_reject_untrusted_or_mutable_images(self):
        for image in ('ubuntu:latest', cd.REGISTRY + 'api:latest',
                      'ghcr.io/other/taskflow-api@sha256:' + 'b'*64,
                      cd.REGISTRY + 'api@sha256:' + 'b'*64 + ';id'):
            data = request()
            data['images']['api'] = image
            with self.assertRaises(ValueError):
                cd.validate_request(data)
        self.assertEqual(cd.validate_request(request()), request())

    def test_reject_extra_arguments(self):
        data = request()
        data['command'] = 'delete'
        with self.assertRaises(ValueError):
            cd.validate_request(data)

    def test_reject_public_backend_or_gateway(self):
        for service in ('api', 'postgres', 'migrate', 'web'):
            data = config()
            data['services'][service]['ports'] = [{'host_ip': '0.0.0.0', 'published': '8082'}]
            with self.assertRaises(RuntimeError):
                cd.validate_config(data)
        cd.validate_config(config())


class Deployment(unittest.TestCase):
    def exercise(self, *, changed=False, failures=None):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            base = root / 'compose.server.yaml'
            base.write_text('original')
            migration_results = [{'001.sql': 'old'}, {'001.sql': 'new' if changed else 'old'}]
            with patch.object(cd, 'ROOT', root), \
                 patch.object(cd, 'compose', return_value=json.dumps(config())) as compose, \
                 patch.object(cd, 'snapshot', return_value=copy.deepcopy(config())), \
                 patch.object(cd, 'run', return_value=json.dumps({'org.opencontainers.image.revision': 'a'*40})), \
                 patch.object(cd, 'migration_hashes', side_effect=migration_results), \
                 patch.object(cd, 'probe'), \
                 patch.object(cd, 'activate', side_effect=failures) as activate:
                error = None
                try:
                    cd.deploy(request())
                except RuntimeError as exc:
                    error = str(exc)
                return error, activate.call_args_list, base.read_text(), compose.call_args_list

    def test_changed_migrations_never_activate(self):
        error, calls, base, _ = self.exercise(changed=True)
        self.assertIn('migrations changed', error)
        self.assertEqual(calls, [])
        self.assertEqual(base, 'original')

    def test_failed_release_rolls_back_and_does_not_promote(self):
        error, calls, base, _ = self.exercise(failures=[RuntimeError('unhealthy'), None])
        self.assertIn('previous images restored', error)
        self.assertEqual([c.args[0].name for c in calls], ['candidate.json', 'previous.json'])
        self.assertEqual(base, 'original')

    def test_failed_rollback_is_visible(self):
        error, calls, base, _ = self.exercise(failures=[RuntimeError('bad'), RuntimeError('bad')])
        self.assertIn('ROLLBACK FAILED', error)
        self.assertEqual(len(calls), 2)
        self.assertEqual(base, 'original')

    def test_success_updates_images_not_database_or_migration_job(self):
        error, calls, base, _ = self.exercise()
        self.assertIsNone(error)
        self.assertEqual(len(calls), 1)
        result = json.loads(base)
        self.assertEqual(result['services']['api']['image'], request()['images']['api'])
        self.assertEqual(result['services']['postgres'], config()['services']['postgres'])
        self.assertEqual(result['services']['migrate'], config()['services']['migrate'])

    def test_cache_network_secret_and_existing_workers_survive_cd_and_rollback(self):
        for fail in (False, True):
            with self.subTest(rollback=fail), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                base = root / 'compose.server.yaml'
                baseline = prep.cache_model(server_config(root), 'connect', root / '.local/cache/redis_password')
                baseline = prep.cache_model(baseline, 'enable', root / '.local/cache/redis_password')
                base.write_text(json.dumps(baseline))
                captured = []
                def activate(path):
                    captured.append(json.loads(path.read_text()))
                    if fail and len(captured) == 1: raise RuntimeError('intentional failed API')
                with patch.object(cd, 'ROOT', root), \
                     patch.object(cd, 'compose', return_value=json.dumps(baseline)), \
                     patch.object(cd, 'snapshot', return_value=copy.deepcopy(baseline)), \
                     patch.object(cd, 'run', return_value=json.dumps({'org.opencontainers.image.revision': 'a'*40})), \
                     patch.object(cd, 'migration_hashes', return_value={'001.sql': 'unchanged'}), \
                     patch.object(cd, 'probe'), patch.object(cd, 'activate', side_effect=activate):
                    if fail:
                        with self.assertRaisesRegex(RuntimeError, 'previous images restored'): cd.deploy(request())
                    else:
                        cd.deploy(request())
                for model in captured + [json.loads(base.read_text())]:
                    self.assertEqual(model['networks'], baseline['networks'])
                    self.assertEqual(model['secrets'], baseline['secrets'])
                    for key in ('environment', 'networks', 'secrets'):
                        self.assertEqual(model['services']['api'][key], baseline['services']['api'][key])
                    self.assertEqual(set(model['services']), {'postgres', 'migrate', 'api', 'web'})
                    self.assertEqual(model['services']['postgres'], baseline['services']['postgres'])

    def test_interrupted_release_blocks_retry_before_docker(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            state = root / '.local/deploy/cd'
            state.mkdir(parents=True)
            (state / 'pending.json').write_text('{}')
            with patch.object(cd, 'ROOT', root), patch.object(cd, 'compose') as compose:
                with self.assertRaisesRegex(RuntimeError, 'interrupted'):
                    cd.deploy(request())
                compose.assert_not_called()

    def test_reject_wrong_ssh_command(self):
        with patch.dict(cd.os.environ, {'SSH_ORIGINAL_COMMAND': 'bash'}), patch.object(cd, 'deploy') as deploy:
            with self.assertRaisesRegex(RuntimeError, 'only the deploy'):
                cd.main()
            deploy.assert_not_called()

    def test_activate_never_starts_dependencies(self):
        with patch.object(cd, 'compose') as compose, patch.object(cd, 'probe'):
            cd.activate(Path('candidate.json'))
        args = compose.call_args_list[0].args
        self.assertIn('--no-deps', args)
        self.assertEqual(args[-2:], ('api', 'web'))
        self.assertNotIn('postgres', args)
        self.assertNotIn('migrate', args)


if __name__ == '__main__':
    unittest.main()
