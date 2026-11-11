// Package browserflow runs a real browser and router against disposable PostgreSQL.
package e2e_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pluto-han/taskflow/internal/auth"
	"github.com/pluto-han/taskflow/internal/discord"
	"github.com/pluto-han/taskflow/internal/httpapi"
	"github.com/pluto-han/taskflow/internal/integration"
	email "github.com/pluto-han/taskflow/internal/mail"
	"github.com/pluto-han/taskflow/internal/notification"
	"github.com/pluto-han/taskflow/internal/project"
	"github.com/pluto-han/taskflow/internal/recurrence"
	"github.com/pluto-han/taskflow/internal/server"
	"github.com/pluto-han/taskflow/internal/session"
	"github.com/pluto-han/taskflow/internal/task"
	"github.com/pluto-han/taskflow/internal/testdb"
	"github.com/pluto-han/taskflow/internal/user"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestBrowserDiscordFlow(t *testing.T) {
	if os.Getenv("TASKFLOW_BROWSER_E2E") != "1" {
		t.Skip("use scripts/test_integration.py --browser-e2e")
	}
	cfg, err := pgxpool.ParseConfig(os.Getenv("TASKFLOW_BROWSER_TEST_DATABASE_URL"))
	if err != nil || cfg.ConnConfig.Database != "taskflow_lesson37_test" {
		t.Fatal("isolated browser database required")
	}
	testdb.Verify(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	bindings := integration.NewService(pool, true)
	sessionService, err := session.NewService(ctx, user.NewPostgresRepository(pool), session.NewPostgresRepository(pool), auth.PasswordHasher{})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	app := httptest.NewUnstartedServer(mux)
	defer app.Listener.Close()
	origin := "http://" + app.Listener.Addr().String()
	inbox := &email.MemorySender{}
	mailService, err := email.New(pool, inbox, origin)
	if err != nil {
		t.Fatal("mail fixture failed")
	}
	projects := project.NewPostgresRepository(pool)
	router := server.NewRouter(server.Dependencies{Mail: mailService, Security: httpapi.SecurityOptions{PublicOrigin: origin}, Notifications: notification.NewService(pool, nil), Discord: discord.NewHandler(public, "777", bindings), Integrations: bindings, Recurrences: recurrence.NewServiceWithScheduler(recurrence.NewPostgresRepository(pool), true), Users: user.NewService(user.NewPostgresRepository(pool), auth.PasswordHasher{}), Sessions: sessionService, Tasks: task.NewService(task.NewPostgresRepository(pool), projects), Projects: project.NewService(projects), Ping: pool.Ping})
	var mu sync.Mutex
	var messages []map[string]any
	fakeDiscord := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v10/channels/222/messages" || r.Header.Get("Authorization") != "Bot fixture-token" {
			http.Error(w, "unexpected request", 400)
			return
		}
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			http.Error(w, "bad json", 400)
			return
		}
		mu.Lock()
		messages = append(messages, body)
		mu.Unlock()
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer fakeDiscord.Close()
	localTransport := &http.Transport{Proxy: nil}
	defer localTransport.CloseIdleConnections()
	client, err := discord.NewMessageSender("fixture-token", transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "discord.com" {
			return nil, fmt.Errorf("unexpected destination")
		}
		copy := r.Clone(r.Context())
		u := *r.URL
		u.Scheme = "http"
		u.Host = strings.TrimPrefix(fakeDiscord.URL, "http://")
		copy.URL = &u
		copy.Host = u.Host
		return localTransport.RoundTrip(copy)
	}))
	if err != nil {
		t.Fatal(err)
	}
	sender := notification.NewService(pool, client)
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dist := filepath.Join(root, "frontend", "dist")
	if _, err = os.Stat(filepath.Join(dist, "index.html")); err != nil {
		t.Fatal("build frontend first: npm run build")
	}
	mux.Handle("/api/", http.StripPrefix("/api", router))
	mux.HandleFunc("/__fixture/bind", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method", 405)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1024))
		if err != nil {
			http.Error(w, "body", 400)
			return
		}
		var input struct {
			Code string `json:"code"`
		}
		if json.Unmarshal(body, &input) != nil {
			http.Error(w, "body", 400)
			return
		}
		payload := map[string]any{"id": "12345", "application_id": "777", "type": 2, "guild_id": "111", "channel_id": "222", "channel": map[string]any{"id": "222", "type": 0, "name": "test-channel"}, "member": map[string]any{"permissions": "1040", "user": map[string]any{"id": "333"}}, "app_permissions": "3072", "authorizing_integration_owners": map[string]string{"0": "111"}, "data": map[string]any{"name": "taskflow", "type": 1, "options": []any{map[string]any{"name": "bind", "type": 1, "options": []any{map[string]any{"name": "code", "type": 3, "value": input.Code}}}}}}
		raw, _ := json.Marshal(payload)
		timestamp := fmt.Sprint(time.Now().Unix())
		signature := hex.EncodeToString(ed25519.Sign(private, append([]byte(timestamp), raw...)))
		request := httptest.NewRequest("POST", discord.Path, bytes.NewReader(raw))
		request.Header.Set("X-Signature-Ed25519", signature)
		request.Header.Set("X-Signature-Timestamp", timestamp)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		w.WriteHeader(response.Code)
		_, _ = w.Write(response.Body.Bytes())
	})

	// Only this guarded disposable fixture exposes session fault controls.
	mux.HandleFunc("/__fixture/session/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method", 405)
			return
		}
		cookie, err := r.Cookie("taskflow_session_dev")
		if err != nil {
			http.Error(w, "session", 401)
			return
		}
		principal, err := sessionService.AuthenticateBrowser(r.Context(), cookie.Value)
		if err != nil {
			http.Error(w, "session", 401)
			return
		}
		switch r.URL.Path {
		case "/__fixture/session/revoke":
			err = sessionService.Logout(r.Context(), principal.Principal)
		case "/__fixture/session/expire":
			_, err = pool.Exec(r.Context(), "UPDATE sessions SET created_at=now()-interval '2 days',expires_at=now() WHERE id=$1 AND user_id=$2", principal.SessionID, string(principal.UserID))
		default:
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "session fixture failed", 500)
			return
		}
		w.WriteHeader(204)
	})
	// Test-only, behind the verified disposable database lease. Never mount in production.
	mux.HandleFunc("/__fixture/mail", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method", 405)
			return
		}
		if err := mailService.Poll(r.Context()); err != nil {
			http.Error(w, "mail fixture failed", 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(inbox.Take())
	})
	mux.HandleFunc("/__fixture/pump", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method", 405)
			return
		}
		result, err := recurrence.NewPostgresRepository(pool).GenerateBatch(r.Context(), time.Now)
		if err != nil || result.Generated != 1 {
			http.Error(w, "generation failed", 500)
			return
		}
		found, err := sender.ProcessOne(r.Context())
		if err != nil || !found {
			http.Error(w, "send failed", 500)
			return
		}
		w.WriteHeader(204)
	})
	mux.HandleFunc("/__fixture/messages", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(messages)
	})
	files := http.FileServer(http.Dir(dist))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/", "/login", "/register", "/profile", "/tasks", "/invite", "/forgot-password", "/reset-password", "/verify-email":
			http.ServeFile(w, r, filepath.Join(dist, "index.html"))
		default:
			files.ServeHTTP(w, r)
		}
	}))
	app.Start()
	defer app.Close()
	cmd := exec.CommandContext(ctx, "npx", "playwright", "test", "test/e2e/flow-live.spec.ts", "test/e2e/account-live.spec.ts", "--config", "playwright.flow.config.ts")
	cmd.Dir = filepath.Join(root, "frontend")
	cmd.Env = append(os.Environ(), "TASKFLOW_FLOW_BASE_URL="+app.URL)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatal("browser test failed", err)
	}
}
