package config

import (
	"os"
	"time"
)

type Config struct {
	HTTPAddr        string
	ShutdownTimeout time.Duration
}

func Load() Config {
	addr := os.Getenv("TASKFLOW_HTTP_ADDR")
	if addr == "" {
		addr = ":9090"
	}

	return Config{
		HTTPAddr:        addr,
		ShutdownTimeout: 10 * time.Second,
	}
}
