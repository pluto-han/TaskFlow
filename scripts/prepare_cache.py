"""Generate a private, reviewable cache candidate from the CURRENT server config.

No container changes, password generation, config replacement or SSH actions.
"""
import argparse
import copy
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import uuid

if __package__:
    from . import deploy_release as cd
else:
    import deploy_release as cd

ROOT = Path(__file__).resolve().parents[1]
MODES = ("off", "connect", "enable", "detach")
NETWORK = "taskflow_cache"
CACHE_PROJECT = "taskflow-cache"
NETWORK_NAME = "taskflow-cache_cache"
SECRET = "redis_password"
CACHE_ENV = {
    "REDIS_ADDR": "taskflow-redis:6379",
    "REDIS_USERNAME": "taskflow_cache",
    "REDIS_PASSWORD_FILE": "/run/secrets/redis_password",
}


def normalized(root):
    result = subprocess.run(
        ["docker", "compose", "--project-directory", str(root), "-p", cd.PROJECT,
         "-f", str(root / "compose.server.yaml"), "config", "--format", "json"],
        cwd=root, text=True, capture_output=True, timeout=30)
    if result.returncode:
        raise RuntimeError("Cannot read current server configuration; output omitted")
    return json.loads(result.stdout)


def validate_server(data):
    cd.validate_config(data)
    api = data["services"]["api"]
    if (data.get("name") != cd.PROJECT or
            api.get("networks", {}).get("edge", {}).get("ipv4_address") != "172.30.23.3" or
            data["services"]["web"].get("networks", {}).get("edge", {}).get("ipv4_address") != "172.30.23.2"):
        raise RuntimeError("Unexpected project or fixed proxy addresses; review manually")


def secret_source(mount):
    return mount if isinstance(mount, str) else mount.get("source")


def cache_model(data, mode, password_file, network_name=None):
    """Pure transform: change only explicitly owned cache settings on the API."""
    if mode not in MODES:
        raise RuntimeError("Unknown cache mode")
    network_name = network_name or NETWORK_NAME
    model = copy.deepcopy(data)
    api = model["services"]["api"]
    env = api.setdefault("environment", {})
    networks = model.setdefault("networks", {})
    definitions = model.setdefault("secrets", {})
    expected_network = {"external": True, "name": network_name}
    expected_secret = {"file": str(password_file)}
    # Refuse collisions instead of taking over another feature's wiring.
    if NETWORK in networks and networks[NETWORK] not in (expected_network, {**expected_network, "ipam": {}}):
        raise RuntimeError("Cache network definition conflicts with managed settings")
    if SECRET in definitions and definitions[SECRET] not in (expected_secret, {**expected_secret, "name": model["name"] + "_" + SECRET}):
        raise RuntimeError("Cache secret definition conflicts with managed settings")
    for name, service in model["services"].items():
        if name != "api" and (NETWORK in service.get("networks", {}) or
                any(secret_source(mount) == SECRET for mount in service.get("secrets", []))):
            raise RuntimeError("Cache wiring is unexpectedly shared by another service")
    mounts = api.get("secrets", [])
    for mount in mounts:
        if (secret_source(mount) != SECRET and isinstance(mount, dict) and
                mount.get("target", "").removeprefix("/run/secrets/") == SECRET):
            raise RuntimeError("Another secret occupies the cache secret target")
    managed = [mount for mount in mounts if secret_source(mount) == SECRET]
    if managed and managed != [{"source": SECRET, "target": "/run/secrets/" + SECRET}]:
        raise RuntimeError("Unexpected cache secret mount; review manually")
    api_networks = api.setdefault("networks", {})
    if NETWORK in api_networks and api_networks[NETWORK] not in ({}, None):
        raise RuntimeError("Unexpected API cache network options")
    if mode == "enable":
        if (NETWORK not in api_networks or NETWORK not in networks or
                SECRET not in definitions or not managed or
                any(env.get(key) != value for key, value in CACHE_ENV.items())):
            raise RuntimeError("Connect the API with caching off before enabling")
    env["TASKFLOW_CACHE_ENABLED"] = "true" if mode == "enable" else "false"
    if mode == "connect":
        api_networks[NETWORK] = {}
        networks[NETWORK] = expected_network
        definitions[SECRET] = expected_secret
        api["secrets"] = [*mounts] if managed else [*mounts, {"source": SECRET, "target": "/run/secrets/" + SECRET}]
        env.update(CACHE_ENV)
    elif mode == "detach":
        for key in CACHE_ENV:
            env.pop(key, None)
        api_networks.pop(NETWORK, None)
        networks.pop(NETWORK, None)
        definitions.pop(SECRET, None)
        if managed:
            api["secrets"] = [mount for mount in mounts if secret_source(mount) != SECRET]
    return model


