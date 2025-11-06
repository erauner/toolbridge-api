package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/erauner12/toolbridge-api/internal/auth"
)

func TestRateLimiting_429Response(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	// Clean up tables
	_, _ = pool.Exec(context.Background(), "DELETE FROM note")

	// Create server with very restrictive rate limit for testing
	srv := &Server{
		DB: pool,
		RateLimitConfig: RateLimitInfo{
			WindowSeconds: 60,
			MaxRequests:   10, // Very low for testing
			Burst:         2,  // Allow only 2 requests in burst
		},
	}
	router := srv.Routes(auth.JWTCfg{HS256Secret: "test-secret", DevMode: true})

	// Create a session first
	sessionReq := httptest.NewRequest("POST", "/v1/sync/sessions", nil)
	sessionReq.Header.Set("X-Debug-Sub", "test-user")
	sessionRec := httptest.NewRecorder()
	router.ServeHTTP(sessionRec, sessionReq)

	if sessionRec.Code != 201 {
		t.Fatalf("Failed to create session: got status %d", sessionRec.Code)
	}

	var sessionResp struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(sessionRec.Body).Decode(&sessionResp); err != nil {
		t.Fatalf("Failed to decode session response: %v", err)
	}
	sessionID := sessionResp.ID

	// Make requests until rate limited
	// Burst is 2, so first 2 should succeed, 3rd should fail with 429
	for i := 1; i <= 3; i++ {
		req := httptest.NewRequest("POST", "/v1/sync/notes/push", nil)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Debug-Sub", "test-user")
		req.Header.Set("X-Sync-Session", sessionID)

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		t.Logf("Request %d: status=%d", i, rec.Code)

		// Check rate limit headers are always present
		limitHeader := rec.Header().Get("X-RateLimit-Limit")
		remainingHeader := rec.Header().Get("X-RateLimit-Remaining")
		resetHeader := rec.Header().Get("X-RateLimit-Reset")
		burstHeader := rec.Header().Get("X-RateLimit-Burst")

		if limitHeader == "" {
			t.Errorf("Request %d: X-RateLimit-Limit header missing", i)
		}
		if remainingHeader == "" {
			t.Errorf("Request %d: X-RateLimit-Remaining header missing", i)
		}
		if resetHeader == "" {
			t.Errorf("Request %d: X-RateLimit-Reset header missing", i)
		}
		if burstHeader == "" {
			t.Errorf("Request %d: X-RateLimit-Burst header missing", i)
		}

		remaining, _ := strconv.Atoi(remainingHeader)
		t.Logf("Request %d: remaining=%d", i, remaining)

		if i <= 2 {
			// First 2 requests should succeed (burst capacity)
			if rec.Code == 429 {
				t.Errorf("Request %d: Expected success (within burst), got 429: %s",
					i, rec.Body.String())
			}

			// Remaining should decrease
			expectedRemaining := 2 - i
			if remaining != expectedRemaining {
				t.Errorf("Request %d: Expected remaining=%d, got %d",
					i, expectedRemaining, remaining)
			}
		} else {
			// 3rd request should be rate limited
			if rec.Code != 429 {
				t.Errorf("Request %d: Expected 429 Too Many Requests, got %d: %s",
					i, rec.Code, rec.Body.String())
			}

			// Check Retry-After header
			retryAfter := rec.Header().Get("Retry-After")
			if retryAfter == "" {
				t.Error("Retry-After header missing on 429 response")
			} else {
				retrySeconds, err := strconv.Atoi(retryAfter)
				if err != nil {
					t.Errorf("Invalid Retry-After value: %s", retryAfter)
				}
				if retrySeconds < 1 {
					t.Errorf("Retry-After should be >= 1, got %d", retrySeconds)
				}
				t.Logf("Retry-After: %d seconds", retrySeconds)
			}

			// Remaining should be 0 when rate limited
			if remaining != 0 {
				t.Errorf("Request %d: Expected remaining=0 when rate limited, got %d",
					i, remaining)
			}
		}
	}
}

func TestRateLimiting_HeaderValues(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	// Clean up tables
	_, _ = pool.Exec(context.Background(), "DELETE FROM note")

	srv := &Server{
		DB: pool,
		RateLimitConfig: RateLimitInfo{
			WindowSeconds: 60,
			MaxRequests:   100,
			Burst:         20,
		},
	}
	router := srv.Routes(auth.JWTCfg{HS256Secret: "test-secret", DevMode: true})

	// Create session
	sessionReq := httptest.NewRequest("POST", "/v1/sync/sessions", nil)
	sessionReq.Header.Set("X-Debug-Sub", "test-user")
	sessionRec := httptest.NewRecorder()
	router.ServeHTTP(sessionRec, sessionReq)

	var sessionResp struct {
		ID string `json:"id"`
	}
	json.NewDecoder(sessionRec.Body).Decode(&sessionResp)

	// Make a single request
	req := httptest.NewRequest("POST", "/v1/sync/notes/push", nil)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Debug-Sub", "test-user")
	req.Header.Set("X-Sync-Session", sessionResp.ID)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	// Verify header values match config
	limit := rec.Header().Get("X-RateLimit-Limit")
	if limit != "100" {
		t.Errorf("Expected X-RateLimit-Limit=100, got %s", limit)
	}

	burst := rec.Header().Get("X-RateLimit-Burst")
	if burst != "20" {
		t.Errorf("Expected X-RateLimit-Burst=20, got %s", burst)
	}

	remaining := rec.Header().Get("X-RateLimit-Remaining")
	remainingInt, _ := strconv.Atoi(remaining)
	if remainingInt < 0 || remainingInt > 20 {
		t.Errorf("Expected X-RateLimit-Remaining between 0-20, got %s", remaining)
	}

	resetTime := rec.Header().Get("X-RateLimit-Reset")
	resetUnix, err := strconv.ParseInt(resetTime, 10, 64)
	if err != nil {
		t.Errorf("Invalid X-RateLimit-Reset value: %s", resetTime)
	}
	if resetUnix < time.Now().Unix() {
		t.Error("X-RateLimit-Reset should be in the future")
	}
}

