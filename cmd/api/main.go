package main

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pluto-han/taskflow/internal/auth"
	"github.com/pluto-han/taskflow/internal/cache"
	"github.com/pluto-han/taskflow/internal/config"
	"github.com/pluto-han/taskflow/internal/discord"
	"github.com/pluto-han/taskflow/internal/integration"
	"github.com/pluto-han/taskflow/internal/notification"
	"github.com/pluto-han/taskflow/internal/project"
	"github.com/pluto-han/taskflow/internal/recurrence"
	"github.com/pluto-han/taskflow/internal/server"
	"github.com/pluto-han/taskflow/internal/session"
	"github.com/pluto-han/taskflow/internal/task"
	"github.com/pluto-han/taskflow/internal/telemetry"
	"github.com/pluto-han/taskflow/internal/user"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		if healthy() {
			return
		}
		os.Exit(1)
	}
	cfg := config.Load()
	cacheOptions, err := cfg.Cache()
	if err != nil {
		log.Fatal(err)
	}
	if err := cfg.ReadSecrets(); err != nil {
		log.Fatal(err)
	}
	senderEnabled, err := cfg.NotificationToken()
	if err != nil {
		log.Fatal(err)
	}
	discordKey, discordApp, err := cfg.Discord()
	if err != nil {
		log.Fatal(err)
	}
	schedulerEnabled, err := cfg.RecurrenceEnabled()
	if err != nil {
		log.Fatal(err)
	}
	security, err := cfg.Security()
	if err != nil {
		log.Fatal(err)
	}

	if cfg.DatabaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	dbCtx, dbCancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)

	pool, err := pgxpool.New(dbCtx, cfg.DatabaseURL)
	if err != nil {
		dbCancel()
		log.Fatal("Could not initialize database pool")
	}

	if err := pool.Ping(dbCtx); err != nil {
		dbCancel()
		pool.Close()
		log.Fatal("Database connection failed")
	}

	if security.Production {
		if err := config.CheckRuntimeRole(dbCtx, pool); err != nil {
			dbCancel()
			pool.Close()
			log.Fatal(err)
		}
	}
	dbCancel()
	defer pool.Close()

	repo := task.NewPostgresRepository(pool)
	projectRepo := project.NewPostgresRepository(pool)
	service := task.NewService(repo, projectRepo)

	userRepo := user.NewPostgresRepository(pool)
	sessionService, err := session.NewService(context.Background(), userRepo, session.NewPostgresRepository(pool), auth.PasswordHasher{})
	if err != nil {
		log.Fatal("Could not initialize password verification")
	}
	cacheClient, err := cache.New(cacheOptions)
	if err != nil {
		log.Print("Could not configure optional cache")
		return
	}
	if cacheClient != nil {
		defer cacheClient.Close()
		service.WithTodayCache(cacheClient)
		if err := cacheClient.Ping(context.Background()); err != nil {
			log.Print("Optional cache unavailable; business queries still use PostgreSQL")
		}
	}
	metrics := telemetry.New(pool, pool.Ping)
	metrics.SetCacheEnabled(cacheClient != nil)
	service.WithTodayCacheObserver(metrics)
	metrics.SetWorkers(schedulerEnabled, senderEnabled)
	bindings := integration.NewService(pool, len(discordKey) > 0)
	router := server.NewRouter(server.Dependencies{
		Notifications: notification.NewService(pool, nil),
		Discord:       discord.NewHandler(discordKey, discordApp, bindings),
		Integrations:  bindings,
		Recurrences:   recurrence.NewServiceWithScheduler(recurrence.NewPostgresRepository(pool), schedulerEnabled),
		Security:      security,
		Metrics:       metrics,
		Users:         user.NewService(userRepo, auth.PasswordHasher{}),
		Sessions:      sessionService,
		Tasks:         service,
		Projects:      project.NewService(projectRepo),
		Ping:          pool.Ping,
		Logger:        slog.New(slog.NewJSONHandler(os.Stdout, nil)),
	})

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 * 1024,
	}

	shutdownCtx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	serverErrors := make(chan error, 2)
	var metricsServer *http.Server
	if cfg.MetricsAddr != "" {
		listener, err := net.Listen("tcp", cfg.MetricsAddr)
		if err != nil {
			log.Print("Metrics listener failed to bind")
			return
		}
		metricsServer = &http.Server{Handler: metrics.Handler(), ReadHeaderTimeout: 3 * time.Second,
			ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192}
		go func() {
			if err := metricsServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
				serverErrors <- err
			}
		}()
	}

	workerDone := make(chan struct{})
	if schedulerEnabled {
		workerConfig, err := recurrence.WorkerPoolConfig(cfg.DatabaseURL)
		if err != nil {
			log.Print("Could not configure recurrence pool")
			if metricsServer != nil {
				_ = metricsServer.Close()
			}
			return
		}
		workerPool, err := pgxpool.NewWithConfig(shutdownCtx, workerConfig)
		if err != nil {
			log.Print("Could not initialize recurrence pool")
			if metricsServer != nil {
				_ = metricsServer.Close()
			}
			return
		}
		defer workerPool.Close()
		generator := recurrence.NewPostgresRepository(workerPool)
		if cacheClient != nil {
			generator.WithTodayInvalidation(service.InvalidateToday)
		}
		worker := recurrence.NewScheduler(generator, slog.Default(), metrics)
		go func() { defer close(workerDone); worker.Run(shutdownCtx) }()
	} else {
		close(workerDone)
	}

	notificationDone := make(chan struct{})
	if senderEnabled {
		workerConfig, err := recurrence.WorkerPoolConfig(cfg.DatabaseURL)
		if err != nil {
			log.Print("Could not configure notification pool")
			stop()
			<-workerDone
			return
		}
		workerConfig.ConnConfig.RuntimeParams["application_name"] = "taskflow-notification"
		workerPool, err := pgxpool.NewWithConfig(shutdownCtx, workerConfig)
		if err != nil {
			log.Print("Could not initialize notification pool")
			stop()
			<-workerDone
			return
		}
		sender, err := discord.NewMessageSender(cfg.DiscordBotToken, nil)
		if err != nil {
			log.Print("Invalid Discord bot token")
			workerPool.Close()
			stop()
			<-workerDone
			return
		}
		defer workerPool.Close()
		worker := notification.NewService(workerPool, sender).WithObserver(metrics)
		go func() { defer close(notificationDone); worker.Run(shutdownCtx) }()
	} else {
		close(notificationDone)
	}

	go func() {
		log.Printf("TaskFlow listening on %s", cfg.HTTPAddr)

		err := server.ListenAndServe()

		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
	}()

	select {
	case <-shutdownCtx.Done():
		log.Println("Shutdown signal received")

	case err := <-serverErrors:
		log.Printf("HTTP server failed: %v", err)
	}

	stop() // Also cancel the worker when the HTTP listener fails.
	ctx, cancel := context.WithTimeout(
		context.Background(),
		cfg.ShutdownTimeout,
	)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Graceful shutdown failed: %v", err)

		if closeErr := server.Close(); closeErr != nil {
			log.Printf("Force close failed: %v", closeErr)
		}
	}

	if metricsServer != nil {
		if err := metricsServer.Shutdown(ctx); err != nil {
			_ = metricsServer.Close()
		}
	}
	<-notificationDone
	<-workerDone // Batch context was canceled; wait before closing its pool.
	log.Println("TaskFlow stopped")
}
