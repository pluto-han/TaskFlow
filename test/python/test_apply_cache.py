"""Operator tool: bounded config delta, shared lock, promotion and rollback state."""
import copy
import fcntl
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from scripts import apply_cache as apply
from scripts import init_cache
from scripts import prepare_cache as prep
from test.python.test_prepare_cache import server_config


class CacheActivation(unittest.TestCase):
    def exercise(self, outcome=None, network_failure=False, image_mismatch=False):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            baseline = server_config(root)
            base = root / "compose.server.yaml"
            base.write_text(json.dumps(baseline))
            init_cache.prepare(root / ".local/cache")
            with patch.object(prep, "normalized", side_effect=lambda _: json.loads(base.read_text())):
                directory = prep.prepare("connect", root)
                previous = copy.deepcopy(baseline)
                for service in ("api", "migrate", "web"):
                    previous["services"][service]["image"] = "sha256:" + "1"*64
                with patch.object(apply.cd, "ROOT", root), \
                     patch.object(apply, "check_network", side_effect=RuntimeError("Required cache network is unavailable") if network_failure else None), \
                     patch.object(apply.cd, "snapshot", return_value=previous), \
                     patch.object(apply.cd, "run", return_value="sha256:" + ("2" if image_mismatch else "1")*64), \
                     patch.object(apply.cd, "compose") as compose, \
                     patch.object(apply.cd, "probe"), \
                     patch.object(apply.cd, "activate_with_rollback", side_effect=outcome) as activate:
                    error = None
                    try:
                        apply.apply(directory, root)
                    except RuntimeError as exc:
                        error = str(exc)
                    return {"error": error, "current": json.loads(base.read_text()),
                            "candidate": json.loads((directory / "candidate.json").read_text()),
                            "baseline": baseline, "activated": activate.call_count,
                            "pending": (root / ".local/deploy/cd/pending.json").exists(),
                            "docker_calls": compose.call_args_list}

    def test_success_promotes_only_cache_settings(self):
        result = self.exercise()
        self.assertIsNone(result["error"])
        self.assertEqual(result["activated"], 1)
        self.assertEqual(result["current"], result["candidate"])
        self.assertFalse(result["pending"])
        for service in ("postgres", "migrate", "web"):
            self.assertEqual(result["current"]["services"][service], result["baseline"]["services"][service])
        self.assertTrue(all("up" not in call.args for call in result["docker_calls"]))

    def test_clean_rollback_keeps_current_file_and_removes_pending(self):
        result = self.exercise(RuntimeError("deployment failed; previous images restored"))
        self.assertIn("previous images restored", result["error"])
        self.assertEqual(result["current"], result["baseline"])
        self.assertFalse(result["pending"])

    def test_failed_rollback_keeps_pending_and_blocks_followup_cd(self):
        result = self.exercise(RuntimeError("ROLLBACK FAILED; inspect locally"))
        self.assertIn("ROLLBACK FAILED", result["error"])
        self.assertEqual(result["current"], result["baseline"])
        self.assertTrue(result["pending"])

    def test_missing_network_and_image_drift_fail_before_activation(self):
        for args in ({"network_failure": True}, {"image_mismatch": True}):
            result = self.exercise(**args)
            self.assertIsNotNone(result["error"])
            self.assertEqual(result["activated"], 0)
            self.assertEqual(result["current"], result["baseline"])
            self.assertFalse(result["pending"])

    def test_same_lock_as_cd_excludes_parallel_activation(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            state = root / ".local/deploy/cd"
            state.mkdir(parents=True)
            with (state / "deploy.lock").open("a") as lock, patch.object(apply.cd, "ROOT", root), patch.object(prep, "check") as check:
                fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
                with self.assertRaisesRegex(RuntimeError, "in progress"): apply.apply(root, root)
                check.assert_not_called()

    def test_network_is_private_and_owned(self):
        good = {"Internal": True, "Labels": {"com.docker.compose.project": prep.CACHE_PROJECT, "com.docker.compose.network": "cache"}}
        with patch.object(apply.cd, "run", return_value=json.dumps([good])): apply.check_network()
        for bad in ({"Internal": False, "Labels": good["Labels"]}, {"Internal": True, "Labels": {} }):
            with patch.object(apply.cd, "run", return_value=json.dumps([bad])):
                with self.assertRaisesRegex(RuntimeError, "ownership or isolation"): apply.check_network()


if __name__ == "__main__":
    unittest.main()
