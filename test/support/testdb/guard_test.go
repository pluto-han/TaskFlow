package testdb

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRejectUnsafeConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, url, marker string
		ok                bool
	}{
		{"runner", "postgres://taskflow:temporary@127.0.0.1:6543/taskflow_lesson21_test?sslmode=disable", strings.Repeat("a", 32), true},
		{"missing lease", "postgres://taskflow@127.0.0.1:6543/test?sslmode=disable", "", false},
		{"default database port", "postgres://taskflow@127.0.0.1:5432/test?sslmode=disable", strings.Repeat("a", 32), false},
		{"remote", "postgres://taskflow@example.com:6543/test?sslmode=disable", strings.Repeat("a", 32), false},
		{"fallback", "postgres://taskflow@127.0.0.1:6543,example.com:6543/test?sslmode=disable", strings.Repeat("a", 32), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := pgxpool.ParseConfig(tc.url)
			if err != nil {
				t.Fatal(err)
			}
			if (validate(cfg, tc.marker) == nil) != tc.ok {
				t.Fatal("unexpected guard outcome")
			}
		})
	}
}
