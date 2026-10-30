// Package testdb guards opt-in integration tests before they modify any data.
package testdb

import (
	"context"
	"encoding/hex"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func validate(cfg *pgxpool.Config, runID string) error {
	raw, err := hex.DecodeString(runID)
	if err != nil || len(raw) != 16 {
		return errors.New("use python3 scripts/test_integration.py: missing valid run marker")
	}
	c := cfg.ConnConfig
	if c.Host != "127.0.0.1" || c.Port == 5432 || c.User != "taskflow" || len(c.Fallbacks) != 0 {
		return errors.New("requires dedicated loopback container port, taskflow user, no fallback hosts")
	}
	return nil
}

// Verify checks a random lease written by the disposable-container runner.
// Existing tests also enforce their exact database name and schema version.
func Verify(t *testing.T, cfg *pgxpool.Config) {
	t.Helper()
	runID := os.Getenv("TASKFLOW_INTEGRATION_RUN_ID")
	if err := validate(cfg, runID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg.Copy())
	if err != nil {
		t.Fatal("cannot open isolated test database")
	}
	defer pool.Close()
	var marker string
	if err := pool.QueryRow(ctx, "SELECT run_id FROM taskflow_test_lease").Scan(&marker); err != nil || marker != runID {
		t.Fatal("database does not belong to this test run; refusing fixture writes")
	}
}
