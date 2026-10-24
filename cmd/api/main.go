package main

import (
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pluto-han/taskflow/internal/task"
)

func main() {
	router := gin.Default()

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
		})
	})

	router.GET("tasks/example", func(c *gin.Context) {
		example := task.Task{
			ID:          "example-1",
			Title:       "Learn Go",
			Description: "Build TaskFlow backend",
			Completed:   false,
			Priority:    3,
			CreatedAt:   time.Now().UTC(),
		}

		c.JSON(http.StatusOK, example)
	})

	log.Println("TaskFlow API starting on :8080")

	if err := router.Run(":8082"); err != nil {
		log.Fatal(err)
	}
}
