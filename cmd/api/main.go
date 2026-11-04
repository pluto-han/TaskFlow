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
	"github.com/pluto-han/taskflow/internal/config"
	"github.com/pluto-han/taskflow/internal/project"
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
	if err := cfg.ReadSecrets(); err != nil {
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
	metrics := telemetry.New(pool, pool.Ping)
	router := server.NewRouter(server.Dependencies{
		Security: security,
		Metrics:  metrics,
		Users:    user.NewService(userRepo, auth.PasswordHasher{}),
		Sessions: sessionService,
		Tasks:    service,
		Projects: project.NewService(projectRepo),
		Ping:     pool.Ping,
		Logger:   slog.New(slog.NewJSONHandler(os.Stdout, nil)),
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
	log.Println("TaskFlow stopped")
}
