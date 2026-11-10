package server_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pluto-han/taskflow/internal/auth"
	"github.com/pluto-han/taskflow/internal/config"
	"github.com/pluto-han/taskflow/internal/project"
	"github.com/pluto-han/taskflow/internal/server"
	"github.com/pluto-han/taskflow/internal/session"
	"github.com/pluto-han/taskflow/internal/task"
	"github.com/pluto-han/taskflow/internal/testdb"
	"github.com/pluto-han/taskflow/internal/user"
)

// Real TCP HTTP + production router + real hashing/session/sqlc/PostgreSQL.
func TestReliabilityHTTPIntegration(t *testing.T) {
	url := os.Getenv("TASKFLOW_RELIABILITY_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("run python3 scripts/test_integration.py")
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil || cfg.ConnConfig.Database != "taskflow_lesson21_test" {
		t.Fatal("requires dedicated reliability database")
	}
	testdb.Verify(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var version int
	var dirty bool
	if err = pool.QueryRow(ctx, "SELECT version,dirty FROM schema_migrations").Scan(&version, &dirty); err != nil || version != 14 || dirty {
		t.Fatal("requires clean schema 14")
	}

	t.Run("least privilege runtime role", func(t *testing.T) {
		if config.CheckRuntimeRole(ctx, pool) == nil {
			t.Fatal("superuser accepted as runtime role")
		}
		_, err := pool.Exec(ctx, `CREATE ROLE taskflow_security_runtime NOLOGIN;
   GRANT USAGE ON SCHEMA public TO taskflow_security_runtime;
   GRANT SELECT,INSERT ON users TO taskflow_security_runtime;
   GRANT SELECT,INSERT,UPDATE ON sessions TO taskflow_security_runtime;
   GRANT SELECT,INSERT,UPDATE,DELETE ON projects,tasks,project_memberships,project_invitations TO taskflow_security_runtime;`)
		if err != nil {
			t.Fatal(err)
		}
		limitedCfg := cfg.Copy()
		limitedCfg.ConnConfig.RuntimeParams["role"] = "taskflow_security_runtime"
		limited, err := pgxpool.NewWithConfig(ctx, limitedCfg)
		if err != nil {
			t.Fatal(err)
		}
		defer limited.Close()
		if err = config.CheckRuntimeRole(ctx, limited); err != nil {
			t.Fatal(err)
		}
		for _, query := range []string{"CREATE TABLE public.forbidden(id int)", "DELETE FROM users", "SELECT * FROM taskflow_test_lease"} {
			if _, err = limited.Exec(ctx, query); err == nil {
				t.Fatalf("unexpected privilege: %s", query)
			}
		}
		u, err := user.NewService(user.NewPostgresRepository(limited), auth.PasswordHasher{}).Register(ctx, user.RegisterRequest{Email: "limited-role@example.com", Password: "limited runtime passphrase"})
		if err != nil {
			t.Fatal(err)
		}
		pr := project.NewPostgresRepository(limited)
		owner := session.UserID(u.ID)
		p, err := project.NewService(pr).Create(ctx, owner, project.CreateRequest{Name: "limited project"})
		if err != nil {
			t.Fatal(err)
		}
		svc := task.NewService(task.NewPostgresRepository(limited), pr)
		item, err := svc.Create(ctx, owner, task.CreateTaskRequest{Title: "limited task", Priority: 1, ProjectID: &p.ID})
		if err != nil {
			t.Fatal(err)
		}
		if err = svc.Delete(ctx, owner, item.ID); err != nil {
			t.Fatal(err)
		}
	})
	users := user.NewPostgresRepository(pool)
	projects := project.NewPostgresRepository(pool)
	sessions, err := session.NewService(ctx, users, session.NewPostgresRepository(pool), auth.PasswordHasher{})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(server.NewRouter(server.Dependencies{Users: user.NewService(users, auth.PasswordHasher{}), Sessions: sessions, Projects: project.NewService(projects), Tasks: task.NewService(task.NewPostgresRepository(pool), projects), Ping: pool.Ping, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}))
	defer srv.Close()
	client := srv.Client()
	client.Timeout = 5 * time.Second
	request := func(t *testing.T, method, path, token, body string, status int) map[string]any {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, method, srv.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != status {
			t.Fatalf("%s %s: got %d want %d", method, path, resp.StatusCode, status)
		}
		if status == 204 {
			return nil
		}
		var result map[string]any
		if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	register := func(name string) (string, string) {
		t.Helper()
		body := fmt.Sprintf(`{"email":%q,"password":"reliability integration passphrase"}`, name+"@example.com")
		account := request(t, "POST", "/auth/register", "", body, 201)
		login := request(t, "POST", "/auth/login", "", body, 200)
		return account["id"].(string), login["access_token"].(string)
	}
	_, a := register("reliability-a")
	bid, b := register("reliability-b")
	cid, _ := register("reliability-c")
	item := request(t, "POST", "/tasks", a, `{"title":"private","priority":2}`, 201)
	tid := item["id"].(string)
	for _, method := range []string{"GET", "PATCH", "DELETE"} {
		request(t, method, "/tasks/"+tid, b, `{"version":1,"title":"attack"}`, 404)
	}
	request(t, "PATCH", "/tasks/"+tid, a, `{"version":1,"title":"updated"}`, 200)
	request(t, "PATCH", "/tasks/"+tid, a, `{"version":1,"title":"stale"}`, 409)
	if request(t, "GET", "/tasks/"+tid, a, "", 200)["title"] != "updated" {
		t.Fatal("stale write persisted")
	}

	t.Run("failure after ownership mutation rolls back entire transaction", func(t *testing.T) {
		p := request(t, "POST", "/projects", a, `{"name":"rollback"}`, 201)
		pid := p["id"].(string)
		invite := request(t, "POST", "/projects/"+pid+"/invitations", a, fmt.Sprintf(`{"user_id":%q}`, bid), 201)
		request(t, "POST", "/invitations/accept", b, fmt.Sprintf(`{"token":%q}`, invite["token"]), 200)
		request(t, "POST", "/projects/"+pid+"/invitations", a, fmt.Sprintf(`{"user_id":%q}`, cid), 201)
		snapshot := func() string {
			t.Helper()
			var raw string
			err := pool.QueryRow(ctx, `SELECT jsonb_build_object('projects',(SELECT jsonb_agg(p ORDER BY id) FROM projects p),'members',(SELECT jsonb_agg(m ORDER BY project_id,user_id) FROM project_memberships m),'invites',(SELECT jsonb_agg(i ORDER BY id) FROM project_invitations i))::text`).Scan(&raw)
			if err != nil {
				t.Fatal(err)
			}
			return raw
		}
		before := snapshot()
		// Fail the LAST write of Transfer, after both owner and memberships changed.
		_, err := pool.Exec(ctx, `CREATE FUNCTION test_fail_invitation_update() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected failure'; END $$;
   CREATE TRIGGER test_fail_transfer BEFORE UPDATE ON project_invitations FOR EACH ROW EXECUTE FUNCTION test_fail_invitation_update();`)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			clean, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			if _, err := pool.Exec(clean, `DROP TRIGGER IF EXISTS test_fail_transfer ON project_invitations; DROP FUNCTION IF EXISTS test_fail_invitation_update();`); err != nil {
				t.Error(err)
			}
		}()
		failed := request(t, "POST", "/projects/"+pid+"/ownership", a, fmt.Sprintf(`{"user_id":%q}`, bid), 500)
		detail := failed["error"].(map[string]any)
		if detail["code"] != "INTERNAL_ERROR" || detail["message"] != "Internal server error" {
			t.Fatal("database failure leaked")
		}
		if snapshot() != before {
			t.Fatal("partial ownership/membership/invitation mutation committed")
		}
	})
	request(t, "POST", "/auth/logout", a, "", 204)
	request(t, "GET", "/tasks/"+tid, a, "", 401)
	request(t, "GET", "/auth/me", b, "", 200)
	digest := sha256.Sum256([]byte(b))
	if _, err = pool.Exec(ctx, `UPDATE sessions SET created_at=now()-interval '2 days',expires_at=now()-interval '1 day' WHERE token_hash=$1`, digest[:]); err != nil {
		t.Fatal(err)
	}
	request(t, "GET", "/tasks/"+tid, b, "", 401)
}
