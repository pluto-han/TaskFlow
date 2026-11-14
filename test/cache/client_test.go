package cache_test

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	applicationtask "github.com/pluto-han/taskflow/internal/application/task"
	infracache "github.com/pluto-han/taskflow/internal/infra/cache"
)

func blackhole(t *testing.T) (string, *atomic.Int32, <-chan struct{}) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var connections []net.Conn
	var total atomic.Int32
	firstClosed := make(chan struct{})
	var readers sync.WaitGroup
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
			number := total.Add(1)
			readers.Add(1)
			go func() {
				defer readers.Done()
				defer func() {
					if number == 1 {
						close(firstClosed)
					}
				}()
				_, _ = io.Copy(io.Discard, connection)
			}()
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		<-done
		mu.Lock()
		for _, connection := range connections {
			_ = connection.Close()
		}
		mu.Unlock()
		readers.Wait()
	})
	return listener.Addr().String(), &total, firstClosed
}

func newTestClient(t *testing.T, addr string) *infracache.Client {
	t.Helper()
	client, err := infracache.New(infracache.Options{Enabled: true, Addr: addr, Username: "taskflow_cache", Password: "fixture-secret"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestDisabledCacheHasNoClient(t *testing.T) {
	client, err := infracache.New(infracache.Options{Addr: "invalid", Username: "", Password: ""})
	if err != nil || client != nil {
		t.Fatal("disabled cache constructed a client")
	}
}

// Failed connections may be replaced before the server observes EOF. This test
// checks wait budgets; healthy retained connections are checked against real Redis.
func TestSlowHandshakeAndPoolWaitAreBounded(t *testing.T) {
	addr, total, firstClosed := blackhole(t)
	client := newTestClient(t, addr)
	ctx := context.Background()
	start := time.Now()
	if err := client.Ping(ctx); !errors.Is(err, applicationtask.ErrUnavailable) || ctx.Err() != nil {
		t.Fatal("slow optional dependency lost error or cancelled parent")
	}
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("50ms operation exceeded test scheduling tolerance: %v", elapsed)
	} else {
		t.Logf("unresponsive handshake: %v (budget %v)", elapsed, applicationtask.OperationTimeout)
	}
	select {
	case <-firstClosed:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("previous handshake connection did not close")
	}
	if total.Load() != 1 {
		t.Fatal("handshake was retried", total.Load())
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
		if !errors.Is(err, applicationtask.ErrUnavailable) {
			t.Fatal("unexpected pool exhaustion error")
		}
	}
	if time.Since(start) > 250*time.Millisecond {
		t.Fatal("pool wait exceeded the operation budget")
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
	if err := client.Ping(context.Background()); !errors.Is(err, applicationtask.ErrUnavailable) || time.Since(start) > 250*time.Millisecond {
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
	if err := client.Ping(context.Background()); !errors.Is(err, applicationtask.ErrUnavailable) {
		t.Fatal("closed client remained usable")
	}
}

func TestCacheInputAndErrorRedaction(t *testing.T) {
	client := newTestClient(t, "127.0.0.1:1")
	for _, key := range []string{"", "other-project:key", applicationtask.KeyPrefix, applicationtask.KeyPrefix + strings.Repeat("x", 512)} {
		if _, err := client.Get(context.Background(), key); !errors.Is(err, applicationtask.ErrInvalid) {
			t.Fatal("invalid key accepted")
		}
		if err := client.Delete(context.Background(), key); !errors.Is(err, applicationtask.ErrInvalid) {
			t.Fatal("invalid delete accepted")
		}
	}
	for _, ttl := range []time.Duration{0, -time.Second, time.Nanosecond} {
		if err := client.Set(context.Background(), applicationtask.KeyPrefix+"fixture", []byte("value"), ttl); !errors.Is(err, applicationtask.ErrInvalid) {
			t.Fatal("nonexpiring or sub-millisecond write accepted")
		}
	}
	if err := client.Set(context.Background(), applicationtask.KeyPrefix+"fixture", make([]byte, applicationtask.MaxValueBytes+1), time.Second); !errors.Is(err, applicationtask.ErrInvalid) {
		t.Fatal("oversized write accepted")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 4096)
		_, _ = conn.Read(buf)
		_, _ = io.WriteString(conn, "-ERR fixture-secret: private server response\r\n")
	}()
	hostile := newTestClient(t, listener.Addr().String())
	if err := hostile.Ping(context.Background()); err != applicationtask.ErrUnavailable {
		t.Fatal("raw error leaked", err)
	}
	<-done

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
		if !errors.Is(err, applicationtask.ErrUnavailable) || ctx.Err() != nil {
			t.Fatal("closing a pending command lost its bounded error or cancelled parent")
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("in-flight command outlived the close budget")
	}
	if time.Since(started) > 250*time.Millisecond {
		t.Fatal("client close was unbounded")
	}
}
