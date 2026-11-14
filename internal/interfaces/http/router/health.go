package interfaceshttprouter

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pluto-han/taskflow/internal/httpapi"
)

// MountHealth uses a bounded, inexpensive dependency ping. The legacy endpoint
// remains a liveness alias; readiness errors never expose connection details.
func MountHealth(router *gin.Engine, ping func(context.Context) error) {
	live := func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	}
	router.GET("/health", live)
	router.GET("/health/live", live)
	router.GET("/health/ready", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		ctx, cancel := context.WithTimeout(c.Request.Context(), time.Second)
		defer cancel()
		if ping == nil || ping(ctx) != nil {
			httpapi.WriteError(c, http.StatusServiceUnavailable, "NOT_READY", "Service is not ready")
			return
		}

		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
}
