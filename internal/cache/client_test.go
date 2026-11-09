package cache

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

func blackhole(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var connections []net.Conn
	var mu sync.Mutex
	go func() {
		defer close(done)
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			connections = append(connections, connection)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		<-done
		mu.Lock()
		defer mu.Unlock()
		for _, connection := range connections {
			_ = connection.Close()
		}
	})
	return listener.Addr().String()
}

func newTestClient(t *testing.T, addr string) *Client {
	t.Helper()
	client, err := New(Options{Enabled: true, Addr: addr, Username: "taskflow_cache", Password: "fixture-secret"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestDisabledCacheHasNoClient(t *testing.T) {
	client, err := New(Options{Addr: "invalid", Username: "", Password: ""})
	if err != nil || client != nil {
		t.Fatal("disabled cache constructed a client")
	}
}

func TestSlowHandshakeAndPoolWaitAreBounded(t *testing.T) {
	client := newTestClient(t, blackhole(t))
	options := client.rdb.Options()
	if options.MaxRetries != 0 || options.DialerRetries != 1 || options.MaxActiveConns != poolSize || options.PipelinePoolSize != -1 {
		t.Fatal("retry or connection limits changed")
	}
	ctx := context.Background()
	start := time.Now()
	if err := client.Ping(ctx); !errors.Is(err, ErrUnavailable) || ctx.Err() != nil {
		t.Fatal("slow optional dependency lost error or cancelled parent")
	}
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("50ms operation exceeded test scheduling tolerance: %v", elapsed)
	} else {
		t.Logf("unresponsive handshake: %v (budget %v)", elapsed, OperationTimeout)
	}
	var wg sync.WaitGroup
	errorsOut := make(chan error, 12)
	start = time.Now()
	for range 12 {
		wg.Add(1)
		go func() { defer wg.Done(); errorsOut <- client.Ping(context.Background()) }()
	}
	wg.Wait()
	close(errorsOut)
	for err := range errorsOut {
		if !errors.Is(err, ErrUnavailable) {
			t.Fatal("unexpected pool exhaustion error")
		}
	}
	if time.Since(start) > 250*time.Millisecond || client.rdb.PoolStats().TotalConns > poolSize {
		t.Fatal("pool wait or connection count is unbounded")
	}
}

func TestRefusedConnectionCancellationAndClose(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()
	client := newTestClient(t, addr)
	start := time.Now()
	if err := client.Ping(context.Background()); !errors.Is(err, ErrUnavailable) || time.Since(start) > 250*time.Millisecond {
		t.Fatal("connection refusal did not fail promptly")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := client.Ping(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("caller cancellation lost")
	}
	if err := client.Close(); err != nil {
		t.Fatal("close failed")
	}
	if err := client.Ping(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatal("closed client remained usable")
	}
}

func TestCacheInputAndErrorRedaction(t *testing.T) {
	client := newTestClient(t, "127.0.0.1:1")
	for _, key := range []string{"", "other-project:key", KeyPrefix, KeyPrefix + strings.Repeat("x", 512)} {
		if _, err := client.Get(context.Background(), key); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid key accepted")
		}
		if err := client.Delete(context.Background(), key); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid delete accepted")
		}
	}
	for _, ttl := range []time.Duration{0, -time.Second, time.Nanosecond} {
		if err := client.Set(context.Background(), KeyPrefix+"fixture", []byte("value"), ttl); !errors.Is(err, ErrInvalid) {
			t.Fatal("nonexpiring or sub-millisecond write accepted")
		}
	}
	if err := client.Set(context.Background(), KeyPrefix+"fixture", make([]byte, MaxValueBytes+1), time.Second); !errors.Is(err, ErrInvalid) {
		t.Fatal("oversized write accepted")
	}
	if err := outcome(context.Background(), errors.New("fixture-secret")); err != ErrUnavailable {
		t.Fatal("raw error leaked")
	}
}

func TestCloseDuringPendingCommand(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	accepted := make(chan net.Conn, 1)
	go func() {
		connection, err := listener.Accept()
		if err == nil {
			accepted <- connection
		}
		close(accepted)
	}()
	client := newTestClient(t, listener.Addr().String())
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- client.Ping(ctx) }()
	select {
	case connection := <-accepted:
		if connection == nil {
			t.Fatal("listener failed")
		}
		defer connection.Close()
	case <-ctx.Done():
		t.Fatal("pending command did not connect")
	}
	started := time.Now()
	if err := client.Close(); err != nil {
		t.Fatal("close failed")
	}
	select {
	case err := <-done:
		if !errors.Is(err, ErrUnavailable) || ctx.Err() != nil {
			t.Fatal("closing a pending command lost its bounded error or cancelled parent")
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("in-flight command outlived the close budget")
	}
	if time.Since(started) > 250*time.Millisecond {
		t.Fatal("client close was unbounded")
	}
}
