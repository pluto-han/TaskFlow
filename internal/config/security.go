package config

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pluto-han/taskflow/internal/httpapi"
)

// Security validates configuration without including credentials in errors.
func (c Config) Security() (httpapi.SecurityOptions, error) {
	options := httpapi.SecurityOptions{Production: c.Environment == "production", PublicOrigin: c.PublicOrigin}
	if c.Environment != "development" && c.Environment != "production" {
		return options, errors.New("TASKFLOW_ENV must be development or production")
	}

	if !options.Production && c.HTTPAddr != "" {
		host, _, err := net.SplitHostPort(c.HTTPAddr)
		ip := net.ParseIP(host)
		if err != nil || ip == nil || !ip.IsLoopback() {
			return options, errors.New("development HTTP address must be loopback; configure production mode for external listeners")
		}
	}
	if c.MetricsAddr != "" {
		host, port, err := net.SplitHostPort(c.MetricsAddr)
		ip := net.ParseIP(host)
		number, portErr := strconv.Atoi(port)
		if err != nil || portErr != nil || number < 1 || number > 65535 || ip == nil || (!ip.IsLoopback() && !ip.IsPrivate()) {
			return options, errors.New("metrics requires an explicit loopback/private IP and port; never publish this port")
		}
	}
	if c.PublicOrigin != "" {
		u, err := url.Parse(c.PublicOrigin)
		if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || (u.Scheme != "http" && u.Scheme != "https") {
			return options, errors.New("TASKFLOW_PUBLIC_ORIGIN must be an exact origin without a path")
		}
		if options.Production && u.Scheme != "https" {
			return options, errors.New("production origin must use HTTPS")
		}
	}
	if options.Production && c.PublicOrigin == "" {
		return options, errors.New("production requires TASKFLOW_PUBLIC_ORIGIN")
	}
	for _, raw := range strings.Split(c.TrustedProxies, ",") {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		_, network, err := net.ParseCIDR(strings.TrimSpace(raw))
		if err != nil {
			return options, errors.New("invalid trusted proxy CIDR")
		}
		ones, _ := network.Mask.Size()
		if ones == 0 {
			return options, errors.New("trusting all proxy addresses is forbidden")
		}
		options.TrustedProxies = append(options.TrustedProxies, network)
	}
	if options.Production {
		// This binary serves HTTP behind a TLS terminator, never direct public HTTP.
		if len(options.TrustedProxies) == 0 {
			return options, errors.New("production requires explicit trusted TLS proxy CIDRs")
		}
		u, err := url.Parse(c.DatabaseURL)
		if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Query().Get("sslmode") != "verify-full" {
			return options, errors.New("production DATABASE_URL requires sslmode=verify-full")
		}
	}
	return options, nil
}

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
