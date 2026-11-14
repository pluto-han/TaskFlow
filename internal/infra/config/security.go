package infraconfig

import (
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// Security validates configuration without including credentials in errors.
func (c Config) Security() (SecurityOptions, error) {
	options := SecurityOptions{
		Production:   c.Environment == "production",
		PublicOrigin: c.PublicOrigin,
	}
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
		if !options.Production && u.Scheme == "http" {
			ip := net.ParseIP(u.Hostname())
			if u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
				return options, errors.New("development HTTP origin must be loopback")
			}
		}
	}
	if options.Production && c.PublicOrigin == "" {
		return options, errors.New("production requires TASKFLOW_PUBLIC_ORIGIN")
	}
	if !options.Production && options.PublicOrigin == "" {
		options.PublicOrigin = "http://127.0.0.1:5173"
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

type SecurityOptions struct {
	Production     bool
	PublicOrigin   string
	TrustedProxies []*net.IPNet
}
