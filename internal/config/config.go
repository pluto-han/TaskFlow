package config

import (
	"os"
	"time"
)

type Config struct {
	Environment     string
	PublicOrigin    string
	TrustedProxies  string
	HTTPAddr        string
	DatabaseURL     string
	ShutdownTimeout time.Duration
}

func Load() Config {
	addr := os.Getenv("TASKFLOW_HTTP_ADDR")
	if addr == "" {
		addr = "127.0.0.1:9090"
	}

	databaseURL := os.Getenv("DATABASE_URL")

	environment := os.Getenv("TASKFLOW_ENV")
	if environment == "" {
		environment = "development"
	}
	return Config{
		Environment: environment, PublicOrigin: os.Getenv("TASKFLOW_PUBLIC_ORIGIN"), TrustedProxies: os.Getenv("TASKFLOW_TRUSTED_PROXIES"),
		HTTPAddr:        addr,
		DatabaseURL:     databaseURL,
		ShutdownTimeout: 10 * time.Second,
	}
}
