package interfaceshttpmiddleware

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pluto-han/taskflow/internal/httpapi"
)

type SecurityOptions struct {
	Production     bool
	PublicOrigin   string
	TrustedProxies []*net.IPNet
}

// windowLimiter is bounded, has no cleanup goroutine, and never trusts IP headers.
// A global limit remains effective even when peer addresses vary.
type windowLimiter struct {
	mu              sync.Mutex
	start           time.Time
	total           int
	peers           map[string]int
	global, perPeer int
	now             func() time.Time
}

func newWindowLimiter(global, perPeer int) *windowLimiter {
	return &windowLimiter{global: global, perPeer: perPeer, now: time.Now}
}

func (l *windowLimiter) allow(peer string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if l.start.IsZero() || now.Sub(l.start) >= time.Minute {
		l.start = now
		l.total = 0
		l.peers = make(map[string]int)
	}
	if l.total >= l.global || l.peers[peer] >= l.perPeer {
		return false
	}

	// global also bounds the number of distinct peer keys allocated per window.
	l.total++
	l.peers[peer]++

	return true
}

func peerIP(remote string) string {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		return "unknown"
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return "unknown"
	}

	return ip.String()
}

func trustedTLS(c *gin.Context, options SecurityOptions) bool {
	if c.Request.TLS != nil {
		return true
	}

	ip := net.ParseIP(peerIP(c.Request.RemoteAddr))
	if ip == nil || len(c.Request.Header.Values("X-Forwarded-Proto")) != 1 || c.GetHeader("X-Forwarded-Proto") != "https" {
		return false
	}

	for _, network := range options.TrustedProxies {
		if network.Contains(ip) {
			return true
		}
	}

	return false
}

// Security protects the production router, including error and unmatched routes.
// Browser and Bearer clients share budgets. Cookie/CSRF checks live with session
// authentication; no cross-origin credential grants are provided here.
func Security(options SecurityOptions) gin.HandlerFunc {
	return SecurityWithClock(options, time.Now)
}

func SecurityWithClock(options SecurityOptions, now func() time.Time) gin.HandlerFunc {
	general := newWindowLimiter(1200, 300)
	registration := newWindowLimiter(30, 10)
	login := newWindowLimiter(60, 20)
	email := newWindowLimiter(60, 10)
	passwordChange := newWindowLimiter(30, 5)
	for _, limiter := range []*windowLimiter{general, registration, login, email, passwordChange} {
		limiter.now = now
	}

	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		c.Header("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
		if options.Production {
			if !trustedTLS(c, options) {
				httpapi.WriteError(c, 403, "HTTPS_REQUIRED", "HTTPS is required")
				c.Abort()
				return
			}

			c.Header("Strict-Transport-Security", "max-age=31536000")
		}
		// Only the signed Discord POST has no browser Origin contract.
		signedCallback := c.Request.Method == http.MethodPost && c.FullPath() == "/discord/interactions"
		if origin := c.GetHeader("Origin"); !signedCallback && options.PublicOrigin != "" && origin != "" && origin != options.PublicOrigin {
			httpapi.WriteError(c, 403, "ORIGIN_NOT_ALLOWED", "Origin is not permitted")
			c.Abort()
			return
		}

		// Health probes are cheap and must remain available when a client is throttled.
		path := c.FullPath()
		if path != "/health" && path != "/health/live" && path != "/health/ready" {
			peer := peerIP(c.Request.RemoteAddr)
			allowed := general.allow(peer)
			if path == "/auth/register" {
				allowed = allowed && registration.allow(peer)
			}
			if path == "/auth/login" || path == "/auth/browser/login" {
				allowed = allowed && login.allow(peer)
			}
			if path == "/auth/password/change" {
				allowed = allowed && passwordChange.allow(peer)
			}
			if c.Request.Method == http.MethodPost && (path == "/auth/email/verification" || path == "/auth/email/verification/confirm" || path == "/auth/password/reset/request" || path == "/auth/password/reset/confirm") {
				allowed = allowed && email.allow(peer)
			}
			if !allowed {
				c.Header("Retry-After", "60")
				httpapi.WriteError(c, 429, "RATE_LIMITED", "Please retry later")
				c.Abort()
				return
			}
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		// Bound decoded bodies, including unknown-length/chunked input.
		limit := int64(64 * 1024)
		if path == "/auth/login" || path == "/auth/browser/login" || path == "/auth/register" || path == "/auth/password/change" || path == "/auth/email/verification" || path == "/auth/email/verification/confirm" || path == "/auth/password/reset/request" || path == "/auth/password/reset/confirm" {
			limit = 4096
		}
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
			// Declared oversize is rejected before reading.
			if c.Request.ContentLength > limit {
				httpapi.WriteError(c, 413, "REQUEST_TOO_LARGE", "Request body too large")
				c.Abort()
				return
			}

			raw, err := io.ReadAll(c.Request.Body)
			if err != nil {
				httpapi.WriteError(c, 413, "REQUEST_TOO_LARGE", "Request body too large or unreadable")
				c.Abort()
				return
			}

			c.Request.Body = io.NopCloser(bytes.NewReader(raw))
		}
		c.Next()
	}
}
