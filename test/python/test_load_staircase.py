"""Classification contracts; does not start Docker or send traffic."""
import json
from pathlib import Path
import tempfile
import unittest

from scripts.test_load_staircase import classify


class StaircaseReportTests(unittest.TestCase):
    def test_only_clean_429_rejections_are_a_limit_result(self):
        with tempfile.TemporaryDirectory() as tmp:
            report = Path(tmp)
            environment = {'cleanup': 'owned resources removed', 'status': 'failed', 'threshold_failure': True}
            metrics = {'iterations': {'values': {'count': 100}},
                       'business_success': {'values': {'rate': 0.9}},
                       'read_duration': {'values': {'p(95)': 5}},
                       'dropped_iterations': {'values': {'count': 0}}}
            (report / 'environment.json').write_text(json.dumps(environment))
            (report / 'summary.json').write_text(json.dumps({'metrics': metrics}))
            operations = {'http_statuses': {'200': 90, '429': 10}, 'operations': {'today': 100}}
            (report / 'operations.json').write_text(json.dumps(operations))
            self.assertEqual(classify(report, 1)[0], 'limited')
            metrics['business_success']['values']['rate'] = 0.8
            (report / 'summary.json').write_text(json.dumps({'metrics': metrics}))
            self.assertEqual(classify(report, 1)[0], 'failed')
            metrics['business_success']['values']['rate'] = 0.9
            metrics['dropped_iterations']['values']['count'] = 1
            (report / 'summary.json').write_text(json.dumps({'metrics': metrics}))
            self.assertEqual(classify(report, 1)[0], 'failed')
            environment.pop('cleanup')
            (report / 'environment.json').write_text(json.dumps(environment))
            self.assertEqual(classify(report, 1)[0], 'failed')
