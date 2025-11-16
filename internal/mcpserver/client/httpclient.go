package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const (
	// MaxRetries is the maximum number of retry attempts for retryable errors
	MaxRetries = 3

	// DefaultBackoff is the initial backoff duration for exponential backoff
	DefaultBackoff = 1 * time.Second
)

// HTTPClient wraps http.Client with authentication and retry logic
// Automatically injects:
// - Authorization: Bearer <token> (production) OR X-Debug-Sub (dev mode)
// - X-Sync-Session: <session-id>
// - X-Sync-Epoch: <epoch>
// - X-Correlation-ID: <uuid>
//
// Handles retries for:
// - 401 Unauthorized: invalidate token cache, retry once
// - 409 Conflict (epoch mismatch): refresh session, retry once
// - 428 Precondition Required: session missing/expired, refresh session, retry once
// - 429 Too Many Requests: respect Retry-After, exponential backoff
//
// Dev Mode: If tokenProvider is nil, the client operates in dev mode and uses
// X-Debug-Sub header from the session manager instead of Bearer tokens.
type HTTPClient struct {
	baseURL       string
	httpClient    *http.Client
	tokenProvider TokenProvider  // nil in dev mode
	sessionMgr    SessionProvider // Required for session headers
	audience      string
	debugSub      string // Subject to use in dev mode (from session manager)
}

// NewHTTPClient creates a new authenticated HTTP client
// For production: provide tokenProvider, sessionMgr, and audience; pass "" for debugSub
// For dev mode: pass nil for tokenProvider, use NewDevSessionManager for sessionMgr, provide debugSub
func NewHTTPClient(baseURL string, tokenProvider TokenProvider, sessionMgr SessionProvider, audience, debugSub string) *HTTPClient {
	return &HTTPClient{
		baseURL:       baseURL,
		httpClient:    &http.Client{Timeout: 30 * time.Second},
		tokenProvider: tokenProvider,
		sessionMgr:    sessionMgr,
		audience:      audience,
		debugSub:      debugSub,
	}
}

// Do executes an HTTP request with auto-injection of auth headers and retry logic
// This is the main entry point for all HTTP requests
func (c *HTTPClient) Do(ctx context.Context, req *http.Request) (*http.Response, error) {
	// Generate correlation ID for request tracing
	correlationID := uuid.New().String()

	logger := log.With().
		Str("method", req.Method).
		Str("url", req.URL.String()).
		Str("correlationId", correlationID).
		Logger()

	// Execute request with retries (headers injected per attempt)
	return c.doWithRetry(ctx, req, &logger, correlationID, 0)
}

// doWithRetry handles retry logic for 401, 409, 429
// Reference: /Users/erauner/git/side/ToolBridge/lib/mcp/transport/authorized_transport_helper.dart:55-120
func (c *HTTPClient) doWithRetry(ctx context.Context, req *http.Request, logger *zerolog.Logger, correlationID string, retryCount int) (*http.Response, error) {
	// Clone request (body may need to be re-sent on retry)
	reqClone, err := cloneRequest(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to clone request: %w", err)
	}

	// Inject correlation ID
	reqClone.Header.Set("X-Correlation-ID", correlationID)

	// Inject authentication headers (fresh on each attempt)
	if c.tokenProvider == nil {
		// Dev mode: use X-Debug-Sub header
		reqClone.Header.Set("X-Debug-Sub", c.debugSub)
		logger.Debug().Str("debugSub", c.debugSub).Msg("using dev mode auth (X-Debug-Sub)")
	} else {
		// Production mode: get Auth0 token
		token, err := c.tokenProvider.GetToken(ctx, c.audience, "", false)
		if err != nil {
			return nil, fmt.Errorf("failed to get auth token: %w", err)
		}
		reqClone.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token.AccessToken))
		logger.Debug().Msg("injected bearer token")
	}

	// Inject session headers (if session manager is configured)
	if c.sessionMgr != nil {
		session, err := c.sessionMgr.EnsureSession(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to ensure session: %w", err)
		}

		reqClone.Header.Set("X-Sync-Session", session.ID)
		reqClone.Header.Set("X-Sync-Epoch", strconv.Itoa(session.Epoch))

		logger.Debug().
			Str("sessionId", session.ID).
			Int("epoch", session.Epoch).
			Msg("injected session headers")
	}

	// Execute request
	start := time.Now()
	resp, err := c.httpClient.Do(reqClone)
	duration := time.Since(start)

	if err != nil {
		logger.Error().Err(err).Dur("duration", duration).Msg("HTTP request failed")
		return nil, err
	}

	logger.Debug().
		Int("status", resp.StatusCode).
		Dur("duration", duration).
		Int("retryCount", retryCount).
		Msg("HTTP request completed")

	// Handle retry scenarios
	switch resp.StatusCode {
	case http.StatusUnauthorized: // 401
		return c.handleUnauthorized(ctx, req, resp, logger, correlationID, retryCount)

	case http.StatusConflict: // 409 (epoch mismatch or version mismatch)
		return c.handleConflict(ctx, req, resp, logger, correlationID, retryCount)

	case http.StatusPreconditionRequired: // 428 (session missing or expired)
		return c.handlePreconditionRequired(ctx, req, resp, logger, correlationID, retryCount)

	case http.StatusTooManyRequests: // 429
		return c.handleRateLimit(ctx, req, resp, logger, correlationID, retryCount)

	default:
		// Success or non-retryable error - return as-is
		return resp, nil
	}
}

