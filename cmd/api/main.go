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
	applicationaccount "github.com/pluto-han/taskflow/internal/application/account"
	applicationintegration "github.com/pluto-han/taskflow/internal/application/integration"
	applicationmail "github.com/pluto-han/taskflow/internal/application/mail"
	applicationnotification "github.com/pluto-han/taskflow/internal/application/notification"
	applicationproject "github.com/pluto-han/taskflow/internal/application/project"
	applicationrecurrence "github.com/pluto-han/taskflow/internal/application/recurrence"
	applicationsession "github.com/pluto-han/taskflow/internal/application/session"
	applicationtask "github.com/pluto-han/taskflow/internal/application/task"
	infracache "github.com/pluto-han/taskflow/internal/infra/cache"
	infraconfig "github.com/pluto-han/taskflow/internal/infra/config"
	infradatabase "github.com/pluto-han/taskflow/internal/infra/database"
	infradiscord "github.com/pluto-han/taskflow/internal/infra/discord"
	inframailsender "github.com/pluto-han/taskflow/internal/infra/mail"
	inframetrics "github.com/pluto-han/taskflow/internal/infra/metrics"
	infrapassword "github.com/pluto-han/taskflow/internal/infra/password"
	infraaccount "github.com/pluto-han/taskflow/internal/infra/persistence/account"
	infraintegration "github.com/pluto-han/taskflow/internal/infra/persistence/integration"
	inframail "github.com/pluto-han/taskflow/internal/infra/persistence/mail"
	infranotification "github.com/pluto-han/taskflow/internal/infra/persistence/notification"
	infraproject "github.com/pluto-han/taskflow/internal/infra/persistence/project"
	infrarecurrence "github.com/pluto-han/taskflow/internal/infra/persistence/recurrence"
	infrasession "github.com/pluto-han/taskflow/internal/infra/persistence/session"
	infratask "github.com/pluto-han/taskflow/internal/infra/persistence/task"
	interfaceshttpdiscord "github.com/pluto-han/taskflow/internal/interfaces/http/discord"
	interfaceshttpmiddleware "github.com/pluto-han/taskflow/internal/interfaces/http/middleware"
	interfaceshttprouter "github.com/pluto-han/taskflow/internal/interfaces/http/router"
	interfacesschedulermail "github.com/pluto-han/taskflow/internal/interfaces/scheduler/mail"
	interfacesschedulernotification "github.com/pluto-han/taskflow/internal/interfaces/scheduler/notification"
	interfacesschedulerrecurrence "github.com/pluto-han/taskflow/internal/interfaces/scheduler/recurrence"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		if healthy() {
			return
		}

		os.Exit(1)
	}
	cfg := infraconfig.Load()
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

	mailOptions, err := cfg.Mail()
	if err != nil {
		log.Fatal(err)
	}

	var mailSender applicationmail.Sender
	if mailOptions != nil {
		if mailOptions.Driver == "memory" {
			mailSender = &inframailsender.MemorySender{}
		} else {
			mailSender, err = inframailsender.NewResendSender(mailOptions.Key, mailOptions.From, nil)
			if err != nil {
				log.Fatal(err)
			}
		}
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
		if err := infradatabase.CheckRuntimeRole(dbCtx, pool); err != nil {
			dbCancel()
			pool.Close()
			log.Fatal(err)
		}
	}
	dbCancel()
	defer pool.Close()

	repo := infratask.New(pool)
	projectRepo := infraproject.New(pool)
	service := applicationtask.New(repo, projectRepo)

	userRepo := infraaccount.New(pool)
	sessionService, err := applicationsession.New(context.Background(), userRepo, infrasession.New(pool), infrapassword.PasswordHasher{})
	if err != nil {
		log.Fatal("Could not initialize password verification")
	}
	cacheClient, err := infracache.New(cacheOptions)
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
	metrics := inframetrics.New(pool, pool.Ping)
	metrics.SetCacheEnabled(cacheClient != nil)
	service.WithTodayCacheObserver(metrics)
	metrics.SetWorkers(schedulerEnabled, senderEnabled)
	mailService, err := applicationaccount.NewCapabilities(infraaccount.NewCapabilities(pool), mailSender != nil, security.PublicOrigin, infrapassword.PasswordHasher{})
	if err != nil {
		log.Fatal(err)
	}
	mailWorker := applicationmail.New(inframail.New(pool), mailSender).WithObserver(metrics)
	bindings := applicationintegration.New(infraintegration.New(pool), len(discordKey) > 0)
	router := interfaceshttprouter.New(interfaceshttprouter.Dependencies{
		Mail:          mailService,
		Notifications: applicationnotification.New(infranotification.New(pool, "default"), nil),
		Discord:       interfaceshttpdiscord.New(discordKey, discordApp, bindings, infradiscord.Verify),
		Integrations:  bindings,
		Recurrences:   applicationrecurrence.NewWithScheduler(infrarecurrence.New(pool), schedulerEnabled),
		Security: interfaceshttpmiddleware.SecurityOptions{
			Production:     security.Production,
			PublicOrigin:   security.PublicOrigin,
			TrustedProxies: security.TrustedProxies,
		},
		Metrics:  metrics,
		Users:    applicationaccount.New(userRepo, infrapassword.PasswordHasher{}),
		Sessions: sessionService,
		Tasks:    service,
		Projects: applicationproject.New(projectRepo, projectRepo),
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

	mailDone := make(chan struct{})
	if mailService.Enabled() {
		go func() { defer close(mailDone); interfacesschedulermail.New(mailWorker, metrics).Run(shutdownCtx) }()
	} else {
		close(mailDone)
	}
	// Stop and join mail before its shared pool closes, including early returns.
	defer func() { stop(); <-mailDone }()

	workerDone := make(chan struct{})
	if schedulerEnabled {
		workerConfig, err := infradatabase.WorkerPoolConfig(cfg.DatabaseURL)
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
		generator := applicationrecurrence.NewGenerator(infrarecurrence.New(workerPool), service.InvalidateToday)
		worker := interfacesschedulerrecurrence.NewScheduler(generator, slog.Default(), metrics)
		go func() {
			defer close(workerDone)
			worker.Run(shutdownCtx)
		}()
	} else {
		close(workerDone)
	}

	notificationDone := make(chan struct{})
	if senderEnabled {
		workerConfig, err := infradatabase.WorkerPoolConfig(cfg.DatabaseURL)
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

		sender, err := infradiscord.NewMessageSender(cfg.DiscordBotToken, nil)
		if err != nil {
			log.Print("Invalid Discord bot token")
			workerPool.Close()
			stop()
			<-workerDone

			return
		}

		defer workerPool.Close()
		worker := applicationnotification.New(infranotification.New(workerPool, sender.CredentialFingerprint()), sender).WithObserver(metrics)
		go func() {
			defer close(notificationDone)
			interfacesschedulernotification.New(worker, slog.Default(), metrics).Run(shutdownCtx)
		}()
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
