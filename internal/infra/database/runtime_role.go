package infradatabase

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

// CheckRuntimeRole blocks privileged/owner connections in production. The runtime
// must be a separate non-DDL role; migrations use a separate administrative role.
func CheckRuntimeRole(ctx context.Context, pool *pgxpool.Pool) error {
	var unsafe bool
	err := pool.QueryRow(ctx, `SELECT
 EXISTS(SELECT 1 FROM pg_roles r WHERE pg_has_role(current_user,r.oid,'MEMBER') AND (r.rolsuper OR r.rolcreatedb OR r.rolcreaterole OR r.rolbypassrls))
 OR has_schema_privilege(current_user,'public','CREATE')
 OR EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND pg_has_role(current_user,c.relowner,'MEMBER'))`).Scan(&unsafe)
	if err != nil {
		return errors.New("cannot validate runtime database privileges")
	}
	if unsafe {
		return errors.New("runtime database role is privileged or owns application objects; use a separate least-privilege role")
	}

	return nil
}
