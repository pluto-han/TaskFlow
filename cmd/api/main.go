package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pluto-han/taskflow/internal/auth"
	"github.com/pluto-han/taskflow/internal/config"
	"github.com/pluto-han/taskflow/internal/session"
	"github.com/pluto-han/taskflow/internal/task"
	"github.com/pluto-han/taskflow/internal/user"
)

func main() {
	cfg := config.Load()

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
		log.Fatal(err)
	}

	if err := pool.Ping(dbCtx); err != nil {
		dbCancel()
		pool.Close()
		log.Fatalf("database connection failed: %v", err)
	}

	dbCancel()
	defer pool.Close()

	repo := task.NewPostgresRepository(pool)
	service := task.NewService(repo)

	router := gin.Default()
	userRepo := user.NewPostgresRepository(pool)
	userHandler := user.NewHandler(user.NewService(userRepo, auth.PasswordHasher{}))
	sessionService, err := session.NewService(context.Background(), userRepo, session.NewPostgresRepository(pool), auth.PasswordHasher{})
	if err != nil {
		log.Fatal("Could not initialize password verification")
	}
	session.Mount(router, sessionService)
	router.POST("/auth/register", userHandler.Register)

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
		})
	})

	task.Mount(router, service, session.NewHandler(sessionService).RequireSession)

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	shutdownCtx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	serverErrors := make(chan error, 1)

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
		return
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

	log.Println("TaskFlow stopped")
}
