// Package cache provides a Redis client with circuit breaker support
// for fallback to Postgres when Redis is unavailable.
package cache

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

// ErrCircuitOpen is returned when the circuit breaker is open and Redis
// operations are short-circuited.
var ErrCircuitOpen = errors.New("cache: circuit breaker is open")

type cbState int

const (
	cbClosed   cbState = iota // Normal operation
	cbOpen                    // Rejecting requests
	cbHalfOpen                // Allowing one probe request
)

const (
	defaultThreshold    = 5
	defaultResetTimeout = 30 * time.Second
)

// circuitBreaker tracks Redis availability and prevents cascading failures.
type circuitBreaker struct {
	mu           sync.Mutex
	state        cbState
	failures     int
	threshold    int
	lastFailure  time.Time
	resetTimeout time.Duration
}

func newCircuitBreaker(threshold int, resetTimeout time.Duration) *circuitBreaker {
	return &circuitBreaker{
		state:        cbClosed,
		threshold:    threshold,
		resetTimeout: resetTimeout,
	}
}

// allow checks whether an operation should be attempted.
func (cb *circuitBreaker) allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case cbClosed:
		return true
	case cbOpen:
		if time.Since(cb.lastFailure) > cb.resetTimeout {
			cb.state = cbHalfOpen
			return true
		}
		return false
	case cbHalfOpen:
		return true
	}
	return false
}

// recordSuccess marks a successful operation and closes the breaker.
func (cb *circuitBreaker) recordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures = 0
	cb.state = cbClosed
}

// recordFailure marks a failed operation and may open the breaker.
func (cb *circuitBreaker) recordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures++
	cb.lastFailure = time.Now()

	if cb.state == cbHalfOpen || cb.failures >= cb.threshold {
		cb.state = cbOpen
	}
}

// isOpen reports whether the circuit breaker is in the open state.
func (cb *circuitBreaker) isOpen() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == cbOpen && time.Since(cb.lastFailure) > cb.resetTimeout {
		cb.state = cbHalfOpen
		return false
	}
	return cb.state == cbOpen
}

// Option configures a Client.
type Option func(*clientOptions)

type clientOptions struct {
	threshold    int
	resetTimeout time.Duration
}

// WithCircuitBreakerThreshold sets the number of consecutive failures
// before the circuit breaker opens.
func WithCircuitBreakerThreshold(n int) Option {
	return func(o *clientOptions) {
		o.threshold = n
	}
}

// WithCircuitBreakerResetTimeout sets how long the circuit breaker stays
// open before allowing a probe request.
func WithCircuitBreakerResetTimeout(d time.Duration) Option {
	return func(o *clientOptions) {
		o.resetTimeout = d
	}
}

// Client wraps a Redis connection with circuit breaker support.
type Client struct {
	rdb *redis.Client
	cb  *circuitBreaker
}

// Connect parses a Redis URL, establishes a connection, and verifies
// connectivity with a ping. It returns a Client with circuit breaker protection.
func Connect(ctx context.Context, redisURL string, opts ...Option) (*Client, error) {
	o := &clientOptions{
		threshold:    defaultThreshold,
		resetTimeout: defaultResetTimeout,
	}
	for _, opt := range opts {
		opt(o)
	}

	redisOpts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}

	rdb := redis.NewClient(redisOpts)

	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()
		return nil, err
	}

	log.Info().Str("addr", redisOpts.Addr).Msg("connected to redis")

	return &Client{
		rdb: rdb,
		cb:  newCircuitBreaker(o.threshold, o.resetTimeout),
	}, nil
}

// Ping checks Redis connectivity, respecting the circuit breaker.
func (c *Client) Ping(ctx context.Context) error {
	if !c.cb.allow() {
		return ErrCircuitOpen
	}

	err := c.rdb.Ping(ctx).Err()
	if err != nil {
		c.cb.recordFailure()
		return err
	}

	c.cb.recordSuccess()
	return nil
}

// IsOpen reports whether the circuit breaker is currently open.
func (c *Client) IsOpen() bool {
	return c.cb.isOpen()
}

// Redis returns the underlying redis.Client for direct operations.
// Callers should check IsOpen before using this to respect the circuit breaker.
func (c *Client) Redis() *redis.Client {
	return c.rdb
}

// Do executes a function against Redis, routing through the circuit breaker.
// If the circuit breaker is open, it returns ErrCircuitOpen so callers can
// fall back to Postgres.
func (c *Client) Do(_ context.Context, fn func(rdb *redis.Client) error) error {
	if !c.cb.allow() {
		return ErrCircuitOpen
	}

	err := fn(c.rdb)
	if err != nil {
		c.cb.recordFailure()
		return err
	}

	c.cb.recordSuccess()
	return nil
}

// Close closes the underlying Redis connection.
func (c *Client) Close() error {
	return c.rdb.Close()
}
