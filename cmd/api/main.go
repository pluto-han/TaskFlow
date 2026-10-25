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

	"github.com/pluto-han/taskflow/internal/config"
	"github.com/pluto-han/taskflow/internal/task"
)

func main() {
	cfg := config.Load()

	repo := task.NewMemoryRepository()
	service := task.NewService(repo)
	handler := task.NewHandler(service)

	example := &task.Task{
		ID:          "example-1",
		Title:       "Learn Go",
		Description: "Build TaskFlow backend",
		Priority:    3,
		Completed:   false,
		CreatedAt:   time.Now().UTC(),
	}

	if err := repo.Save(example); err != nil {
		log.Fatal(err)
	}

	router := gin.Default()

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
		})
	})

	router.GET("/tasks", handler.List)
	router.GET("/tasks/:id", handler.GetByID)
	router.POST("/tasks", handler.Create)
	router.PATCH("/tasks/:id", handler.Update)
	router.DELETE("/tasks/:id", handler.Delete)

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
