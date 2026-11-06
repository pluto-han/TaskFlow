package httpapi

import (
	"crypto/tls"
	"io"
	"net"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestSecurityTLSAndOrigin(t *testing.T) {
	_, trusted, _ := net.ParseCIDR("127.0.0.1/32")
	for _, tc := range []struct {
		name, peer, proto, origin string
		tls                       bool
		status                    int
	}{
		{"forged external proxy", "192.0.2.1:1234", "https", "", false, 403},
		{"plain proxy", "127.0.0.1:1234", "http", "", false, 403},
		{"trusted proxy", "127.0.0.1:1234", "https", "https://tasks.example.com", false, 204},
		{"cross origin", "127.0.0.1:1234", "https", "https://evil.example", false, 403},
		{"null origin", "127.0.0.1:1234", "https", "null", false, 403},
		{"direct TLS", "192.0.2.1:1234", "", "", true, 204},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.Use(Security(SecurityOptions{Production: true, PublicOrigin: "https://tasks.example.com", TrustedProxies: []*net.IPNet{trusted}}))
			r.GET("/test", func(c *gin.Context) {
				if _, ok := c.Request.Context().Deadline(); !ok {
					t.Error("missing deadline")
				}
				c.Status(204)
			})
			req := httptest.NewRequest("GET", "/test", nil)
			req.RemoteAddr = tc.peer
			req.Header.Set("X-Forwarded-Proto", tc.proto)
			req.Header.Set("Origin", tc.origin)
			if tc.tls {
				req.TLS = &tls.ConnectionState{}
			}
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status %d", rec.Code)
			}
			if rec.Header().Get("Access-Control-Allow-Origin") != "" || rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("unsafe headers")
			}
			if tc.status == 204 && rec.Header().Get("Strict-Transport-Security") == "" {
				t.Fatal("missing HSTS")
			}
		})
	}
}
func TestSecurityBodyBoundsAndProxySpoofing(t *testing.T) {
	r := gin.New()
	r.Use(Security(SecurityOptions{}))
	r.POST("/auth/register", func(c *gin.Context) { c.Status(204) })
	r.POST("/tasks", func(c *gin.Context) { _, _ = io.Copy(io.Discard, c.Request.Body); c.Status(204) })
	r.GET("/health/live", func(c *gin.Context) { c.Status(204) })
	for _, path := range []string{"/auth/register", "/tasks"} {
		req := httptest.NewRequest("POST", path, strings.NewReader(strings.Repeat("x", 65537)))
		req.ContentLength = -1
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != 413 {
			t.Fatal("chunked oversize accepted")
		}
	}
	// Different spoofed addresses cannot buy extra registration attempts.
	for i := 0; i < 12; i++ {
		req := httptest.NewRequest("POST", "/auth/register", strings.NewReader("{}"))
		req.Header.Set("X-Forwarded-For", net.IPv4(192, 0, 2, byte(i)).String())
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if i >= 10 && (rec.Code != 429 || rec.Header().Get("Retry-After") != "60") {
			t.Fatal("spoofed IP bypassed limiter")
		}
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/health/live", nil))
	if rec.Code != 204 {
		t.Fatal("throttled health")
	}
}
func TestWindowLimiterConcurrentAndBounded(t *testing.T) {
	l := newWindowLimiter(50, 10)
	now := time.Now()
	l.now = func() time.Time { return now }
	results := make(chan bool, 100)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- l.allow("same-peer") }()
	}
	wg.Wait()
	close(results)
	allowed := 0
	for v := range results {
		if v {
			allowed++
		}
	}
	if allowed != 10 {
		t.Fatal("per-peer race")
	}
	for i := 0; i < 1000; i++ {
		l.allow(net.IPv4(10, 0, byte(i/256), byte(i%256)).String())
	}
	if l.total != 50 || len(l.peers) > 50 {
		t.Fatal("unbounded limiter")
	}
	now = now.Add(time.Minute)
	if !l.allow("same-peer") || len(l.peers) != 1 {
		t.Fatal("window did not reset")
	}
}

func TestDiscordOriginExceptionIsExact(t *testing.T) {
	r := gin.New()
	r.Use(Security(SecurityOptions{PublicOrigin: "https://tasks.example.com"}))
	for _, path := range []string{"/discord/interactions", "/discord/interactions/other", "/tasks"} {
		r.POST(path, func(c *gin.Context) { c.Status(204) })
	}
	r.GET("/discord/interactions", func(c *gin.Context) { c.Status(204) })
	for _, tc := range []struct {
		method, path string
		want         int
	}{
		{"POST", "/discord/interactions", 204},
		{"GET", "/discord/interactions", 403},
		{"POST", "/discord/interactions/other", 403},
		{"POST", "/tasks", 403},
	} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.Header.Set("Origin", "https://other.example")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Fatal(tc, rec.Code)
		}
	}
}
