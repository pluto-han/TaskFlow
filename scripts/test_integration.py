"""Run Go integration/race tests against disposable PostgreSQL; never accepts a DSN."""
import argparse
import json
import os
from pathlib import Path
import secrets
import shutil
import signal
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[1]
SUITES = [
    ("TASKFLOW_TEST_DATABASE_URL", 12, 4),
    ("TASKFLOW_SESSION_TEST_DATABASE_URL", 13, 5),
    ("TASKFLOW_TASK_TEST_DATABASE_URL", 14, 10),
    ("TASKFLOW_PAGINATION_TEST_DATABASE_URL", 15, 10),
    ("TASKFLOW_PROJECT_TEST_DATABASE_URL", 16, 10),
    ("TASKFLOW_API_TEST_DATABASE_URL", 17, 10),
    ("TASKFLOW_SHARING_TEST_DATABASE_URL", 20, 10),
    ("TASKFLOW_RELIABILITY_TEST_DATABASE_URL", 21, 10),
    ("TASKFLOW_TODAY_TEST_DATABASE_URL", 29, 10),
]


def run(args, *, env=None, capture=False, input=None, timeout=180):
    # Errors deliberately omit command arguments: migrate receives a temporary DSN.
    result = subprocess.run(args, cwd=ROOT, env=env, text=True, input=input,
                            stdout=subprocess.PIPE if capture else None,
                            stderr=subprocess.PIPE if capture else None, timeout=timeout)
    if result.returncode:
        raise RuntimeError(f"{args[0]} failed (exit {result.returncode}); credentials omitted")
    return result.stdout.strip() if capture else ""


