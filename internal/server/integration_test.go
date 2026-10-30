package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pluto-han/taskflow/internal/auth"
	"github.com/pluto-han/taskflow/internal/project"
	"github.com/pluto-han/taskflow/internal/session"
	"github.com/pluto-han/taskflow/internal/task"
	"github.com/pluto-han/taskflow/internal/user"
)

// This is a response shape check for the subset used here, not a replacement for
// a complete OpenAPI validator. Required fields, types, nullability and enums
// are checked against real responses from the production router.
func checkShape(t *testing.T, d document, s schema, value any) {
	t.Helper()
	if s.Ref != "" {
		s = d.Components.Schemas[strings.TrimPrefix(s.Ref, "#/components/schemas/")]
	}
	if value == nil {
		if !s.Nullable {
			t.Error("unexpected null")
		}
		return
	}
	switch s.Type {
	case "object":
		m, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("expected object: %T", value)
		}
		for _, k := range s.Required {
			if _, ok := m[k]; !ok {
				t.Errorf("required field absent: %s", k)
			}
		}
		for k, v := range m {
			property, ok := s.Properties[k]
			if !ok {
				t.Errorf("undocumented response field: %s", k)
				continue
			}
			checkShape(t, d, property, v)
		}
	case "array":
		items, ok := value.([]any)
		if !ok || s.Items == nil {
			t.Fatal("invalid array schema/response")
		}
		for _, v := range items {
			checkShape(t, d, *s.Items, v)
		}
	case "string":
		str, ok := value.(string)
		if !ok {
			t.Fatalf("expected string: %T", value)
		}
		if len(s.Enum) > 0 {
			found := false
			for _, v := range s.Enum {
				if str == v {
					found = true
				}
			}
			if !found {
				t.Errorf("unknown enum value %q", str)
			}
		}
	case "integer":
		n, ok := value.(float64)
		if !ok || n != float64(int64(n)) {
			t.Errorf("expected integer: %v", value)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			t.Errorf("expected boolean: %T", value)
		}
	default:
		t.Fatalf("unhandled schema type %q", s.Type)
	}
}

func TestAPIExamplesIntegration(t *testing.T) {
	url := os.Getenv("TASKFLOW_API_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TASKFLOW_API_TEST_DATABASE_URL to isolated migrated taskflow_lesson17_test")
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil || cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Database != "taskflow_lesson17_test" {
		t.Fatal("requires isolated loopback taskflow_lesson17_test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var version int
	var dirty bool
	if err := pool.QueryRow(ctx, "SELECT version,dirty FROM schema_migrations").Scan(&version, &dirty); err != nil || version != 9 || dirty {
		t.Fatal("requires clean migration 9")
	}
	users := user.NewPostgresRepository(pool)
	projects := project.NewPostgresRepository(pool)
	sessions, err := session.NewService(ctx, users, session.NewPostgresRepository(pool), auth.PasswordHasher{})
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	r := NewRouter(Dependencies{Users: user.NewService(users, auth.PasswordHasher{}), Sessions: sessions, Tasks: task.NewService(task.NewPostgresRepository(pool), projects), Projects: project.NewService(projects), Ping: pool.Ping, Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
	d, tree := readContract(t)
	paths := tree["paths"].(map[string]any)
	sample := func(path, method string) string {
		t.Helper()
		op := paths[path].(map[string]any)[method].(map[string]any)
		value := op["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["example"]
		b, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	credential := ""
	request := func(method, path, contractPath, body string, status int) map[string]any {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if credential != "" {
			req.Header.Set("Authorization", "Bearer "+credential)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != status {
			t.Fatalf("%s %s: got %d want %d", method, contractPath, rec.Code, status)
		}
		if rec.Header().Get("X-Request-ID") == "" {
			t.Fatal("missing request ID")
		}
		rawResponse, ok := d.Paths[contractPath][strings.ToLower(method)].Responses[strconv.Itoa(status)]
		if !ok {
			t.Fatal("undocumented response status")
		}
		if status == 204 {
			if rec.Body.Len() != 0 {
				t.Fatal("204 body")
			}
			return nil
		}
		var response struct {
			Ref     string `json:"$ref"`
			Content map[string]struct {
				Schema schema `json:"schema"`
			} `json:"content"`
		}
		if json.Unmarshal(rawResponse, &response) != nil {
			t.Fatal("invalid response contract")
		}
		if response.Ref != "" {
			component := tree["components"].(map[string]any)["responses"].(map[string]any)[strings.TrimPrefix(response.Ref, "#/components/responses/")]
			raw, _ := json.Marshal(component)
			if json.Unmarshal(raw, &response) != nil {
				t.Fatal("invalid referenced response")
			}
		}
		var value any
		if json.Unmarshal(rec.Body.Bytes(), &value) != nil {
			t.Fatal("invalid response JSON")
		}
		checkShape(t, d, response.Content["application/json"].Schema, value)
		result, _ := value.(map[string]any)
		return result
	}
	account := request("POST", "/auth/register", "/auth/register", sample("/auth/register", "post"), 201)
	uid := account["id"].(string)
	defer func() {
		clean, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		for _, table := range []string{"tasks", "projects", "users"} {
			column := "owner_id"
			if table == "users" {
				column = "id"
			}
			if _, err := pool.Exec(clean, "DELETE FROM "+table+" WHERE "+column+"=$1", uid); err != nil {
				t.Error(err)
			}
		}
	}()
	login := request("POST", "/auth/login", "/auth/login", sample("/auth/login", "post"), 200)
	credential = login["access_token"].(string)
	request("GET", "/auth/me", "/auth/me", "", 200)
	p := request("POST", "/projects", "/projects", sample("/projects", "post"), 201)
	pid := p["id"].(string)
	request("GET", "/projects", "/projects", "", 200)
	request("GET", "/projects/"+pid, "/projects/{id}", "", 200)
	request("PATCH", "/projects/"+pid, "/projects/{id}", sample("/projects/{id}", "patch"), 200)
	item := request("POST", "/tasks", "/tasks", sample("/tasks", "post"), 201)
	tid := item["id"].(string)
	request("GET", "/tasks?limit=1", "/tasks", "", 200)
	request("GET", "/tasks/"+tid, "/tasks/{id}", "", 200)
	request("PATCH", "/tasks/"+tid, "/tasks/{id}", sample("/tasks/{id}", "patch"), 200)
	request("PATCH", "/tasks/"+tid, "/tasks/{id}", fmt.Sprintf(`{"version":2,"project_id":%q}`, pid), 200)
	conflict := request("DELETE", "/projects/"+pid, "/projects/{id}", "", 409)
	if conflict["error"].(map[string]any)["code"] != "PROJECT_NOT_EMPTY" {
		t.Fatal("wrong deletion error")
	}
	request("PATCH", "/tasks/"+tid, "/tasks/{id}", sample("/tasks/{id}", "patch"), 409)
	request("DELETE", "/tasks/"+tid, "/tasks/{id}", "", 204)
	request("DELETE", "/projects/"+pid, "/projects/{id}", "", 204)
	request("GET", "/health/live", "/health/live", "", 200)
	request("GET", "/health/ready", "/health/ready", "", 200)
	request("POST", "/auth/logout", "/auth/logout", "", 204)
	request("GET", "/auth/me", "/auth/me", "", 401)
	for _, secret := range []string{credential, "a long learning passphrase", "learner@example.com", pid, tid} {
		if strings.Contains(logs.String(), secret) {
			t.Fatal("credential or actual resource ID in request log")
		}
	}
}