// handleUnauthorized handles 401 Unauthorized by invalidating token and retrying
func (c *HTTPClient) handleUnauthorized(ctx context.Context, req *http.Request, resp *http.Response, logger *zerolog.Logger, correlationID string, retryCount int) (*http.Response, error) {
	resp.Body.Close()

	if retryCount >= MaxRetries {
		logger.Warn().Msg("401 Unauthorized - max retries exceeded")
		return nil, fmt.Errorf("authentication failed after %d retries", retryCount)
	}

	if c.tokenProvider == nil {
		// Dev mode: 401 is a real error (X-Debug-Sub not working)
		logger.Error().Msg("401 in dev mode - check X-Debug-Sub header support")
		return nil, fmt.Errorf("authentication failed in dev mode")
	}

	logger.Warn().Msg("401 Unauthorized - invalidating token and retrying")

	// Invalidate cached token
	c.tokenProvider.InvalidateToken(c.audience, "")

	// Retry once with fresh token
	return c.doWithRetry(ctx, req, logger, correlationID, retryCount+1)
}

// handleConflict handles 409 Conflict (epoch mismatch or version mismatch)
func (c *HTTPClient) handleConflict(ctx context.Context, req *http.Request, resp *http.Response, logger *zerolog.Logger, correlationID string, retryCount int) (*http.Response, error) {
	// Try to parse error response to distinguish epoch mismatch from version mismatch
	var errResp struct {
		Error         string `json:"error"`
		Epoch         int    `json:"epoch,omitempty"`
		CorrelationID string `json:"correlation_id,omitempty"`
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	resp.Body.Close()

	if err == nil {
		if jsonErr := json.Unmarshal(bodyBytes, &errResp); jsonErr == nil {
			// Check if this is an epoch mismatch
			if errResp.Error == "epoch_mismatch" {
				// Also check for epoch in X-Sync-Epoch header
				if epochHeader := resp.Header.Get("X-Sync-Epoch"); epochHeader != "" {
					if e, parseErr := strconv.Atoi(epochHeader); parseErr == nil {
						errResp.Epoch = e
					}
				}

				return c.handleEpochMismatch(ctx, req, errResp.Epoch, logger, correlationID, retryCount)
			}
		}
	}

	// Not epoch mismatch - likely version mismatch or other conflict
	// Return error response as-is (caller will handle)
	logger.Warn().
		Str("error", errResp.Error).
		Msg("409 Conflict - not epoch mismatch, returning to caller")

	// Reconstruct response with body
	resp.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	return resp, nil
}

// handleEpochMismatch handles epoch mismatch by refreshing session and retrying
func (c *HTTPClient) handleEpochMismatch(ctx context.Context, req *http.Request, serverEpoch int, logger *zerolog.Logger, correlationID string, retryCount int) (*http.Response, error) {
	if retryCount >= MaxRetries {
		logger.Error().Int("serverEpoch", serverEpoch).Msg("Epoch mismatch - max retries exceeded")
		return nil, ErrEpochMismatch{ServerEpoch: serverEpoch}
	}

	if c.sessionMgr == nil {
		// No session manager - can't recover
		logger.Error().Msg("Epoch mismatch but no session manager configured")
		return nil, ErrEpochMismatch{ServerEpoch: serverEpoch}
	}

	logger.Warn().
		Int("serverEpoch", serverEpoch).
		Msg("Epoch mismatch - refreshing session and retrying")

	// Invalidate session and retry (next call will create new session with new epoch)
	c.sessionMgr.InvalidateSession()

	return c.doWithRetry(ctx, req, logger, correlationID, retryCount+1)
}

// handlePreconditionRequired handles 428 Precondition Required (session missing or expired)
func (c *HTTPClient) handlePreconditionRequired(ctx context.Context, req *http.Request, resp *http.Response, logger *zerolog.Logger, correlationID string, retryCount int) (*http.Response, error) {
	resp.Body.Close()

	if retryCount >= MaxRetries {
		logger.Error().Msg("428 Precondition Required - max retries exceeded")
		return nil, fmt.Errorf("session precondition failed after %d retries", retryCount)
	}

	if c.sessionMgr == nil {
		// No session manager - can't recover
		logger.Error().Msg("428 Precondition Required but no session manager configured")
		return nil, fmt.Errorf("session required but no session manager configured")
	}

	logger.Warn().Msg("428 Precondition Required - session missing or expired, refreshing and retrying")

	// Invalidate session and retry (next call will create new session)
	c.sessionMgr.InvalidateSession()

	return c.doWithRetry(ctx, req, logger, correlationID, retryCount+1)
}

// handleRateLimit handles 429 Too Many Requests with exponential backoff
func (c *HTTPClient) handleRateLimit(ctx context.Context, req *http.Request, resp *http.Response, logger *zerolog.Logger, correlationID string, retryCount int) (*http.Response, error) {
	resp.Body.Close()

	if retryCount >= MaxRetries {
		logger.Warn().Msg("Rate limited - max retries exceeded")
		retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
		return nil, ErrRateLimited{RetryAfter: int(retryAfter.Seconds())}
	}

	// Parse Retry-After header (seconds or HTTP-date)
	retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))

	// Apply exponential backoff if no Retry-After header
	if retryAfter == 0 {
		retryAfter = DefaultBackoff * time.Duration(1<<retryCount)
	}

	logger.Warn().
		Dur("retryAfter", retryAfter).
		Int("retryCount", retryCount).
		Str("rateLimitRemaining", resp.Header.Get("X-RateLimit-Remaining")).
		Str("rateLimitReset", resp.Header.Get("X-RateLimit-Reset")).
		Msg("Rate limited - backing off")

	// Wait before retry
	select {
	case <-time.After(retryAfter):
		return c.doWithRetry(ctx, req, logger, correlationID, retryCount+1)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// cloneRequest creates a copy of an HTTP request for retry
// Preserves the request body by reading and restoring it
func cloneRequest(ctx context.Context, req *http.Request) (*http.Request, error) {
	var bodyBytes []byte
	if req.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		req.Body.Close()
		// Restore original request body
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}

	reqClone, err := http.NewRequestWithContext(ctx, req.Method, req.URL.String(), bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}

	// Copy headers (skip auth/session headers as they will be re-injected)
	for k, v := range req.Header {
		if k == "Authorization" || k == "X-Sync-Session" || k == "X-Sync-Epoch" || k == "X-Debug-Sub" {
			continue // Will be re-injected
		}
		reqClone.Header[k] = v
	}

	return reqClone, nil
}

// parseRetryAfter parses the Retry-After header
// Supports both integer seconds and HTTP-date format
// Reference: internal/httpapi/ratelimit.go
func parseRetryAfter(value string) time.Duration {
	if value == "" {
		return 0
	}

	// Try parsing as integer (seconds)
	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}

	// Try parsing as HTTP-date
	if t, err := http.ParseTime(value); err == nil {
		duration := time.Until(t)
		if duration > 0 {
			return duration
		}
	}

	// Fallback
	return 0
}
