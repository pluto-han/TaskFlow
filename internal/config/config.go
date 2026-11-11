package config

import (
	"os"
	"time"
)

type Config struct {
	MailEnabled          string
	MailDriver           string
	MailFrom             string
	MailSecretFile       string
	CacheEnabled         string
	RedisAddr            string
	RedisUsername        string
	RedisPasswordFile    string
	DiscordSender        string
	DiscordBotToken      string
	DiscordApplicationID string
	DiscordPublicKey     string
	RecurrenceScheduler  string
	Environment          string
	PublicOrigin         string
	TrustedProxies       string
	HTTPAddr             string
	MetricsAddr          string
	DatabaseURL          string
	ShutdownTimeout      time.Duration
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
		MailEnabled:          os.Getenv("TASKFLOW_MAIL_ENABLED"),
		MailDriver:           os.Getenv("TASKFLOW_MAIL_DRIVER"),
		MailFrom:             os.Getenv("TASKFLOW_MAIL_FROM"),
		MailSecretFile:       os.Getenv("TASKFLOW_MAIL_SECRET_FILE"),
		CacheEnabled:         os.Getenv("TASKFLOW_CACHE_ENABLED"),
		RedisAddr:            os.Getenv("REDIS_ADDR"),
		RedisUsername:        os.Getenv("REDIS_USERNAME"),
		RedisPasswordFile:    os.Getenv("REDIS_PASSWORD_FILE"),
		DiscordSender:        os.Getenv("TASKFLOW_DISCORD_SENDER_ENABLED"),
		DiscordApplicationID: os.Getenv("DISCORD_APPLICATION_ID"),
		DiscordPublicKey:     os.Getenv("DISCORD_PUBLIC_KEY"),
		RecurrenceScheduler:  os.Getenv("TASKFLOW_RECURRENCE_ENABLED"),
		Environment:          environment, PublicOrigin: os.Getenv("TASKFLOW_PUBLIC_ORIGIN"), TrustedProxies: os.Getenv("TASKFLOW_TRUSTED_PROXIES"),
		HTTPAddr:        addr,
		MetricsAddr:     os.Getenv("TASKFLOW_METRICS_ADDR"),
		DatabaseURL:     databaseURL,
		ShutdownTimeout: 10 * time.Second,
	}
}
