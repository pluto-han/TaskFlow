"""Preparation must not start services, replace secrets, or rewrite live config."""
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
from types import SimpleNamespace
import prepare_monitoring as prep


class PrepareTests(unittest.TestCase):
    def test_only_candidate_changes_and_password_is_preserved(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root/'compose.server.yaml').write_text('original')
            directory = root/'.local/monitoring'
            directory.mkdir(parents=True)
            password = directory/'grafana_password'
            password.write_text('existing-secret')
            config = {'name':'taskflow-server','services':{'api':{'networks':{'edge':{'ipv4_address':'172.30.23.3'}},'environment':{'KEEP':'unchanged'}}}}
            with patch.object(prep,'ROOT',root), patch.object(prep.subprocess,'run',return_value=SimpleNamespace(returncode=0,stdout=json.dumps(config))) as run:
                prep.main()
                with self.assertRaises(SystemExit):prep.main()
                self.assertTrue(all('config' in call.args[0] for call in run.call_args_list))
            self.assertEqual((root/'compose.server.yaml').read_text(),'original')
            self.assertEqual(password.read_text(),'existing-secret')
            candidate=json.loads((directory/'api.compose.json').read_text())
            self.assertEqual(candidate['services']['api']['environment'],{'KEEP':'unchanged','TASKFLOW_METRICS_ADDR':'172.30.23.3:9091'})

    def test_unexpected_network_or_published_api_is_rejected(self):
        for api in ({'networks':{}},{'networks':{'edge':{'ipv4_address':'172.30.23.3'}},'ports':[9090]}):
            with tempfile.TemporaryDirectory() as tmp:
                root=Path(tmp);(root/'compose.server.yaml').write_text('original')
                config={'name':'taskflow-server','services':{'api':api}}
                with patch.object(prep,'ROOT',root),patch.object(prep.subprocess,'run',return_value=SimpleNamespace(returncode=0,stdout=json.dumps(config))):
                    with self.assertRaises(SystemExit):prep.main()
                self.assertFalse((root/'.local/monitoring').exists())


if __name__=='__main__':unittest.main()
