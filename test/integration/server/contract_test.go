package server_test

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/pluto-han/taskflow/internal/httpapi"
	"github.com/pluto-han/taskflow/internal/integration"
	"github.com/pluto-han/taskflow/internal/notification"
	"github.com/pluto-han/taskflow/internal/project"
	"github.com/pluto-han/taskflow/internal/recurrence"
	"github.com/pluto-han/taskflow/internal/server"
	"github.com/pluto-han/taskflow/internal/session"
	"github.com/pluto-han/taskflow/internal/task"
	"github.com/pluto-han/taskflow/internal/user"
)

type schema struct {
	Type       string            `json:"type"`
	Ref        string            `json:"$ref"`
	Properties map[string]schema `json:"properties"`
	Required   []string          `json:"required"`
	Nullable   bool              `json:"nullable"`
	Items      *schema           `json:"items"`
	Enum       []string          `json:"enum"`
}
type operation struct {
	OperationID string                     `json:"operationId"`
	Responses   map[string]json.RawMessage `json:"responses"`
	Security    []map[string][]string      `json:"security"`
	Parameters  []struct {
		Name     string `json:"name"`
		In       string `json:"in"`
		Required bool   `json:"required"`
	} `json:"parameters"`
}
type document struct {
	Security   []map[string][]string           `json:"security"`
	OpenAPI    string                          `json:"openapi"`
	Paths      map[string]map[string]operation `json:"paths"`
	Components struct {
		Schemas map[string]schema `json:"schemas"`
	} `json:"components"`
	Codes map[string][]string `json:"x-error-status-codes"`
}

