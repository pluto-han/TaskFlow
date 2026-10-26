package config

import (
	"os"
	"time"
)

type Config struct {
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

	return Config{
		HTTPAddr:        addr,
		DatabaseURL:     databaseURL,
		ShutdownTimeout: 10 * time.Second,
	}
}
