"""Explicit operator action: apply a reviewed cache candidate with CD locking.

This tool is NOT installed in authorized_keys and accepts no remote CD payload.
Only cache settings may change; database migrations are never executed.
"""
import argparse
import fcntl
import json
import os
from pathlib import Path
import sys
import uuid

if __package__:
    from . import prepare_cache as prep
else:
    import prepare_cache as prep

cd = prep.cd
ROOT = Path(__file__).resolve().parents[1]


def check_network():
    try:
        network = json.loads(cd.run(["docker", "network", "inspect", prep.NETWORK_NAME]))[0]
    except Exception:
        raise RuntimeError("Required cache network is unavailable; start the independent cache project first") from None
    labels = network.get("Labels") or {}
    if (not network.get("Internal") or labels.get("com.docker.compose.project") != prep.CACHE_PROJECT or
            labels.get("com.docker.compose.network") != "cache"):
        raise RuntimeError("Cache network ownership or isolation is unexpected")


def apply(directory, root=ROOT):
    directory = Path(directory).resolve()
    # Reuse the unchanged receiver's activation/rollback implementation. This
    # operator tool deliberately targets the same fixed server checkout.
    if root.resolve() != cd.ROOT.resolve():
        raise RuntimeError("Run this tool in the receiver's /root/projects/TaskFlow checkout")
    state = root / ".local/deploy/cd"
    state.mkdir(parents=True, mode=0o700, exist_ok=True)
    with (state / "deploy.lock").open("a") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise RuntimeError("Another CD or cache operation is in progress") from None
        candidate = prep.check(directory, root)
        mode = json.loads((directory / "state.json").read_text())["mode"]
        if mode in ("connect", "enable"):
            check_network()
        base = root / "compose.server.yaml"
        previous = cd.snapshot(json.loads((directory / "baseline.json").read_text()), base)
        # A cache change must not incidentally publish a locally retagged image.
        for service in ("api", "web"):
            loaded = cd.run(["docker", "image", "inspect", "--format", "{{.Id}}", candidate["services"][service]["image"]])
            if loaded != previous["services"][service]["image"]:
                raise RuntimeError("Current image/config mismatch; reconcile the release before cache changes")
        target = directory / "candidate.json"
        rollback = directory / "previous-runtime.json"
        if rollback.exists():
            raise RuntimeError("Candidate was already attempted; prepare a fresh candidate")
        cd.compose(target, "config", "--quiet")
        cd.compose(base, "exec", "-T", "api", "/api", "healthcheck")
        cd.probe()
        cd.write_json(rollback, previous)
        cd.write_json(state / "pending.json", {"release": str(directory), "kind": "cache-config"})
        try:
            cd.activate_with_rollback(target, rollback)
        except RuntimeError as error:
            if str(error) == "deployment failed; previous images restored":
                cd.write_json(directory / "result.json", {"status": "rolled_back"})
                (state / "pending.json").unlink()
            raise
        temporary = root / ("compose.server.yaml.cache-" + uuid.uuid4().hex)
        cd.write_json(temporary, candidate)
        os.replace(temporary, base)
        cd.write_json(directory / "result.json", {"status": "applied", "mode": mode})
        (state / "pending.json").unlink()
        print(f"Applied cache mode={mode}; current compose.server.yaml synchronized")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("directory", type=Path, help="Reviewed preparation directory printed by prepare_cache.py")
    args = parser.parse_args()
    os.umask(0o077)
    apply(args.directory)


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(str(error) if isinstance(error, RuntimeError) else "Cache activation failed; inspect pending.json locally, output omitted", file=sys.stderr)
        raise SystemExit(1)
