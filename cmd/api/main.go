package main

import (
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pluto-han/taskflow/internal/task"
)

func main() {
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

	example2 := &task.Task{
		ID:          "example-2",
		Title:       "Learn Go",
		Description: "Build TaskFlow backend",
		Priority:    3,
		Completed:   false,
		CreatedAt:   time.Now().UTC(),
	}

	if err := repo.Save(example); err != nil {
		log.Fatal(err)
	}

	if err := repo.Save(example2); err != nil {
		log.Fatal(err)
	}

	// Monitor
	router := gin.Default()

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
		})
	})

	router.GET("/tasks/:id", handler.GetByID)
	router.GET("/tasks", handler.List)
	router.POST("/tasks", handler.Create)
	router.PATCH("/tasks/:id", handler.Update)
	router.DELETE("/tasks/:id", handler.Delete)

	log.Println("TaskFlow API starting on :8080")

	if err := router.Run(":8082"); err != nil {
		log.Fatal(err)
	}
}
