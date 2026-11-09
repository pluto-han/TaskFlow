"""Real optional-cache rollout inside the disposable application smoke stack.

No real Discord token, server checkout, production network or database is used.
"""
import copy
import json
from pathlib import Path
import subprocess
import tempfile
from unittest.mock import patch

from scripts import apply_cache as activation
from scripts import init_cache
from scripts import prepare_cache as prep
from test.python.test_deploy_drill import drill as failed_release_drill

ROOT = Path(__file__).resolve().parents[2]


def drill(base, project, model, isolated, request, token, read_task):
    if not project.startswith("taskflow-smoke-"):
        raise RuntimeError("cache rollout only accepts disposable application projects")
    report = ROOT / "artifacts/lesson43"
    report.mkdir(parents=True, exist_ok=True)

    def run(args, check=True):
        result = subprocess.run(args, cwd=ROOT, text=True, capture_output=True, timeout=180)
        if check and result.returncode:
            raise RuntimeError(f"disposable cache rollout command failed (exit {result.returncode}); output omitted")
        return result

    # This listener stays inside this disposable API container; no host port.
    model["services"]["api"]["environment"]["TASKFLOW_METRICS_ADDR"] = model["services"]["api"]["networks"]["edge"]["ipv4_address"] + ":9091"
    isolated.write_text(json.dumps(model))
    run(base + ["up", "-d", "--no-deps", "--no-build", "--pull", "never", "--wait", "--wait-timeout", "120", "api"])
    initial = read_task()
    before = {service: run(base + ["ps", "-a", "-q", service]).stdout.strip() for service in ("postgres", "migrate")}
    cache_project = project.replace("taskflow-smoke-", "taskflow-cache-smoke-")
    network = cache_project + "_cache"
    with tempfile.TemporaryDirectory(prefix="cache-rollout-", dir=ROOT / ".local") as tmp:
        root = Path(tmp)
        init_cache.prepare(root / ".local/cache")
        (root / "compose.server.yaml").write_text(json.dumps(model))
        cache_model = json.loads(run(["docker", "compose", "-f", "compose.cache.yaml", "config", "--format", "json"]).stdout)
        cache_model["name"] = cache_project
        for definition in cache_model["networks"].values(): definition.pop("name", None)
        for key, file in (("redis_acl", "users.acl"), ("redis_password", "redis_password")):
            cache_model["secrets"][key]["file"] = str(root / ".local/cache" / file)
        cache_file = root / "cache.json"
        cache_file.write_text(json.dumps(cache_model))
        cache = ["docker", "compose", "--project-directory", str(ROOT), "-p", cache_project, "-f", str(cache_file)]
        app = ["docker", "compose", "--project-directory", str(root), "-p", project, "-f", str(root / "compose.server.yaml")]

        def validate_test_boundary(data):
            # Only project, test proxy addresses/origin and gateway port differ
            # from production. Unit tests exercise the production validator.
            assert data["name"] == project and set(data["services"]) == {"postgres", "migrate", "api", "web"}
            for name in ("postgres", "api", "migrate"): assert not data["services"][name].get("ports")
            ports = data["services"]["web"]["ports"]
            assert len(ports) == 1
            for key, value in model["services"]["web"]["ports"][0].items(): assert ports[0][key] == value
            assert data["services"]["api"]["networks"]["edge"] == model["services"]["api"]["networks"]["edge"]
            assert data["services"]["api"]["environment"]["TASKFLOW_PUBLIC_ORIGIN"] == model["services"]["api"]["environment"]["TASKFLOW_PUBLIC_ORIGIN"]

        def metrics():
            return run(app + ["exec", "-T", "web", "wget", "-q", "-O", "-", "http://api:9091/metrics"]).stdout

        def count(name):
            return next(float(line.rsplit(" ", 1)[1]) for line in metrics().splitlines() if line.startswith(name + " "))

        def stage(mode):
            print(f"Cache rollout stage: {mode}", flush=True)
            directory = prep.prepare(mode, root)
            # A normalized Compose roundtrip must not invalidate a prepared file.
            activation.apply(directory, root)
            return json.loads((root / "compose.server.yaml").read_text())

        with patch.object(prep, "CACHE_PROJECT", cache_project), patch.object(prep, "NETWORK_NAME", network), \
             patch.object(prep, "validate_server", side_effect=validate_test_boundary), \
             patch.object(activation.cd, "ROOT", root), patch.object(activation.cd, "PROJECT", project), \
             patch.object(activation.cd, "probe", read_task):
            try:
                stage("off")
                assert count("taskflow_today_cache_enabled") == 0
                # Missing external network fails before changing any containers.
                directory = prep.prepare("connect", root)
                api_before = run(app + ["ps", "-q", "api"]).stdout.strip()
                try:
                    activation.apply(directory, root)
                except RuntimeError as error:
                    assert "Required cache network" in str(error)
                else: raise RuntimeError("missing cache network was accepted")
                assert run(app + ["ps", "-q", "api"]).stdout.strip() == api_before
                run(cache + ["up", "-d", "--wait", "--wait-timeout", "90"])
                redis = run(cache + ["ps", "-q", "redis"]).stdout.strip()
                details = json.loads(run(["docker", "inspect", redis]).stdout)[0]
                assert details["Config"]["Labels"]["com.docker.compose.project"] == cache_project
                assert not details["HostConfig"].get("PortBindings")
                stage("connect")
                assert count("taskflow_today_cache_enabled") == 0
                stage("enable")
                assert count("taskflow_today_cache_enabled") == 1
                first = request("GET", "/api/tasks/today?limit=20", token=token)
                hits = count('taskflow_today_cache_reads_total{outcome="hit"}')
                request("GET", "/api/tasks/today?limit=20", token=token)
                assert count('taskflow_today_cache_reads_total{outcome="hit"}') == hits + 1
                item = request("POST", "/api/tasks/today", {"plan_date": first["plan_date"], "title": "Cache rollout", "priority": 2}, token, 201)
                assert any(x["id"] == item["id"] for x in request("GET", "/api/tasks/today", token=token)["items"])
                other = {"email": "cache-rollout@example.com", "password": "isolated cache rollout passphrase"}
                request("POST", "/api/auth/register", other, expected=201)
                other_token = request("POST", "/api/auth/login", other)["access_token"]
                assert request("GET", "/api/tasks/today", token=other_token)["items"] == []
                failed_release_drill(app, project, read_task)
                assert count("taskflow_today_cache_enabled") == 1
                run(["docker", "pause", redis])
                try:
                    request("GET", "/api/health/ready")
                    request("PATCH", "/api/tasks/" + item["id"], {"version": 1, "completed": True}, token)
                    page = request("GET", "/api/tasks/today", token=token)
                    assert next(x for x in page["items"] if x["id"] == item["id"])["completed"]
                    assert count('taskflow_today_cache_reads_total{outcome="error"}') >= 1
                finally:
                    run(["docker", "unpause", redis])
                stage("off")
                assert count("taskflow_today_cache_enabled") == 0
                run(cache + ["stop", "redis"])
                request("GET", "/api/tasks/today", token=token)
                request("GET", "/api/health/ready")
                # Demonstrate real Compose's hard network dependency, separately
                # from our preflight. Previous OFF config can still be restored.
                previous = json.loads((root / "compose.server.yaml").read_text())
                missing = copy.deepcopy(previous)
                missing["networks"][prep.NETWORK]["name"] = network + "-missing"
                bad = root / "missing-network.json"; bad.write_text(json.dumps(missing))
                old = root / "previous.json"; old.write_text(json.dumps(previous))
                try:
                    activation.cd.activate_with_rollback(bad, old)
                except RuntimeError as error:
                    assert str(error) == "deployment failed; previous images restored"
                else: raise RuntimeError("Compose accepted a missing external network")
                detached = stage("detach")
                api = run(app + ["ps", "-q", "api"]).stdout.strip()
                assert network not in json.loads(run(["docker", "inspect", api]).stdout)[0]["NetworkSettings"]["Networks"]
                assert prep.SECRET not in detached["secrets"]
                assert read_task() == initial
                for service, cid in before.items(): assert run(app + ["ps", "-a", "-q", service]).stdout.strip() == cid
                # Let the caller continue its recurrence/Discord drills using the
                # final detached config, including its private metrics listener.
                model.clear(); model.update(detached); isolated.write_text(json.dumps(model))
                print("PASS: real cache OFF/connect/ON, hit, isolation, failed release rollback, Redis pause, OFF/detach, missing network refusal; PostgreSQL/migrate unchanged", flush=True)
                (report / "rollout.json").write_text(json.dumps({"status": "passed", "checks": ["off-connect-enable", "hit", "owner-isolation", "cached-release-rollback", "paused-fallback-write", "off-detach", "missing-network", "pg-job-unchanged"]}) + "\n")
            finally:
                # Restore the original cache-free app before removing this
                # invocation's Redis network, including on a failed assertion.
                restore = root / "cleanup-app.json"
                restored = prep.cache_model(model, "detach", root / ".local/cache/redis_password", network)
                restore.write_text(json.dumps(restored))
                run(app[:-1] + [str(restore), "up", "-d", "--no-deps", "--no-build", "--pull", "never", "--wait", "--wait-timeout", "120", "api"])
                run(cache + ["down", "--remove-orphans"])
