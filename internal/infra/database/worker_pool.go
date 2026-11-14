package infradatabase

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

// WorkerPoolConfig isolates background work from HTTP pool acquisitions.
// Each API process uses at most one additional PostgreSQL connection.
func WorkerPoolConfig(url string) (*pgxpool.Config, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}

	cfg.MaxConns = 1
	cfg.MinConns = 0
	cfg.MinIdleConns = 0
	cfg.ConnConfig.RuntimeParams["application_name"] = "taskflow-recurrence"
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "4000"
	cfg.ConnConfig.RuntimeParams["lock_timeout"] = "1000"

	return cfg, nil
}
