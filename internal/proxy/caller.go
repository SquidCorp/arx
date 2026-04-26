package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Sentinel errors returned by Caller.
var (
	// ErrToolNotFound is returned when the requested tool cannot be resolved.
	ErrToolNotFound = errors.New("proxy: tool not found")

	// ErrUpstreamTimeout is returned when the upstream does not respond within
	// the configured per-tool timeout.
	ErrUpstreamTimeout = errors.New("proxy: upstream_timeout")

	// ErrUpstreamError is returned when the upstream returns a 5xx status code.
	// The upstream response body is never exposed.
	ErrUpstreamError = errors.New("proxy: upstream_error")

	// ErrCircuitOpen is returned when the per-tool circuit breaker is open.
	ErrCircuitOpen = errors.New("proxy: circuit_open")
)

// ToolData holds the configuration needed to proxy a request to a tool's
// upstream endpoint.
type ToolData struct {
	// UpstreamURL is the merchant endpoint to proxy to.
	UpstreamURL string
	// UpstreamMethod is the HTTP method for the upstream call.
	UpstreamMethod string
	// ParamMapping is the JSON mapping from standard params to upstream fields.
	ParamMapping string
	// TimeoutMs is the upstream call timeout in milliseconds.
	TimeoutMs int
}

// ToolDataResolver retrieves the full tool configuration for proxying.
type ToolDataResolver interface {
	ResolveToolData(ctx context.Context, tenantID, toolName string) (*ToolData, error)
}

// --- per-tool circuit breaker ---

type cbState int

const (
	cbClosed   cbState = iota // Normal operation.
	cbOpen                    // Rejecting requests.
	cbHalfOpen                // Allowing one probe request.
)

const (
	defaultCBThreshold    = 5
	defaultCBResetTimeout = 30 * time.Second
)

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

// --- Caller options ---

// Option configures a Caller.
type Option func(*callerOptions)

type callerOptions struct {
	cbThreshold    int
	cbResetTimeout time.Duration
	httpClient     *http.Client
}

// WithCBThreshold sets the number of consecutive failures before a tool's
// circuit breaker opens.
func WithCBThreshold(n int) Option {
	return func(o *callerOptions) {
		o.cbThreshold = n
	}
}

// WithCBResetTimeout sets how long a tool's circuit breaker stays open before
// allowing a probe request.
func WithCBResetTimeout(d time.Duration) Option {
	return func(o *callerOptions) {
		o.cbResetTimeout = d
	}
}

// WithHTTPClient overrides the default HTTP client used for upstream calls.
// The client should not have a timeout set — per-tool timeouts are enforced
// via context deadlines.
func WithHTTPClient(c *http.Client) Option {
	return func(o *callerOptions) {
		o.httpClient = c
	}
}

// --- Caller ---

// Caller proxies tool calls to merchant upstream endpoints. It resolves tool
// configuration, maps parameters, enforces per-tool timeouts, and protects
// each tool with an independent circuit breaker.
type Caller struct {
	resolver       ToolDataResolver
	client         *http.Client
	cbThreshold    int
	cbResetTimeout time.Duration

	mu       sync.Mutex
	breakers map[string]*circuitBreaker
}

// NewCaller creates a Caller with the given resolver and options.
func NewCaller(resolver ToolDataResolver, opts ...Option) *Caller {
	o := &callerOptions{
		cbThreshold:    defaultCBThreshold,
		cbResetTimeout: defaultCBResetTimeout,
		httpClient:     &http.Client{},
	}
	for _, opt := range opts {
		opt(o)
	}

	return &Caller{
		resolver:       resolver,
		client:         o.httpClient,
		cbThreshold:    o.cbThreshold,
		cbResetTimeout: o.cbResetTimeout,
		breakers:       make(map[string]*circuitBreaker),
	}
}

// breaker returns the circuit breaker for a given tool, creating one if needed.
func (c *Caller) breaker(tenantID, toolName string) *circuitBreaker {
	key := tenantID + ":" + toolName
	c.mu.Lock()
	defer c.mu.Unlock()

	cb, ok := c.breakers[key]
	if !ok {
		cb = newCircuitBreaker(c.cbThreshold, c.cbResetTimeout)
		c.breakers[key] = cb
	}
	return cb
}

// CallTool resolves tool configuration, maps parameters, and proxies the
// request to the merchant upstream. It implements the mcp.ToolCaller interface.
func (c *Caller) CallTool(
	ctx context.Context,
	tenantID, toolName string,
	params map[string]any,
	_ *http.Request,
) (any, error) {
	td, err := c.resolver.ResolveToolData(ctx, tenantID, toolName)
	if err != nil {
		return nil, err
	}

	// Check circuit breaker.
	cb := c.breaker(tenantID, toolName)
	if !cb.allow() {
		return nil, ErrCircuitOpen
	}

	// Parse param mapping and transform parameters.
	mapping, err := ParseMapping(td.ParamMapping)
	if err != nil {
		return nil, fmt.Errorf("proxy: %w", err)
	}
	mapped := MapParams(mapping, params)

	// Build the upstream request.
	req, err := c.buildRequest(ctx, td, mapped)
	if err != nil {
		return nil, fmt.Errorf("proxy: build request: %w", err)
	}

	// Apply per-tool timeout.
	timeout := time.Duration(td.TimeoutMs) * time.Millisecond
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req = req.WithContext(ctx)

	// Execute.
	resp, err := c.client.Do(req)
	if err != nil {
		cb.recordFailure()
		if ctx.Err() != nil {
			log.Warn().Str("tenant", tenantID).Str("tool", toolName).
				Msg("upstream timeout")
			return nil, ErrUpstreamTimeout
		}
		return nil, fmt.Errorf("proxy: upstream request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	return c.handleResponse(resp, cb, tenantID, toolName)
}

// buildRequest constructs the upstream HTTP request from the mapped parameters.
func (c *Caller) buildRequest(
	ctx context.Context,
	td *ToolData,
	mapped MappedRequest,
) (*http.Request, error) {
	upstreamURL := td.UpstreamURL

	// Append query parameters.
	if len(mapped.Query) > 0 {
		sep := "?"
		for k, v := range mapped.Query {
			upstreamURL += sep + k + "=" + fmt.Sprintf("%v", v)
			sep = "&"
		}
	}

	var body io.Reader
	if len(mapped.Body) > 0 {
		bodyBytes, err := json.Marshal(mapped.Body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		body = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, td.UpstreamMethod, upstreamURL, body)
	if err != nil {
		return nil, err
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return req, nil
}

// handleResponse processes the upstream response according to status code.
func (c *Caller) handleResponse(
	resp *http.Response,
	cb *circuitBreaker,
	tenantID, toolName string,
) (any, error) {
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		cb.recordFailure()
		return nil, fmt.Errorf("proxy: read upstream response: %w", err)
	}

	// 5xx — record failure, mask body.
	if resp.StatusCode >= 500 {
		cb.recordFailure()
		log.Warn().Str("tenant", tenantID).Str("tool", toolName).
			Int("status", resp.StatusCode).Msg("upstream 5xx error")
		return nil, ErrUpstreamError
	}

	// Success (2xx) or client error (4xx) — forward body.
	cb.recordSuccess()

	var result any
	if err := json.Unmarshal(respBody, &result); err != nil {
		// Return raw text if not valid JSON.
		return string(respBody), nil
	}
	return result, nil
}
