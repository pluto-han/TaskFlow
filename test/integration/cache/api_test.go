package cache_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pluto-han/taskflow/internal/testdb"
)

func TestAPIOptionalCacheLifecycle(t *testing.T) {
	raw := os.Getenv("TASKFLOW_API_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("run make test-integration for the guarded disposable PostgreSQL")
	}
	cfg, err := pgxpool.ParseConfig(raw)
	if err != nil || cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Database != "taskflow_lesson17_test" {
		t.Fatal("isolated API database required")
	}
	testdb.Verify(t, cfg)
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	binary := filepath.Join(directory, "api")
	// A cold normal build uses a different cache variant from the surrounding
	// race/coverage tests and can take longer on shared CI runners.
	const buildTimeout = 90 * time.Second
	buildCtx, buildCancel := context.WithTimeout(context.Background(), buildTimeout)
	defer buildCancel()
	build := exec.CommandContext(buildCtx, "go", "build", "-o", binary, "./cmd/api")
	build.Dir = root
	if err := build.Run(); err != nil {
		if buildCtx.Err() != nil {
			t.Fatalf("API lifecycle fixture build timed out after %s", buildTimeout)
		}
		t.Fatal("could not build API lifecycle fixture")
	}
	password := filepath.Join(directory, "password")
	if err := os.WriteFile(password, []byte("fixture-redis-password\n"), 0600); err != nil {
		t.Fatal("cannot prepare fixture password")
	}
	for _, enabled := range []string{"false", "true"} {
		t.Run("cache="+enabled, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			addr := listener.Addr().String()
			_ = listener.Close()
			env := []string{}
			for _, value := range os.Environ() {
				key, _, _ := strings.Cut(value, "=")
				if !strings.HasPrefix(key, "TASKFLOW_") && !strings.HasPrefix(key, "REDIS_") &&
					!strings.HasPrefix(key, "DISCORD_") && !strings.HasPrefix(key, "PG") &&
					key != "DATABASE_URL" && key != "DATABASE_URL_FILE" && key != "GIN_MODE" {
					env = append(env, value)
				}
			}
			secret := password
			if enabled == "false" {
				secret += ".does-not-exist"
			}
			env = append(env, "DATABASE_URL="+raw, "TASKFLOW_ENV=development", "TASKFLOW_HTTP_ADDR="+addr,
				"TASKFLOW_CACHE_ENABLED="+enabled, "REDIS_ADDR=127.0.0.1:1", "REDIS_USERNAME=taskflow_cache", "REDIS_PASSWORD_FILE="+secret)
			command := exec.Command(binary)
			command.Env = env
			command.Stdout, command.Stderr = io.Discard, io.Discard
			if err := command.Start(); err != nil {
				t.Fatal("could not start API fixture")
			}
			done := make(chan error, 1)
			go func() { done <- command.Wait() }()
			t.Cleanup(func() {
				_ = command.Process.Signal(syscall.SIGTERM)
				select {
				case err := <-done:
					if err != nil {
						t.Error("API fixture did not shut down cleanly")
					}
				case <-time.After(5 * time.Second):
					_ = command.Process.Kill()
					<-done
					t.Error("API fixture shutdown timed out")
				}
			})
			httpClient := &http.Client{Timeout: 100 * time.Millisecond}
			defer httpClient.CloseIdleConnections()
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				response, err := httpClient.Get("http://" + addr + "/health/ready")
				if err == nil {
					_ = response.Body.Close()
					if response.StatusCode == http.StatusOK {
						return
					}
				}
				time.Sleep(20 * time.Millisecond)
			}
			t.Fatal("optional cache prevented API readiness")
		})
	}
}
