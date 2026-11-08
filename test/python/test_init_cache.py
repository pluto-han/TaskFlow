import hashlib
from pathlib import Path
import tempfile
import unittest

from scripts import init_cache


class CachePreparationTests(unittest.TestCase):
    def test_private_directory_hash_and_narrow_acl(self):
        with tempfile.TemporaryDirectory() as directory:
            target = Path(directory) / "cache"
            init_cache.prepare(target)
            password = (target / "redis_password").read_text().strip()
            acl = (target / "users.acl").read_text()
            self.assertEqual(len(password), 64)
            self.assertNotIn(password, acl)
            self.assertIn("#" + hashlib.sha256(password.encode()).hexdigest(), acl)
            self.assertIn("user default reset off", acl)
            self.assertIn("~taskflow:cache:* -@all +hello +ping +get +set +del", acl)
            self.assertEqual(target.stat().st_mode & 0o777, 0o700)
            for file in target.iterdir():
                self.assertEqual(file.stat().st_mode & 0o777, 0o644)

    def test_existing_files_never_change(self):
        with tempfile.TemporaryDirectory() as directory:
            target = Path(directory) / "cache"
            init_cache.prepare(target)
            before = {file.name: file.read_bytes() for file in target.iterdir()}
            with self.assertRaises(SystemExit):
                init_cache.prepare(target)
            self.assertEqual(before, {file.name: file.read_bytes() for file in target.iterdir()})
