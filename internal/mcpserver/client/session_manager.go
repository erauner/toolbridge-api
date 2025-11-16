package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	// SessionCacheTTL is the client-side session cache duration (23 hours)
	// Server-side TTL is 24 hours, so we refresh 1 hour early for safety
	SessionCacheTTL = 23 * time.Hour

	// SessionRefreshBuffer is the time before expiry to proactively refresh
	SessionRefreshBuffer = 1 * time.Minute
)

// SessionManager manages REST API sync sessions with caching
// Reference: internal/httpapi/sessions.go (server-side)
type SessionManager struct {
	mu            sync.RWMutex
	baseURL       string
	httpClient    *http.Client
	tokenProvider TokenProvider
	audience      string

	// Cached session (keyed by user ID in the future; single session for now)
	cachedSession *Session
	cacheExpiry   time.Time
}

// NewSessionManager creates a new session manager
func NewSessionManager(baseURL string, tokenProvider TokenProvider, audience string) *SessionManager {
	return &SessionManager{
		baseURL:       baseURL,
		httpClient:    &http.Client{Timeout: 30 * time.Second},
		tokenProvider: tokenProvider,
		audience:      audience,
	}
}

// EnsureSession returns a valid session, creating or refreshing as needed
// This method is thread-safe and will only create one session even with concurrent calls
func (sm *SessionManager) EnsureSession(ctx context.Context) (*Session, error) {
	// Fast path: check if cached session is still valid (read lock only)
	sm.mu.RLock()
	cached := sm.cachedSession
	expiry := sm.cacheExpiry
	sm.mu.RUnlock()

	// Check if cached session is still valid (with refresh buffer)
	if cached != nil && time.Now().Add(SessionRefreshBuffer).Before(expiry) {
		log.Debug().
			Str("sessionId", cached.ID).
			Int("epoch", cached.Epoch).
			Time("expiresAt", expiry).
			Msg("using cached session")
		return cached, nil
	}

	// Slow path: need to create new session
	return sm.createSession(ctx)
}

// InvalidateSession clears the cached session (e.g., on epoch mismatch)
// The next call to EnsureSession will create a fresh session
func (sm *SessionManager) InvalidateSession() {
	sm.mu.Lock()
	sm.cachedSession = nil
	sm.cacheExpiry = time.Time{}
	sm.mu.Unlock()

	log.Debug().Msg("invalidated cached session")
}

// createSession creates a new REST API session
// This method uses a write lock to prevent concurrent session creation
func (sm *SessionManager) createSession(ctx context.Context) (*Session, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Double-check: another goroutine may have created session while we waited for lock
	if sm.cachedSession != nil && time.Now().Add(SessionRefreshBuffer).Before(sm.cacheExpiry) {
		log.Debug().Msg("session created by another goroutine, using cached")
		return sm.cachedSession, nil
	}

	// Get auth token
	token, err := sm.tokenProvider.GetToken(ctx, sm.audience, "", false)
	if err != nil {
		return nil, fmt.Errorf("failed to get auth token: %w", err)
	}

	// Create session request
	url := sm.baseURL + "/v1/sync/sessions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token.AccessToken))
	req.Header.Set("Content-Type", "application/json")

	log.Debug().Str("url", url).Msg("creating new session")

	// Execute request
	resp, err := sm.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("session creation failed with status %d", resp.StatusCode)
	}

	// Parse response
	var sessionResp struct {
		ID        string    `json:"id"`
		UserID    string    `json:"userId"`
		Epoch     int       `json:"epoch"`
		CreatedAt time.Time `json:"createdAt"`
		ExpiresAt time.Time `json:"expiresAt"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&sessionResp); err != nil {
		return nil, fmt.Errorf("failed to parse session response: %w", err)
	}

	// Parse epoch from header (fallback to response body)
	// Reference: internal/httpapi/sessions.go:33-35
	epoch := sessionResp.Epoch
	if epochHeader := resp.Header.Get("X-Sync-Epoch"); epochHeader != "" {
		if e, err := strconv.Atoi(epochHeader); err == nil {
			epoch = e
		}
	}

	// Create session
	session := &Session{
		ID:        sessionResp.ID,
		UserID:    sessionResp.UserID,
		Epoch:     epoch,
		CreatedAt: sessionResp.CreatedAt,
		ExpiresAt: sessionResp.ExpiresAt,
	}

	// Cache with 23-hour TTL (server TTL is 24 hours)
	sm.cachedSession = session
	sm.cacheExpiry = session.CreatedAt.Add(SessionCacheTTL)

	log.Info().
		Str("sessionId", session.ID).
		Int("epoch", epoch).
		Time("expiresAt", sm.cacheExpiry).
		Msg("created new session")

	return session, nil
}
