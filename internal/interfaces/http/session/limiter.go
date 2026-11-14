package interfaceshttpsession

import (
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pluto-han/taskflow/internal/httpapi"
)

// LoginLimiter is a bounded, process-wide fixed-window learning baseline.
// It intentionally does not trust proxy headers or allocate per-IP map entries.
type LoginLimiter struct {
	mu    sync.Mutex
	start time.Time
	used  int
	now   func() time.Time
}

func NewLoginLimiter() *LoginLimiter { return NewLoginLimiterWithClock(time.Now) }

func NewLoginLimiterWithClock(now func() time.Time) *LoginLimiter { return &LoginLimiter{now: now} }

func (l *LoginLimiter) allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if l.start.IsZero() || now.Sub(l.start) >= time.Minute {
		l.start = now
		l.used = 0
	}
	if l.used >= 30 {
		return false
	}

	l.used++

	return true
}

func (l *LoginLimiter) Middleware(c *gin.Context) {
	if !l.allow() {
		c.Header("Retry-After", "60")
		httpapi.WriteError(c, 429, "LOGIN_RATE_LIMITED", "Please retry later")
		c.Abort()
		return
	}

	c.Next()
}
