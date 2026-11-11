"""Exercise only random disposable Redis/PostgreSQL containers, never existing services."""
import hashlib
import json
import os
import platform
from pathlib import Path
import secrets
import signal
import socket
import shutil
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


def main(*, performance=False):
    signal.signal(signal.SIGTERM, lambda *_: sys.exit(143))
    for tool in ("docker", "go", "migrate"):
        if not shutil.which(tool):
            raise RuntimeError(f"missing required tool: {tool}")
    context = run(["docker", "context", "inspect", "--format", "{{.Endpoints.docker.Host}}"], capture=True)
    endpoint = context if os.environ.get("DOCKER_CONTEXT") else (os.environ.get("DOCKER_HOST") or context)
    if not endpoint.startswith("unix://"):
        raise RuntimeError("requires a local Docker Unix socket")
    marker = secrets.token_hex(16)
    project = "taskflow-cache-test-" + marker
    local = ROOT / ".local"
    local.mkdir(exist_ok=True, mode=0o700)
    with tempfile.TemporaryDirectory(prefix="cache-test-", dir=local) as directory:
        directory = Path(directory)
        init_cache.prepare(directory / "secrets")
        # A read-only user exists only in this disposable candidate. It proves
        # real SET/DEL permission failures without changing production ACLs.
        redis_password = (directory / "secrets/redis_password").read_text().strip()
        digest = hashlib.sha256(redis_password.encode()).hexdigest()
        with (directory / "secrets/users.acl").open("a") as acl:
            acl.write(f"user taskflow_readonly reset on #{digest} ~taskflow:cache:* -@all +hello +ping +get\n")
        pg_password = secrets.token_hex(32)
        password_file = directory / "postgres_password"
        password_file.write_text(pg_password + "\n")
        password_file.chmod(0o644)
        model = json.loads(run(["docker", "compose", "-f", "compose.cache.yaml", "config", "--format", "json"], capture=True))
        model["name"] = project
        service = model["services"]["redis"]
        # Keep eviction fault injection small. Production still has 64 MiB;
        # only this generated test candidate overrides maxmemory to 4 MiB.
        service["command"] += ["--maxmemory", "4mb"]
        # Loopback publication exists ONLY in this generated disposable test file.
        # The checked-in cache Compose publishes no ports.
        # A fixed-for-this-run random port lets the SAME client reconnect after
        # stop/start; Docker may otherwise reassign a dynamic published port.
        with socket.socket() as reservation:
            reservation.bind(("127.0.0.1", 0))
            redis_port = reservation.getsockname()[1]
        service["ports"] = [{"host_ip": "127.0.0.1", "target": 6379, "published": str(redis_port), "protocol": "tcp"}]
        service["labels"] = {"taskflow.cache-test": marker}
        service["restart"] = "no"
        for network in model["networks"].values():
            network.pop("name", None)
        model["networks"]["test_ingress"] = {}
        service["networks"]["test_ingress"] = {}
        production = json.loads(run(["docker", "compose", "-f", "compose.production.yaml", "config", "--format", "json"], capture=True))
        model["secrets"]["test_pg_password"] = {"file": str(password_file)}
        model["services"]["postgres"] = {
            "image": production["services"]["postgres"]["image"],
            "environment": {"POSTGRES_USER": "taskflow", "POSTGRES_DB": "taskflow_lesson41_test",
                            "POSTGRES_PASSWORD_FILE": "/run/secrets/test_pg_password"},
            "command": ["postgres", "-c", "max_connections=20"],
            "secrets": ["test_pg_password"], "networks": {"test_ingress": {}},
            "ports": [{"host_ip": "127.0.0.1", "target": 5432, "published": "0", "protocol": "tcp"}],
            "tmpfs": ["/var/lib/postgresql/data:size=256m"],
            "labels": {"taskflow.cache-test": marker}, "restart": "no",
            "mem_limit": "256m", "pids_limit": 128,
            "healthcheck": {"test": ["CMD", "pg_isready", "-h", "127.0.0.1", "-U", "taskflow"],
                            "interval": "1s", "timeout": "2s", "retries": 30},
        }
        for name, filename in (("redis_acl", "users.acl"), ("redis_password", "redis_password")):
            model["secrets"][name]["file"] = str(directory / "secrets" / filename)
        candidate = directory / "compose.json"
        candidate.write_text(json.dumps(model))
        base = ["docker", "compose", "--project-directory", str(ROOT), "-p", project, "-f", str(candidate)]
        container = None
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
            pg_container = run(base + ["ps", "-q", "postgres"], capture=True)
            pg_details = json.loads(run(["docker", "inspect", pg_container], capture=True))[0]
            pg_bindings = pg_details["NetworkSettings"]["Ports"].get("5432/tcp")
            if pg_details["Config"]["Labels"].get("taskflow.cache-test") != marker or not pg_bindings or pg_bindings[0]["HostIp"] != "127.0.0.1":
                raise RuntimeError("isolated PostgreSQL fixture mismatch")
            pg_url = f"postgres://taskflow:{pg_password}@127.0.0.1:{pg_bindings[0]['HostPort']}/taskflow_lesson41_test?sslmode=disable"
            env = {key: value for key, value in os.environ.items()
                   if not key.startswith(("TASKFLOW_", "REDIS_", "PG")) and key not in ("DATABASE_URL", "DATABASE_URL_FILE")}
            run(["migrate", "-path", "migrations", "-database", pg_url, "goto", "17"], capture=True, env=env)
            run(base + ["exec", "-T", "postgres", "psql", "-X", "-U", "taskflow", "-d", "taskflow_lesson41_test",
                        "-v", "ON_ERROR_STOP=1", "-c", f"CREATE TABLE taskflow_test_lease(run_id text NOT NULL); INSERT INTO taskflow_test_lease VALUES ('{marker}');"], capture=True)
            env.update({"TASKFLOW_CACHE_TEST_ADDR": "127.0.0.1:" + port,
                        "TASKFLOW_CACHE_TEST_RUN_ID": marker,
                        "TASKFLOW_INTEGRATION_RUN_ID": marker,
                        "TASKFLOW_CACHE_TEST_DATABASE_URL": pg_url,
                        "TASKFLOW_CACHE_TEST_CONTAINER_ID": container,
                        "TASKFLOW_CACHE_TEST_PASSWORD_FILE": str(directory / "secrets/redis_password")})

            def seed_marker():
                run(base + ["exec", "-T", "redis", "sh", "-ec",
                            'export REDISCLI_AUTH="$(cat /run/secrets/redis_password)"; exec redis-cli --user taskflow_cache "$@"',
                            "fixture", "SET", "taskflow:cache:test-run:" + marker, marker], capture=True)

            seed_marker()
            if performance:
                env["TASKFLOW_CACHE_PERFORMANCE"] = "true"
                report = ROOT / "artifacts/lesson42"
                report.mkdir(parents=True, exist_ok=True)
                env["TASKFLOW_CACHE_REPORT_DIR"] = str(report)
                metadata = {"host": platform.platform(), "cpu_count": os.cpu_count(),
                            "load_average_before": os.getloadavg(),
                            "redis_image_id": details["Image"], "postgres_image_id": pg_details["Image"],
                            "redis_maxmemory_test": "4mb", "redis_maxmemory_production": "64mb",
                            "redis_memory_limit_bytes": details["HostConfig"]["Memory"],
                            "postgres_memory_limit_bytes": pg_details["HostConfig"]["Memory"],
                            "redis_cpus": details["HostConfig"]["NanoCpus"] / 1e9,
                            "test_transport": "loopback HTTP -> real Gin -> Redis/PostgreSQL; no Nginx/TLS",
                            "rate_limits": "unchanged Go 300/peer/minute, 1200/global/minute; new router per group",
                            "race_detector": False}
                run(["go", "test", "-count=1", "-timeout=240s", "-v", "./test/integration/cache", "-run", "^TestRedisPostgresPerformanceComparison$"], env=env, timeout=270)
                metadata["load_average_after"] = os.getloadavg()
                metadata["container_stats_after"] = [json.loads(line) for line in run(
                    ["docker", "stats", "--no-stream", "--format", "{{json .}}", container, pg_container],
                    capture=True).splitlines()]
                # Docker stats contains random container names but no credentials.
                (report / "environment.json").write_text(json.dumps(metadata, indent=2) + "\n")
                return
            run(["go", "test", "-race", "-count=1", "-timeout=30s", "-v", "./test/integration/cache", "-run", "^TestRedis(Client|TodayService|TodayWrites)Integration$"] , env=env)
            run(["go", "test", "-race", "-shuffle=on", "-count=1", "-timeout=90s", "-v", "./test/integration/cache", "-run", "^TestRedisPostgres"], env=env)
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
            print("PASS: Redis/PostgreSQL HTTP, revoked sessions, fault recovery, concurrent misses, ACL and restart")
        finally:
            if container is not None:
                details = json.loads(run(["docker", "inspect", container], capture=True))[0]
                if details["Config"]["Labels"].get("taskflow.cache-test") == marker and details["State"].get("Paused"):
                    run(["docker", "unpause", container], capture=True)
            run(base + ["down", "--remove-orphans", "--volumes", "--timeout", "5"], capture=True)
            print("Only this invocation's disposable cache project removed")


if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, subprocess.TimeoutExpired) as error:
        print(str(error) if isinstance(error, RuntimeError) else "cache test timed out", file=sys.stderr)
        sys.exit(1)
