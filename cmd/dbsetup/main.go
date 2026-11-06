// dbsetup is a one-shot administrative job. Never run it in API replicas.
package main

import (
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pluto-han/taskflow/internal/config"
)

func setup() error {
	cfg := config.Load()
	if err := cfg.ReadSecrets(); err != nil {
		return err
	}
	if cfg.DatabaseURL == "" {
		return errors.New("database secret required")
	}
	m, err := migrate.New("file:///migrations", cfg.DatabaseURL)
	if err != nil {
		return errors.New("cannot initialize migration")
	}
	defer m.Close()
	m.LockTimeout = 15 * time.Second
	if err = m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return errors.New("migration failed; application startup blocked")
	}
	password, err := os.ReadFile(os.Getenv("APP_PASSWORD_FILE"))
	if err != nil || len(strings.TrimSpace(string(password))) < 32 {
		return errors.New("runtime password secret missing or too short")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return errors.New("cannot initialize provisioning connection")
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return errors.New("cannot begin provisioning")
	}
	defer tx.Rollback(context.Background())
	// Serialize repeated provisioning jobs too; the migration driver has its own lock.
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(230023)"); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "SELECT set_config('taskflow.bootstrap_password',$1,true)", strings.TrimSpace(string(password))); err != nil {
		return errors.New("cannot bind runtime password")
	}
	_, err = tx.Exec(ctx, `DO $$ BEGIN
 IF NOT EXISTS(SELECT FROM pg_roles WHERE rolname='taskflow_app') THEN
  CREATE ROLE taskflow_app LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS;
 END IF;
 EXECUTE format('ALTER ROLE taskflow_app PASSWORD %L',current_setting('taskflow.bootstrap_password'));
 END $$;
 REVOKE CREATE ON SCHEMA public FROM PUBLIC;
 GRANT CONNECT ON DATABASE taskflow TO taskflow_app;
 GRANT USAGE ON SCHEMA public TO taskflow_app;
 GRANT SELECT,INSERT ON users TO taskflow_app;
 GRANT SELECT,INSERT,UPDATE ON sessions TO taskflow_app;
 GRANT SELECT,INSERT,UPDATE ON discord_bindings TO taskflow_app;
 GRANT SELECT,INSERT ON discord_interactions TO taskflow_app;
 GRANT SELECT,INSERT,UPDATE ON notification_outbox,notification_cooldowns TO taskflow_app;
 GRANT SELECT,INSERT,UPDATE ON recurrence_rules TO taskflow_app;
 GRANT SELECT,INSERT ON recurrence_occurrences TO taskflow_app;
 GRANT SELECT,INSERT,UPDATE,DELETE ON projects,tasks,project_memberships,project_invitations TO taskflow_app;`)
	if err != nil {
		return errors.New("runtime role provisioning failed")
	}
	if err = tx.Commit(ctx); err != nil {
		return errors.New("runtime role provisioning commit failed")
	}
	return nil
}
func main() {
	if err := setup(); err != nil {
		log.Fatal("Database setup failed; inspect migration state using the administrative connection")
	}
	log.Print("Migrations and runtime grants complete")
}
