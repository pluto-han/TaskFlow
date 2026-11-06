"""Preview one test-guild command. Remote registration requires explicit --apply."""
import argparse
import json
import os
from pathlib import Path
import urllib.error
import urllib.request

API = "https://discord.com/api/v10"
COMMAND = {
    "name": "taskflow", "type": 1,
    "description": "Bind a TaskFlow account to this channel",
    "default_member_permissions": "16",
    "options": [{
        "name": "bind", "type": 1,
        "description": "Use a one-time code from your TaskFlow profile",
        "options": [{
            "name": "code", "type": 3, "required": True,
            "description": "Private one-time TaskFlow binding code",
            "min_length": 43, "max_length": 43,
        }],
    }],
}


def snowflake(value):
    if (not value.isascii() or not value.isdigit() or value.startswith("0")
            or len(value) > 20 or int(value) > 2**64 - 1):
        raise argparse.ArgumentTypeError("Expected a decimal Discord ID")
    return value


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def exchange(method, url, token, body=None):
    data = None if body is None else json.dumps(body).encode()
    req = urllib.request.Request(url, data=data, method=method, headers={
        "Authorization": "Bot " + token, "Content-Type": "application/json",
        "User-Agent": "TaskFlow-Lesson33/1.0",
    })
    try:
        with urllib.request.build_opener(NoRedirect()).open(req, timeout=10) as response:
            raw = response.read(1024 * 1024 + 1)
            if len(raw) > 1024 * 1024:
                raise RuntimeError("Discord response too large")
            return json.loads(raw)
    except urllib.error.HTTPError as error:
        raise RuntimeError(f"Discord returned HTTP {error.code}; response and token omitted") from None
    except (OSError, ValueError):
        raise RuntimeError("Discord request failed; details omitted") from None


def register(app, guild, token, replace=False):
    endpoint = f"{API}/applications/{app}/guilds/{guild}/commands"
    existing = exchange("GET", endpoint, token)
    if not isinstance(existing, list):
        raise RuntimeError("Unexpected command list")
    if any(c.get("name") == "taskflow" and c.get("type", 1) == 1 for c in existing) and not replace:
        raise RuntimeError("taskflow already exists; review it before using --replace")
    # POST upserts only this command. Never bulk PUT the guild command list.
    exchange("POST", endpoint, token, COMMAND)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--application-id", required=True, type=snowflake)
    parser.add_argument("--guild-id", required=True, type=snowflake)
    parser.add_argument("--apply", action="store_true")
    parser.add_argument("--replace", action="store_true")
    args = parser.parse_args(argv)
    if not args.apply:
        print(json.dumps({"mode": "preview", "application_id": args.application_id,
                          "guild_id": args.guild_id, "command": COMMAND}, indent=2))
        return
    token_path = os.getenv("DISCORD_BOT_TOKEN_FILE")
    if not token_path:
        raise SystemExit("Set DISCORD_BOT_TOKEN_FILE to a private token file")
    try:
        with Path(token_path).open() as stream:
            token = stream.read(4097).strip()
        if not token or len(token) > 4096 or any(c.isspace() for c in token):
            raise ValueError()
        register(args.application_id, args.guild_id, token, args.replace)
    except (OSError, ValueError, RuntimeError) as error:
        # Known RuntimeError messages contain no response body or credential.
        raise SystemExit(str(error) if isinstance(error, RuntimeError) else "Cannot read token file") from None
    print("Registered taskflow in the selected test guild; no other commands changed")


if __name__ == "__main__":
    main()
