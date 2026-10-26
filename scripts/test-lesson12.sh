#!/usr/bin/env bash
# Creates and removes only its own disposable PostgreSQL container and volume.
set -euo pipefail
cd "$(dirname "$0")/.."
for tool in docker migrate go; do
  command -v "$tool" >/dev/null || { echo "Missing tool: $tool" >&2; exit 1; }
done
container_id=$(docker run -d --rm -e POSTGRES_HOST_AUTH_METHOD=trust -e POSTGRES_DB=taskflow_lesson12_test -p 127.0.0.1::5432 postgres:17)
trap 'docker rm -fv "$container_id" >/dev/null 2>&1 || true' EXIT
ready=false
for ((i=0; i<60; i++)); do
  if docker exec "$container_id" pg_isready -h 127.0.0.1 -U postgres >/dev/null 2>&1; then ready=true; break; fi
  sleep 1
done
if [[ "$ready" != true ]]; then echo "Test PostgreSQL did not become ready" >&2; exit 1; fi
port=$(docker port "$container_id" 5432/tcp)
port=${port##*:}
test_url="postgres://postgres@127.0.0.1:${port}/taskflow_lesson12_test?sslmode=disable"
migrate -path migrations -database "$test_url" up
TASKFLOW_TEST_DATABASE_URL="$test_url" go test -race -count=3 ./internal/user -run TestPostgresRegistrationIntegration -v
# A second disposable database tests upgrading a populated version-3 schema.
docker exec "$container_id" createdb -U postgres taskflow_lesson12_upgrade
upgrade_url="postgres://postgres@127.0.0.1:${port}/taskflow_lesson12_upgrade?sslmode=disable"
migrate -path migrations -database "$upgrade_url" goto 3
docker exec "$container_id" psql -X -U postgres -d taskflow_lesson12_upgrade -v ON_ERROR_STOP=1 -c "INSERT INTO tasks(id,title,priority,due_date,version) VALUES ('preserve-me','Upgrade fixture',2,'2026-10-25T12:00:00Z',7);" >/dev/null
snapshot_sql="SELECT md5(coalesce(jsonb_agg(to_jsonb(t) ORDER BY id)::text,'[]')) FROM tasks t"
before=$(docker exec "$container_id" psql -X -At -U postgres -d taskflow_lesson12_upgrade -c "$snapshot_sql")
migrate -path migrations -database "$upgrade_url" up
after=$(docker exec "$container_id" psql -X -At -U postgres -d taskflow_lesson12_upgrade -c "$snapshot_sql")
[[ "$before" == "$after" ]] || { echo "Task data changed during upgrade" >&2; exit 1; }
state=$(docker exec "$container_id" psql -X -At -U postgres -d taskflow_lesson12_upgrade -c 'SELECT version, dirty FROM schema_migrations')
[[ "$state" == '4|f' ]] || { echo "Unexpected migration state: $state" >&2; exit 1; }
echo "Fresh replay, concurrent registration, and populated 3 -> 4 upgrade passed."
