import contextlib
import io
import unittest
from unittest.mock import patch
from scripts import register_discord_commands as commands


class RegistrationTests(unittest.TestCase):
    def test_preview_never_reads_token_or_network(self):
        with patch.object(commands, "exchange") as network, patch.object(commands.Path, "open") as secret:
            with contextlib.redirect_stdout(io.StringIO()):
                commands.main(["--application-id", "123", "--guild-id", "456"])
            network.assert_not_called()
            secret.assert_not_called()

    def test_single_upsert_never_bulk_replaces(self):
        with patch.object(commands, "exchange", side_effect=[[{"name": "unrelated", "type": 1}], {}]) as call:
            commands.register("123", "456", "private")
            self.assertEqual([c.args[0] for c in call.call_args_list], ["GET", "POST"])
            self.assertEqual(call.call_args_list[1].args[3]["name"], "taskflow")
        with patch.object(commands, "exchange", return_value=[{"name": "taskflow", "type": 1}]) as call:
            with self.assertRaisesRegex(RuntimeError, "already exists"):
                commands.register("123", "456", "private")
            self.assertEqual(call.call_count, 1)

    def test_replace_requires_explicit_flag(self):
        with patch.object(commands, "exchange", side_effect=[[{"name": "taskflow", "type": 1}], {}]) as call:
            commands.register("123", "456", "private", replace=True)
            self.assertEqual(call.call_count, 2)
