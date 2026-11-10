package config

import "testing"

func TestProductionSecurityConfiguration(t *testing.T) {
	safe := Config{Environment: "production", PublicOrigin: "https://tasks.example.com", TrustedProxies: "127.0.0.1/32", DatabaseURL: "postgres://app:secret@db/taskflow?sslmode=verify-full"}
	if _, err := safe.Security(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*Config)
	}{
		{"invalid env", func(c *Config) { c.Environment = "prod" }},
		{"no origin", func(c *Config) { c.PublicOrigin = "" }},
		{"http", func(c *Config) { c.PublicOrigin = "http://tasks.example.com" }},
		{"origin path", func(c *Config) { c.PublicOrigin += "/path" }},
		{"no proxies", func(c *Config) { c.TrustedProxies = "" }},
		{"trust all", func(c *Config) { c.TrustedProxies = "0.0.0.0/0" }},
		{"no DB verification", func(c *Config) { c.DatabaseURL = "postgres://app:secret@db/taskflow?sslmode=disable" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := safe
			tc.change(&c)
			if _, err := c.Security(); err == nil {
				t.Fatal("unsafe production accepted")
			}
		})
	}
}

func TestDevelopmentCannotBindPublicly(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:9090", ":9090", "[::]:9090"} {
		if _, err := (Config{Environment: "development", HTTPAddr: addr}).Security(); err == nil {
			t.Fatal("public development listener accepted")
		}
	}
	if _, err := (Config{Environment: "development", HTTPAddr: "127.0.0.1:9090"}).Security(); err != nil {
		t.Fatal(err)
	}
}

func TestMetricsAddressBoundary(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:9091", ":9091", "8.8.8.8:9091", "localhost:9091", "127.0.0.1:0", "127.0.0.1:70000"} {
		c := Config{Environment: "development", MetricsAddr: addr}
		if _, err := c.Security(); err == nil {
			t.Errorf("accepted unsafe metrics address %s", addr)
		}
	}
	for _, addr := range []string{"", "127.0.0.1:9091", "172.30.23.3:9091"} {
		c := Config{Environment: "development", MetricsAddr: addr}
		if _, err := c.Security(); err != nil {
			t.Errorf("rejected %s: %v", addr, err)
		}
	}
}

func TestBrowserDevelopmentOrigin(t *testing.T) {
	defaults, err := (Config{Environment: "development", HTTPAddr: "127.0.0.1:9090"}).Security()
	if err != nil || defaults.PublicOrigin != "http://127.0.0.1:5173" {
		t.Fatal("missing safe browser origin default")
	}
	for _, origin := range []string{"http://localhost:5173", "http://[::1]:5173", "http://127.0.0.2:5174", "https://preview.example.com"} {
		options, err := (Config{Environment: "development", PublicOrigin: origin}).Security()
		if err != nil || options.PublicOrigin != origin {
			t.Errorf("rejected development origin: %s", origin)
		}
	}
	for _, origin := range []string{"http://example.com", "http://192.168.1.2:5173", "http://0.0.0.0:5173", "http://localhost.evil.example:5173"} {
		if _, err := (Config{Environment: "development", PublicOrigin: origin}).Security(); err == nil {
			t.Errorf("accepted external HTTP origin: %s", origin)
		}
	}
}