def verify_credentials(root):
    directory = root / ".local/cache"
    if directory.is_symlink() or not directory.is_dir() or directory.stat().st_mode & 0o077:
        raise RuntimeError("Cache credential directory must be private (0700)")
    values = {}
    for name in (SECRET, "users.acl"):
        path = directory / name
        if path.is_symlink() or not path.is_file() or path.stat().st_size > 4096:
            raise RuntimeError("Cache credential files are missing or invalid")
        values[name] = path.read_text()
    password = values[SECRET].strip()
    if not re.fullmatch(r"[0-9a-f]{64}", password):
        raise RuntimeError("Use init_cache.py credentials; password omitted")
    expected = ("user default reset off\n"
                f"user taskflow_cache reset on #{hashlib.sha256(password.encode()).hexdigest()} "
                "~taskflow:cache:* -@all +hello +ping +get +set +del\n")
    if values["users.acl"] != expected:
        raise RuntimeError("Cache ACL does not match the password and narrow command policy")


def model_hash(data):
    return hashlib.sha256(json.dumps(data, sort_keys=True).encode()).hexdigest()


def current(root):
    if (root / ".local/deploy/cd/pending.json").exists():
        raise RuntimeError("Unfinished CD release; inspect pending.json before cache changes")
    path = root / "compose.server.yaml"
    if not path.is_file() or path.is_symlink():
        raise RuntimeError("Requires the current regular compose.server.yaml on the server")
    raw_hash = hashlib.sha256(path.read_bytes()).hexdigest()
    data = normalized(root)
    validate_server(data)
    # Detect a replacement while Compose was normalizing the source.
    if hashlib.sha256(path.read_bytes()).hexdigest() != raw_hash:
        raise RuntimeError("Server config changed during preparation; retry")
    return data, raw_hash


def prepare(mode, root=ROOT):
    data, raw_hash = current(root)
    if mode in ("connect", "enable"):
        verify_credentials(root)
    candidate = cache_model(data, mode, root / ".local/cache/redis_password")
    validate_server(candidate)
    directory = root / ".local/deploy/cache" / uuid.uuid4().hex
    directory.mkdir(mode=0o700, parents=True)
    directory.chmod(0o700)
    cd.write_json(directory / "baseline.json", data)
    cd.write_json(directory / "candidate.json", candidate)
    cd.write_json(directory / "state.json", {"mode": mode, "base_sha256": raw_hash,
                  "model_sha256": model_hash(data)})
    print(f"Prepared {directory.relative_to(root)}/candidate.json (mode={mode}); no services changed")
    return directory


def check(directory, root=ROOT):
    directory = Path(directory).resolve()
    if not directory.is_relative_to((root / ".local/deploy/cache").resolve()):
        raise RuntimeError("Candidate must be inside this checkout's cache preparation directory")
    state = json.loads((directory / "state.json").read_text())
    data, raw_hash = current(root)
    if raw_hash != state["base_sha256"] or model_hash(data) != state["model_sha256"]:
        raise RuntimeError("Candidate is stale; prepare again from the current server config")
    if json.loads((directory / "baseline.json").read_text()) != data:
        raise RuntimeError("Candidate baseline was changed")
    mode = state["mode"]
    if mode in ("connect", "enable"):
        verify_credentials(root)
    expected = cache_model(data, mode, root / ".local/cache/redis_password")
    candidate = json.loads((directory / "candidate.json").read_text())
    if candidate != expected:
        raise RuntimeError("Candidate has unexpected changes; prepare it again")
    validate_server(candidate)
    return candidate


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    group = parser.add_mutually_exclusive_group(required=True)
    group.add_argument("--mode", choices=MODES)
    group.add_argument("--check", type=Path, metavar="CANDIDATE_DIRECTORY")
    args = parser.parse_args()
    os.umask(0o077)
    if args.check:
        check(args.check)
        print("Candidate matches the current config and managed cache changes; no services changed")
    else:
        prepare(args.mode)


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(str(error) if isinstance(error, RuntimeError) else "Cache preparation failed; inspect locally, output omitted", file=sys.stderr)
        raise SystemExit(1)
