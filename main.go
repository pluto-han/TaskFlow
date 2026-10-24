package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type Task struct {
	ID    uint   `json:"id" gorm:"primaryKey"`
	Title string `json:"title" binding:"required"`
	Done  bool   `json:"done"`
}

func main() {
	db, err := gorm.Open(sqlite.Open("tasks.db"), &gorm.Config{})
	if err != nil {
		panic(err)
	}

	if err := db.AutoMigrate(&Task{}); err != nil {
		panic(err)
	}

	r := gin.Default()

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	r.GET("/tasks", func(c *gin.Context) {
		var tasks []Task
		if err := db.Find(&tasks).Error; err != nil {
			c.JSON(500, gin.H{"error": "database error"})
			return
		}
		c.JSON(200, tasks)
	})

	r.POST("/tasks", func(c *gin.Context) {
		var task Task
		if err := c.ShouldBindJSON(&task); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		if err := db.Create(&task).Error; err != nil {
			c.JSON(500, gin.H{"error": "database error"})
			return
		}
		c.JSON(201, task)
	})

	if err := r.Run(":8080"); err != nil {
		panic(err)
	}
}