func readContract(t *testing.T) (document, map[string]any) {
	t.Helper()
	raw, err := os.ReadFile("../../../api/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	var d document
	var tree map[string]any
	if json.Unmarshal(raw, &d) != nil || json.Unmarshal(raw, &tree) != nil {
		t.Fatal("invalid JSON contract")
	}
	return d, tree
}
func testRouter() *gin.Engine {
	return server.NewRouter(server.Dependencies{Users: user.NewService(nil, nil), Ping: func(context.Context) error { return nil }, Logger: slog.New(slog.NewJSONHandler(io.Discard, nil))})
}

func TestContractRoutesAndReferences(t *testing.T) {
	gin.SetMode(gin.TestMode)
	d, tree := readContract(t)
	if d.OpenAPI != "3.0.3" {
		t.Fatal("unexpected contract version")
	}
	routes := map[string]bool{}
	ids := map[string]bool{}
	for _, r := range testRouter().Routes() {
		path := strings.NewReplacer(":id", "{id}", ":userID", "{userID}", ":invitationID", "{invitationID}").Replace(r.Path)
		routes[strings.ToLower(r.Method)+" "+path] = true
	}
	for path, methods := range d.Paths {
		for method, op := range methods {
			key := method + " " + path
			if !routes[key] {
				t.Errorf("documented route missing: %s", key)
			}
			delete(routes, key)
			if op.OperationID == "" || ids[op.OperationID] {
				t.Error("missing/duplicate operation ID")
			}
			ids[op.OperationID] = true
			if len(op.Responses) == 0 {
				t.Error("no responses")
			}
			if strings.Contains(path, "{id}") {
				found := false
				for _, p := range op.Parameters {
					if p.Name == "id" && p.In == "path" && p.Required {
						found = true
					}
				}
				if !found {
					t.Error("missing required path parameter")
				}
			}
			public := strings.HasPrefix(path, "/health") || path == "/auth/register" || path == "/auth/login" || path == "/auth/browser/login" || path == "/discord/interactions"
			want := []map[string][]string(nil) // GET business routes inherit both mechanisms.
			if public {
				want = []map[string][]string{}
			} else if path == "/auth/me" || path == "/auth/logout" {
				want = []map[string][]string{{"bearerSession": {}}}
			} else if path == "/auth/browser/session" {
				want = []map[string][]string{{"browserSession": {}}}
			} else if path == "/auth/browser/logout" {
				want = []map[string][]string{{"browserSession": {}, "csrfToken": {}}}
			} else if method != "get" && method != "head" && method != "options" {
				want = []map[string][]string{{"bearerSession": {}}, {"browserSession": {}, "csrfToken": {}}}
			}
			if !reflect.DeepEqual(op.Security, want) {
				t.Errorf("incorrect authentication/CSRF contract: %s %s", method, path)
			}
		}
	}
	if len(routes) != 0 {
		t.Fatalf("undocumented routes: %v", routes)
	}
	var walk func(any)
	walk = func(node any) {
		switch v := node.(type) {
		case map[string]any:
			if raw, ok := v["$ref"].(string); ok {
				if !strings.HasPrefix(raw, "#/") {
					t.Errorf("external/unresolvable reference %s", raw)
					return
				}
				var current any = tree
				for _, part := range strings.Split(strings.TrimPrefix(raw, "#/"), "/") {
					m, ok := current.(map[string]any)
					if !ok {
						t.Fatalf("invalid reference %s", raw)
					}
					current, ok = m[part]
					if !ok {
						t.Fatalf("missing reference %s", raw)
					}
				}
			}
			for _, child := range v {
				walk(child)
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		}
	}
	walk(tree)
}

func TestContractDTOFields(t *testing.T) {
	d, _ := readContract(t)
	if !reflect.DeepEqual(d.Security, []map[string][]string{{"bearerSession": {}}, {"browserSession": {}}}) {
		t.Fatal("global authentication must allow either mechanism, never require both")
	}
	for name, value := range map[string]any{"SessionInfo": session.SessionInfo{}, "SessionPage": session.SessionPage{}, "PasswordChangeRequest": session.PasswordChangeRequest{}, "BrowserSession": session.BrowserResponse{}, "NotificationEvent": notification.Event{}, "DiscordBinding": integration.Binding{}, "DiscordBindingCode": integration.Code{}, "UnbindDiscord": integration.UnbindRequest{}, "Recurrence": recurrence.Rule{}, "CreateRecurrence": recurrence.CreateRequest{}, "UpdateRecurrence": recurrence.UpdateRequest{}, "DeleteRecurrence": recurrence.DeleteRequest{}, "PreviewRecurrenceRequest": recurrence.PreviewRequest{}, "RecurrencePreview": recurrence.Preview{}, "RecurrencePage": recurrence.Page{}, "RegisterRequest": user.RegisterRequest{}, "LoginRequest": session.LoginRequest{}, "LoginResponse": session.LoginResponse{}, "User": user.UserResponse{}, "Task": task.Task{}, "CreateTask": task.CreateTaskRequest{}, "CreateTodayTask": task.CreateTodayTaskRequest{}, "TodayPage": task.TodayPage{}, "UpdateTask": task.UpdateTaskRequest{}, "Member": project.Member{}, "Invitation": project.Invitation{}, "InviteRequest": project.InviteRequest{}, "AcceptRequest": project.AcceptRequest{}, "Project": project.Project{}, "CreateProject": project.CreateRequest{}, "UpdateProject": project.UpdateRequest{}, "Error": httpapi.ErrorResponse{}, "ErrorDetail": httpapi.ErrorDetail{}} {
		t.Run(name, func(t *testing.T) {
			fields := map[string]bool{}
			typ := reflect.TypeOf(value)
			for i := 0; i < typ.NumField(); i++ {
				tag := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
				if tag != "-" && tag != "" {
					fields[tag] = true
				}
			}
			documented, ok := d.Components.Schemas[name]
			if !ok {
				t.Fatal("missing schema")
			}
			for field := range documented.Properties {
				if !fields[field] {
					t.Errorf("extra documented field %s", field)
				}
				delete(fields, field)
			}
			if len(fields) != 0 {
				t.Fatalf("undocumented DTO fields %v", fields)
			}
		})
	}
}

// Scan actual WriteError calls so a new/removed/renumbered code requires updating
// the documented registry, rather than comparing two manually copied lists.
func TestContractErrorCodes(t *testing.T) {
	d, _ := readContract(t)
	actual := map[string]string{}
	statusNames := map[string]string{"StatusForbidden": "403", "StatusBadRequest": "400", "StatusUnauthorized": "401", "StatusNotFound": "404", "StatusMethodNotAllowed": "405", "StatusConflict": "409", "StatusRequestEntityTooLarge": "413", "StatusTooManyRequests": "429", "StatusInternalServerError": "500", "StatusServiceUnavailable": "503", "StatusGatewayTimeout": "504"}
	err := filepath.WalkDir("../../../internal", func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 4 {
				return true
			}
			name := ""
			switch f := call.Fun.(type) {
			case *ast.SelectorExpr:
				name = f.Sel.Name
			case *ast.Ident:
				name = f.Name
			}
			if name != "WriteError" {
				return true
			}
			literal, ok := call.Args[2].(*ast.BasicLit)
			if !ok {
				t.Errorf("nonliteral error code in %s; extend scanner", path)
				return true
			}
			code, _ := strconv.Unquote(literal.Value)
			status := ""
			switch v := call.Args[1].(type) {
			case *ast.BasicLit:
				status = v.Value
			case *ast.SelectorExpr:
				status = statusNames[v.Sel.Name]
			}
			if status == "" {
				t.Errorf("unresolved status for %s", code)
			}
			if previous, ok := actual[code]; ok && previous != status {
				t.Errorf("code %s has conflicting statuses", code)
			}
			actual[code] = status
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	documented := map[string]string{}
	for status, codes := range d.Codes {
		for _, code := range codes {
			documented[code] = status
		}
	}
	if !reflect.DeepEqual(actual, documented) {
		t.Fatalf("error registry drift\nactual %v\ndocumented %v", actual, documented)
	}
	enum := d.Components.Schemas["ErrorDetail"].Properties["code"].Enum
	if len(enum) != len(actual) {
		t.Fatal("error enum drift")
	}
	for _, code := range enum {
		if _, ok := actual[code]; !ok {
			t.Errorf("unknown enum %s", code)
		}
	}
}

func TestRouterErrorAndHealthContracts(t *testing.T) {
	d, _ := readContract(t)
	r := testRouter()
	for path, methods := range d.Paths {
		for method, op := range methods {
			url := strings.ReplaceAll(path, "{id}", "00000000-0000-4000-8000-000000000001")
			req := httptest.NewRequest(strings.ToUpper(method), url, strings.NewReader("{"))
			req.Header.Set("Content-Type", "application/json")
			if path == "/auth/browser/login" {
				req.Header.Set("Origin", "http://127.0.0.1:5173")
			}
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			expected := 401
			if path == "/discord/interactions" {
				expected = 503
			}
			if strings.HasPrefix(path, "/health") {
				expected = 200
			} else if path == "/auth/login" || path == "/auth/register" || path == "/auth/browser/login" {
				expected = 400
			}
			if rec.Code != expected {
				t.Fatalf("%s %s: %d %s", method, path, rec.Code, rec.Body.String())
			}
			if _, ok := op.Responses[strconv.Itoa(rec.Code)]; !ok {
				t.Errorf("undocumented HTTP status %d", rec.Code)
			}
			if rec.Header().Get("X-Request-ID") == "" {
				t.Error("missing request ID")
			}
			if expected != 200 {
				var body httpapi.ErrorResponse
				if json.Unmarshal(rec.Body.Bytes(), &body) != nil || body.Error.Code == "" || body.Error.Message == "" {
					t.Fatal("invalid error envelope")
				}
			}
		}
	}
	for _, tc := range []struct {
		method, path, code string
		status             int
	}{{"GET", "/missing", "ROUTE_NOT_FOUND", 404}, {"PUT", "/tasks", "METHOD_NOT_ALLOWED", 405}} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		var body httpapi.ErrorResponse
		if json.Unmarshal(rec.Body.Bytes(), &body) != nil || rec.Code != tc.status || body.Error.Code != tc.code {
			t.Fatal("fallback error contract")
		}
		if tc.status == 405 && rec.Header().Get("Allow") == "" {
			t.Fatal("405 must include Allow")
		}
	}
}
