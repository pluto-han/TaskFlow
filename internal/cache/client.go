// Package cache provides an optional, short-lived Redis connection helper.
// PostgreSQL remains the source of truth; cache errors must permit database fallback.
package cache

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/redis/go-redis/v9/maintnotifications"
)

const (
	OperationTimeout = 50 * time.Millisecond
	KeyPrefix        = "taskflow:cache:"
	MaxValueBytes    = 256 * 1024
	poolSize         = 4
)

var (
	ErrMiss        = errors.New("cache miss")
	ErrUnavailable = errors.New("cache unavailable")
	ErrInvalid     = errors.New("invalid cache input")
)

// Store is the small replaceable boundary used by the Today Service.
type Store interface {
	Get(context.Context, string) ([]byte, error)
	Set(context.Context, string, []byte, time.Duration) error
	Delete(context.Context, string) error
}

type Options struct {
	Enabled  bool
	Addr     string
	Username string
	Password string
}

// ValidateEndpoint accepts host:port only, never a URL or inline credentials.
func ValidateEndpoint(addr, username string) error {
	host, port, err := net.SplitHostPort(addr)
	n, parseErr := strconv.Atoi(port)
	if err != nil || parseErr != nil || host == "" || n < 1 || n > 65535 ||
		strings.ContainsAny(addr, "@/\\ \t\r\n") || username == "" ||
		strings.ContainsAny(username, " \t\r\n") || username == "default" {
		return errors.New("set REDIS_ADDR to host:port and REDIS_USERNAME to a dedicated user")
	}
	return nil
}

type Client struct {
	rdb *redis.Client
}

var _ Store = (*Client)(nil)

// New is lazy: disabled means no client, goroutines, connection or Ping.
func New(options Options) (*Client, error) {
	if !options.Enabled {
		return nil, nil
	}
	if err := ValidateEndpoint(options.Addr, options.Username); err != nil {
		return nil, err
	}
	if options.Password == "" {
		return nil, errors.New("Redis password is required")
	}
	return &Client{rdb: redis.NewClient(&redis.Options{
		Addr: options.Addr, Username: options.Username, Password: options.Password,
		Protocol: 2, DisableIdentity: true,
		MaintNotificationsConfig: &maintnotifications.Config{Mode: maintnotifications.ModeDisabled},
		MaxRetries:               -1, DialerRetries: 1,
		DialTimeout: OperationTimeout, ReadTimeout: OperationTimeout, WriteTimeout: OperationTimeout,
		ContextTimeoutEnabled: true,
		PoolSize:              poolSize, MaxActiveConns: poolSize, MaxConcurrentDials: 1,
		PoolTimeout: OperationTimeout, MinIdleConns: 0, MaxIdleConns: poolSize,
		PipelinePoolSize: -1,
		ConnMaxIdleTime:  time.Minute, ConnMaxLifetime: 5 * time.Minute,
	})}, nil
}

func validKey(key string) bool {
	return strings.HasPrefix(key, KeyPrefix) && len(key) > len(KeyPrefix) && len(key) <= 512 &&
		!strings.ContainsAny(key, "\x00\r\n")
}

// outcome never exposes Redis errors, endpoint credentials or payload contents.
func outcome(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err == nil {
		return nil
	}
	if errors.Is(err, redis.Nil) {
		return ErrMiss
	}
	return ErrUnavailable
}

func (c *Client) Ping(ctx context.Context) error {
	short, cancel := context.WithTimeout(ctx, OperationTimeout)
	defer cancel()
	return outcome(ctx, c.rdb.Ping(short).Err())
}

func (c *Client) Get(ctx context.Context, key string) ([]byte, error) {
	if !validKey(key) {
		return nil, ErrInvalid
	}
	short, cancel := context.WithTimeout(ctx, OperationTimeout)
	defer cancel()
	value, err := c.rdb.Get(short, key).Bytes()
	if err := outcome(ctx, err); err != nil {
		return nil, err
	}
	if len(value) > MaxValueBytes {
		return nil, ErrInvalid
	}
	return value, nil
}

func (c *Client) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if !validKey(key) || len(value) > MaxValueBytes || ttl < time.Millisecond {
		return ErrInvalid
	}
	short, cancel := context.WithTimeout(ctx, OperationTimeout)
	defer cancel()
	return outcome(ctx, c.rdb.Set(short, key, value, ttl).Err())
}

func (c *Client) Delete(ctx context.Context, key string) error {
	if !validKey(key) {
		return ErrInvalid
	}
	short, cancel := context.WithTimeout(ctx, OperationTimeout)
	defer cancel()
	return outcome(ctx, c.rdb.Del(short, key).Err())
}

func (c *Client) Close() error { return c.rdb.Close() }
