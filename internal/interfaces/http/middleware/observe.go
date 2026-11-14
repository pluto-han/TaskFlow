package interfaceshttpmiddleware

import (
	"context"
	"crypto/rand"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pluto-han/taskflow/internal/httpapi"
)

type requestIDKey struct{}

// RequestID is available to downstream operations without passing Gin's context.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)

	return id
}

// Observe deliberately excludes raw URLs, headers, bodies, client IDs, errors
// and panic values. Even a panic containing credentials must not reach the log.
type RequestObserver interface {
	Observe(method, route string, status int, elapsed time.Duration)
}

func Observe(logger *slog.Logger, observers ...RequestObserver) gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		id := rand.Text()
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), requestIDKey{}, id))
		c.Header("X-Request-ID", id)
		panicked := false
		defer func() {
			if recover() != nil {
				panicked = true
				c.Abort()
				// Once headers are sent, appending JSON would corrupt the response.
				if !c.Writer.Written() {
					httpapi.InternalError(c)
				}
			}
			route := c.FullPath()
			if route == "" {
				route = "<unmatched>"
			}
			method := c.Request.Method
			switch method {
			case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "CONNECT", "TRACE":
			default:
				method = "OTHER"
			}
			for _, observer := range observers {
				if observer != nil {
					observer.Observe(method, route, c.Writer.Status(), time.Since(started))
				}
			}
			level := slog.LevelInfo
			if panicked || c.Writer.Status() >= 500 {
				level = slog.LevelError
			}
			logger.Log(c.Request.Context(), level, "http_request",
				"request_id", id, "method", method, "route", route,
				"status", c.Writer.Status(), "duration_ms", time.Since(started).Milliseconds(), "panicked", panicked)
		}()
		c.Next()
	}
}
