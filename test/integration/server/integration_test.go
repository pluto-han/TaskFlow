package server_test

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
	applicationaccount "github.com/pluto-han/taskflow/internal/application/account"
	applicationintegration "github.com/pluto-han/taskflow/internal/application/integration"
	applicationnotification "github.com/pluto-han/taskflow/internal/application/notification"
	applicationproject "github.com/pluto-han/taskflow/internal/application/project"
	applicationrecurrence "github.com/pluto-han/taskflow/internal/application/recurrence"
	applicationsession "github.com/pluto-han/taskflow/internal/application/session"
	applicationtask "github.com/pluto-han/taskflow/internal/application/task"
	infrapassword "github.com/pluto-han/taskflow/internal/infra/password"
	infraaccount "github.com/pluto-han/taskflow/internal/infra/persistence/account"
	infraintegration "github.com/pluto-han/taskflow/internal/infra/persistence/integration"
	infranotification "github.com/pluto-han/taskflow/internal/infra/persistence/notification"
	infraproject "github.com/pluto-han/taskflow/internal/infra/persistence/project"
	infrarecurrence "github.com/pluto-han/taskflow/internal/infra/persistence/recurrence"
	infrasession "github.com/pluto-han/taskflow/internal/infra/persistence/session"
	infratask "github.com/pluto-han/taskflow/internal/infra/persistence/task"
	interfaceshttprouter "github.com/pluto-han/taskflow/internal/interfaces/http/router"
	"github.com/pluto-han/taskflow/test/support/testdb"
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
	testdb.Verify(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var version int
	var dirty bool
	if err := pool.QueryRow(ctx, "SELECT version,dirty FROM schema_migrations").Scan(&version, &dirty); err != nil || version != 17 || dirty {
		t.Fatal("requires clean migration 15")
	}
	users := infraaccount.New(pool)
	projects := infraproject.New(pool)
	sessions, err := applicationsession.New(ctx, users, infrasession.New(pool), infrapassword.PasswordHasher{})
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	r := interfaceshttprouter.New(interfaceshttprouter.Dependencies{Notifications: applicationnotification.New(infranotification.New(pool, "default"), nil), Integrations: applicationintegration.New(infraintegration.New(pool), true), Recurrences: applicationrecurrence.NewWithClock(infrarecurrence.New(pool), func() time.Time { return time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC) }), Users: applicationaccount.New(users, infrapassword.PasswordHasher{}), Sessions: sessions, Tasks: applicationtask.New(infratask.New(pool), projects), Projects: applicationproject.New(projects, projects), Ping: pool.Ping, Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
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
		for _, table := range []string{"tasks", "notification_outbox", "recurrence_occurrences", "recurrence_rules", "discord_interactions", "discord_bindings", "projects", "users"} {
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

	rule := request("POST", "/recurrences", "/recurrences", `{"title":"Read","priority":2,"anchor_date":"2026-11-05","interval_days":3}`, 201)
	rid := rule["id"].(string)
	request("GET", "/recurrences", "/recurrences", "", 200)
	request("GET", "/recurrences/"+rid, "/recurrences/{id}", "", 200)
	request("POST", "/recurrences/preview", "/recurrences/preview", `{"anchor_date":"2026-11-05","interval_days":3}`, 200)
	for _, body := range []string{
		`{"title":"x","priority":2,"anchor_date":"2026-11-05","interval_days":1.5}`,
		`{"owner_id":"forged","title":"x","priority":2,"anchor_date":"2026-11-05","interval_days":3}`,
		`{"title":"x","priority":2,"anchor_date":"2026-11-05","interval_days":0}`,
		`{"title":"x","priority":2,"anchor_date":"2026-11-05","interval_days":3} {}`,
	} {
		request("POST", "/recurrences", "/recurrences", body, 400)
	}
	request("GET", "/recurrences?limit=101", "/recurrences", "", 400)
	request("GET", "/recurrences?limit=1&limit=2", "/recurrences", "", 400)
	request("PATCH", "/recurrences/"+rid, "/recurrences/{id}", `{"version":1,"paused":true}`, 200)
	request("PATCH", "/recurrences/"+rid, "/recurrences/{id}", `{"version":1,"paused":false}`, 409)
	request("DELETE", "/recurrences/"+rid, "/recurrences/{id}", `{"version":2}`, 204)
	request("GET", "/recurrences/"+rid, "/recurrences/{id}", "", 404)

	request("GET", "/integrations/discord", "/integrations/discord", "", 200)
	request("GET", "/integrations/discord/notifications", "/integrations/discord/notifications", "", 200)
	bindingCode := request("POST", "/integrations/discord/code", "/integrations/discord/code", "", 201)
	request("POST", "/integrations/discord/code", "/integrations/discord/code", `{ "owner_id": "forged" }`, 400)
	request("POST", "/integrations/discord/code", "/integrations/discord/code", ` { } `, 201)
	if len(bindingCode["code"].(string)) != 43 {
		t.Fatal("invalid binding code")
	}
	request("DELETE", "/integrations/discord", "/integrations/discord", `{"version":1,"owner_id":"forged"}`, 400)
	request("DELETE", "/integrations/discord", "/integrations/discord", `{"version":1}`, 204)
	request("DELETE", "/integrations/discord", "/integrations/discord", `{"version":1}`, 409)
	if strings.Contains(logs.String(), bindingCode["code"].(string)) {
		t.Fatal("binding code in log")
	}
	request("POST", "/auth/logout", "/auth/logout", "", 204)
	request("GET", "/auth/me", "/auth/me", "", 401)
	for _, secret := range []string{credential, "a long learning passphrase", "learner@example.com", pid, tid} {
		if strings.Contains(logs.String(), secret) {
			t.Fatal("credential or actual resource ID in request log")
		}
	}
}