func TestRateLimiting_NoSession(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	srv := &Server{
		DB:              pool,
		RateLimitConfig: DefaultRateLimitConfig,
	}
	router := srv.Routes(auth.JWTCfg{HS256Secret: "test-secret", DevMode: true})

	// Try to push without session - should get 428, not 429
	req := httptest.NewRequest("POST", "/v1/sync/notes/push", nil)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Debug-Sub", "test-user")
	// No X-Sync-Session header

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != 428 {
		t.Errorf("Expected 428 Precondition Required (no session), got %d: %s",
			rec.Code, rec.Body.String())
	}
}

func TestRateLimiting_RemainingDecreases(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	// Clean up tables
	_, _ = pool.Exec(context.Background(), "DELETE FROM note")

	srv := &Server{
		DB: pool,
		RateLimitConfig: RateLimitInfo{
			WindowSeconds: 60,
			MaxRequests:   100,
			Burst:         5,
		},
	}
	router := srv.Routes(auth.JWTCfg{HS256Secret: "test-secret", DevMode: true})

	// Create session
	sessionReq := httptest.NewRequest("POST", "/v1/sync/sessions", nil)
	sessionReq.Header.Set("X-Debug-Sub", "test-user")
	sessionRec := httptest.NewRecorder()
	router.ServeHTTP(sessionRec, sessionReq)

	var sessionResp struct {
		ID string `json:"id"`
	}
	json.NewDecoder(sessionRec.Body).Decode(&sessionResp)

	// Track remaining count decreasing
	var prevRemaining int = 5 // Initial burst capacity

	for i := 1; i <= 3; i++ {
		req := httptest.NewRequest("POST", "/v1/sync/notes/push", nil)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Debug-Sub", "test-user")
		req.Header.Set("X-Sync-Session", sessionResp.ID)

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		remaining, _ := strconv.Atoi(rec.Header().Get("X-RateLimit-Remaining"))

		t.Logf("Request %d: remaining=%d (previous=%d)", i, remaining, prevRemaining)

		// Remaining should decrease with each request
		if remaining >= prevRemaining {
			t.Errorf("Request %d: Expected remaining to decrease, got %d (was %d)",
				i, remaining, prevRemaining)
		}

		// Remaining should never be negative
		if remaining < 0 {
			t.Errorf("Request %d: Remaining count should never be negative, got %d",
				i, remaining)
		}

		prevRemaining = remaining
	}
}

func TestRateLimiting_PerUser(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := getTestDB(t)
	defer pool.Close()

	// Clean up tables
	_, _ = pool.Exec(context.Background(), "DELETE FROM note")

	srv := &Server{
		DB: pool,
		RateLimitConfig: RateLimitInfo{
			WindowSeconds: 60,
			MaxRequests:   10,
			Burst:         2,
		},
	}
	router := srv.Routes(auth.JWTCfg{HS256Secret: "test-secret", DevMode: true})

	// Create sessions for two different users
	createSession := func(userID string) string {
		req := httptest.NewRequest("POST", "/v1/sync/sessions", nil)
		req.Header.Set("X-Debug-Sub", userID)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		var resp struct {
			ID string `json:"id"`
		}
		json.NewDecoder(rec.Body).Decode(&resp)
		return resp.ID
	}

	userASession := createSession("user-a")
	userBSession := createSession("user-b")

	// Exhaust user A's rate limit
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("POST", "/v1/sync/notes/push", nil)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Debug-Sub", "user-a")
		req.Header.Set("X-Sync-Session", userASession)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
	}

	// User A should be rate limited
	reqA := httptest.NewRequest("POST", "/v1/sync/notes/push", nil)
	reqA.Header.Set("Content-Type", "application/json")
	reqA.Header.Set("X-Debug-Sub", "user-a")
	reqA.Header.Set("X-Sync-Session", userASession)
	recA := httptest.NewRecorder()
	router.ServeHTTP(recA, reqA)

	if recA.Code != 429 {
		t.Errorf("Expected user-a to be rate limited (429), got %d", recA.Code)
	}

	// User B should NOT be rate limited (separate bucket)
	reqB := httptest.NewRequest("POST", "/v1/sync/notes/push", nil)
	reqB.Header.Set("Content-Type", "application/json")
	reqB.Header.Set("X-Debug-Sub", "user-b")
	reqB.Header.Set("X-Sync-Session", userBSession)
	recB := httptest.NewRecorder()
	router.ServeHTTP(recB, reqB)

	if recB.Code == 429 {
		t.Errorf("Expected user-b NOT to be rate limited, got 429: %s", recB.Body.String())
	}

	remainingB := recB.Header().Get("X-RateLimit-Remaining")
	if remainingB == "0" {
		t.Error("User B should have tokens remaining (independent rate limit)")
	}
}
