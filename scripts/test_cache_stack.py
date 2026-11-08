"""Exercise only a random disposable Redis project, never an existing server."""
import json
import os
from pathlib import Path
import secrets
import signal
import subprocess
import sys
import tempfile

from scripts import init_cache

ROOT = Path(__file__).resolve().parents[1]


def run(args, *, capture=False, env=None, timeout=180):
    result = subprocess.run(args, cwd=ROOT, env=env, text=True, timeout=timeout,
                            stdout=subprocess.PIPE if capture else None,
                            stderr=subprocess.PIPE if capture else None)
    if result.returncode:
        raise RuntimeError(f"{args[0]} failed; credentials omitted")
    return result.stdout.strip() if capture else ""


def main():
    signal.signal(signal.SIGTERM, lambda *_: sys.exit(143))
    context = run(["docker", "context", "inspect", "--format", "{{.Endpoints.docker.Host}}"], capture=True)
    endpoint = context if os.environ.get("DOCKER_CONTEXT") else os.environ.get("DOCKER_HOST", context)
    if not endpoint.startswith("unix://"):
        raise RuntimeError("requires a local Docker Unix socket")
    marker = secrets.token_hex(16)
    project = "taskflow-cache-test-" + marker
    local = ROOT / ".local"
    local.mkdir(exist_ok=True, mode=0o700)
    with tempfile.TemporaryDirectory(prefix="cache-test-", dir=local) as directory:
        directory = Path(directory)
        init_cache.prepare(directory / "secrets")
        model = json.loads(run(["docker", "compose", "-f", "compose.cache.yaml", "config", "--format", "json"], capture=True))
        model["name"] = project
        service = model["services"]["redis"]
        # Loopback publication exists ONLY in this generated disposable test file.
        # The checked-in cache Compose publishes no ports.
        service["ports"] = [{"host_ip": "127.0.0.1", "target": 6379, "published": "0", "protocol": "tcp"}]
        service["labels"] = {"taskflow.cache-test": marker}
        service["restart"] = "no"
        for network in model["networks"].values():
            network.pop("name", None)
        model["networks"]["test_ingress"] = {}
        service["networks"]["test_ingress"] = {}
        for name, filename in (("redis_acl", "users.acl"), ("redis_password", "redis_password")):
            model["secrets"][name]["file"] = str(directory / "secrets" / filename)
        candidate = directory / "compose.json"
        candidate.write_text(json.dumps(model))
        base = ["docker", "compose", "--project-directory", str(ROOT), "-p", project, "-f", str(candidate)]
        try:
            run(base + ["up", "-d", "--wait", "--wait-timeout", "90"])
            container = run(base + ["ps", "-q", "redis"], capture=True)
            details = json.loads(run(["docker", "inspect", container], capture=True))[0]
            if details["Config"]["Labels"].get("taskflow.cache-test") != marker:
                raise RuntimeError("disposable project marker mismatch")
            bindings = details["NetworkSettings"]["Ports"].get("6379/tcp")
            if not bindings or bindings[0]["HostIp"] != "127.0.0.1":
                raise RuntimeError("isolated loopback port was not published")
            port = bindings[0]["HostPort"]
            env = {key: value for key, value in os.environ.items() if not key.startswith("TASKFLOW_") and not key.startswith("REDIS_")}
            env.update({"TASKFLOW_CACHE_TEST_ADDR": "127.0.0.1:" + port,
                        "TASKFLOW_CACHE_TEST_RUN_ID": marker,
                        "TASKFLOW_CACHE_TEST_PASSWORD_FILE": str(directory / "secrets/redis_password")})

            def seed_marker():
                run(base + ["exec", "-T", "redis", "sh", "-ec",
                            'export REDISCLI_AUTH="$(cat /run/secrets/redis_password)"; exec redis-cli --user taskflow_cache "$@"',
                            "fixture", "SET", "taskflow:cache:test-run:" + marker, marker], capture=True)

            seed_marker()
            run(["go", "test", "-race", "-count=1", "-timeout=30s", "-v", "./test/integration/cache", "-run", "^TestRedis(Client|TodayService|TodayWrites)Integration$"] , env=env)
            run(base + ["restart", "redis"])
            run(base + ["up", "-d", "--wait", "--wait-timeout", "90"])
            seed_marker()
            # Docker may allocate a different host port when a dynamic binding
            # restarts. Read the actual binding again instead of reusing it.
            refreshed = json.loads(run(["docker", "inspect", container], capture=True))[0]
            bindings = refreshed["NetworkSettings"]["Ports"].get("6379/tcp")
            if not bindings or bindings[0]["HostIp"] != "127.0.0.1":
                raise RuntimeError("restarted fixture lost its loopback port")
            env["TASKFLOW_CACHE_TEST_ADDR"] = "127.0.0.1:" + bindings[0]["HostPort"]
            run(["go", "test", "-race", "-count=1", "-timeout=30s", "-v", "./test/integration/cache", "-run", "^TestRedisRestartEmpty$"] , env=env)
            print("PASS: real Redis handshake, TTL, ACL, Today cache and restart without persistence")
        finally:
            run(base + ["down", "--remove-orphans"], capture=True)
            print("Only this invocation's disposable cache project removed")


if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, subprocess.TimeoutExpired) as error:
        print(str(error) if isinstance(error, RuntimeError) else "cache test timed out", file=sys.stderr)
        sys.exit(1)