def round_tests(number, output):
    marker = secrets.token_hex(16)
    name = "taskflow-tests-" + marker
    password = secrets.token_hex(24)
    env = {k: v for k, v in os.environ.items()
           if not k.startswith("PG") and not k.startswith("TASKFLOW_") and k != "DATABASE_URL"}
    env["TASKFLOW_INTEGRATION_RUN_ID"] = marker
    # --rm and finally provide cleanup for success, test failure and interruption.
    try:
        run(["docker", "run", "--rm", "-d", "--name", name,
             "--label", "taskflow.test-run=" + marker,
             "-e", "POSTGRES_USER=taskflow", "-e", "POSTGRES_PASSWORD=" + password,
             "-p", "127.0.0.1::5432", "postgres:17"], capture=True)
        for _ in range(120):
            ready = subprocess.run(["docker", "exec", name, "pg_isready", "-h", "127.0.0.1", "-U", "taskflow"],
                                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=5)
            if ready.returncode == 0:
                break
            time.sleep(.25)
        else:
            raise RuntimeError("isolated PostgreSQL startup timeout")
        details = json.loads(run(["docker", "inspect", name], capture=True))[0]
        port = details["NetworkSettings"]["Ports"]["5432/tcp"][0]["HostPort"]
        for key, lesson, version in SUITES:
            db = f"taskflow_lesson{lesson}_test"
            run(["docker", "exec", name, "createdb", "-U", "taskflow", db], capture=True)
            url = f"postgres://taskflow:{password}@127.0.0.1:{port}/{db}?sslmode=disable"
            run(["migrate", "-path", "migrations", "-database", url, "goto", str(9 if lesson == 29 else version)], capture=True, env=env)
            def sql(query):
                return run(["docker", "exec", "-i", name, "psql", "-X", "-U", "taskflow", "-d", db,
                            "-At", "-v", "ON_ERROR_STOP=1"], capture=True, input=query)
            if lesson == 29:
                # Upgrade a nonempty v9 database, not just a clean v10 installation.
                sql("""
                    INSERT INTO users(id,email,password_hash) VALUES
                      ('00000000-0000-4000-8000-000000000090','upgrade@example.com','fixture-only');
                    INSERT INTO projects(id,owner_id,name) VALUES
                      ('00000000-0000-4000-8000-000000000091','00000000-0000-4000-8000-000000000090','Preserved project');
                    INSERT INTO tasks(id,title,priority,owner_id,project_id) VALUES
                      ('upgrade-task','Preserved task',2,'00000000-0000-4000-8000-000000000090','00000000-0000-4000-8000-000000000091');
                    INSERT INTO sessions(id,user_id,token_hash,expires_at) VALUES
                      ('00000000-0000-4000-8000-000000000092','00000000-0000-4000-8000-000000000090',decode(repeat('ab',32),'hex'),now()+interval '1 day');
                """)
                snapshot_query = """SELECT json_build_object(
                    'users',(SELECT json_agg(u ORDER BY id) FROM users u),
                    'projects',(SELECT json_agg(p ORDER BY id) FROM projects p),
                    'sessions',(SELECT json_agg(s ORDER BY id) FROM sessions s),
                    'tasks',(SELECT json_agg(t ORDER BY id) FROM
                      (SELECT id,title,description,completed,priority,created_at,due_date,version,owner_id,project_id FROM tasks) t));"""
                before = sql(snapshot_query)
                run(["migrate", "-path", "migrations", "-database", url, "goto", str(version)], capture=True, env=env)
                if before != sql(snapshot_query):
                    raise RuntimeError("v9 upgrade changed existing business data")
                if sql("SELECT count(*) FROM tasks WHERE plan_date IS NOT NULL;") != "0":
                    raise RuntimeError("v9 upgrade unexpectedly assigned historical plans")
                print("PASS: v9 -> v10 preserves task/project/user/session data", flush=True)
            if sql("SELECT version::text || ':' || dirty::text FROM schema_migrations;") != f"{version}:false":
                raise RuntimeError("migration did not reach a clean expected version")
            sql("CREATE TABLE taskflow_test_lease(run_id text NOT NULL); "
                f"INSERT INTO taskflow_test_lease VALUES ('{marker}');")
            env[key] = url
        print(f"Round {number}: {len(SUITES)} fresh migrated databases; running race/coverage tests", flush=True)
        profile = output / f"coverage-{number}.out"
        run(["go", "test", "-race", "-shuffle=on", "-count=1", "-timeout=120s", "-covermode=atomic",
             "-coverpkg=./internal/...", "-coverprofile=" + str(profile), "./internal/...", "./cmd/..."], env=env, timeout=300)
        report = run(["go", "tool", "cover", "-func=" + str(profile)], capture=True)
        (output / f"coverage-{number}.txt").write_text(report + "\n")
        print(report.splitlines()[-1], flush=True)
    finally:
        # Only the unique container name generated by this invocation is removed.
        subprocess.run(["docker", "rm", "-f", name], stdout=subprocess.DEVNULL,
                       stderr=subprocess.DEVNULL, timeout=30)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repeat", type=int, default=3, help="fresh database rounds (default: 3)")
    args = parser.parse_args()
    if not 1 <= args.repeat <= 20:
        parser.error("--repeat must be between 1 and 20")
    for tool in ("docker", "go", "migrate"):
        if not shutil.which(tool):
            parser.error(f"missing required tool: {tool}")
    signal.signal(signal.SIGTERM, lambda *_: sys.exit(143))
    context_endpoint = run(
        ["docker", "context", "inspect", "--format", '{{.Endpoints.docker.Host}}'], capture=True)
    # Docker gives DOCKER_CONTEXT precedence over DOCKER_HOST.
    endpoint = context_endpoint if os.environ.get("DOCKER_CONTEXT") else (
        os.environ.get("DOCKER_HOST") or context_endpoint)
    if not endpoint.startswith("unix://"):
        parser.error("requires a local Docker Unix socket; remote Docker endpoints are refused")
    output = ROOT / "artifacts" / "lesson21" / secrets.token_hex(8)
    output.mkdir(parents=True, exist_ok=True)
    for number in range(1, args.repeat + 1):
        round_tests(number, output)
    print(f"All {args.repeat} isolated rounds passed. Coverage: {output.relative_to(ROOT)}/")


if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, subprocess.TimeoutExpired) as error:
        # TimeoutExpired contains command arguments; never print it verbatim.
        print(str(error) if isinstance(error, RuntimeError) else "test command timed out", file=sys.stderr)
        sys.exit(1)
