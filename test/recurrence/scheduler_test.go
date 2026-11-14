package recurrence_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	applicationrecurrence "github.com/pluto-han/taskflow/internal/application/recurrence"
	infradatabase "github.com/pluto-han/taskflow/internal/infra/database"
	interfacesschedulerrecurrence "github.com/pluto-han/taskflow/internal/interfaces/scheduler/recurrence"
)

type batchFunc func(context.Context, func() time.Time) (applicationrecurrence.BatchResult, error)

func (f batchFunc) GenerateBatch(c context.Context, n func() time.Time) (applicationrecurrence.BatchResult, error) {
	return f(c, n)
}
func TestSchedulerBackoffRecoveryAndCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	var log bytes.Buffer
	runner := batchFunc(func(ctx context.Context, _ func() time.Time) (applicationrecurrence.BatchResult, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("unbounded batch")
		}
		calls++
		if calls <= 6 {
			return applicationrecurrence.BatchResult{}, errors.New("private task title secret-dsn")
		}
		return applicationrecurrence.BatchResult{Processed: applicationrecurrence.BatchSize, Generated: 1}, nil
	})
	var delays []time.Duration
	wait := func(_ context.Context, d time.Duration) bool {
		delays = append(delays, d)
		if calls == 8 {
			cancel()
			return false
		}
		return true
	}
	s := interfacesschedulerrecurrence.NewWithClock(runner, slog.New(slog.NewTextHandler(&log, nil)), nil, time.Now, wait)
	s.Run(ctx)
	want := []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 5 * time.Minute, 5 * time.Minute, time.Second, time.Second}
	if !reflect.DeepEqual(delays, want) {
		t.Fatal(delays)
	}
	if strings.Contains(log.String(), "secret-dsn") || strings.Contains(log.String(), "private task") {
		t.Fatal("private error leaked")
	}
}
func TestSchedulerShutdownAndDayRetry(t *testing.T) {
	started := make(chan struct{})
	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	s := interfacesschedulerrecurrence.NewScheduler(batchFunc(func(ctx context.Context, _ func() time.Time) (applicationrecurrence.BatchResult, error) {
		close(started)
		<-ctx.Done()
		return applicationrecurrence.BatchResult{}, ctx.Err()
	}), nil, nil)
	go func() { defer close(done); s.Run(ctx) }()
	<-started
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not exit")
	}
	runner := batchFunc(func(context.Context, func() time.Time) (applicationrecurrence.BatchResult, error) {
		return applicationrecurrence.BatchResult{}, applicationrecurrence.ErrDayChanged
	})
	wait := func(_ context.Context, d time.Duration) bool {
		if d != time.Second {
			t.Fatal(d)
		}
		return false
	}
	s = interfacesschedulerrecurrence.NewWithClock(runner, nil, nil, time.Now, wait)
	s.Run(context.Background())
	canceled, stop := context.WithCancel(context.Background())
	stop()
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		interfacesschedulerrecurrence.NewScheduler(runner, nil, nil).Run(canceled)
	}()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("timer ignored cancellation")
	}
}
func TestWorkerConnectionBudget(t *testing.T) {
	cfg, err := infradatabase.WorkerPoolConfig("postgres://user:password@127.0.0.1/db?pool_max_conns=20&pool_min_conns=5")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxConns != 1 || cfg.MinConns != 0 || cfg.MinIdleConns != 0 {
		t.Fatal("worker exceeded one connection")
	}
}
