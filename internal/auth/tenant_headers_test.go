package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// Helper function to generate valid tenant headers for testing
func generateTenantHeaders(secret, tenantID string, timestamp time.Time) map[string]string {
	timestampMs := timestamp.UnixMilli()
	timestampStr := strconv.FormatInt(timestampMs, 10)
	
	message := fmt.Sprintf("%s:%s", tenantID, timestampStr)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	signature := hex.EncodeToString(mac.Sum(nil))
	
	return map[string]string{
		"X-TB-Tenant-ID":  tenantID,
		"X-TB-Timestamp":  timestampStr,
		"X-TB-Signature":  signature,
	}
}

func TestValidateTenantHeaders_Success(t *testing.T) {
	secret := "test-secret-key"
	tenantID := "tenant-123"
	timestamp := time.Now()
	
	req := httptest.NewRequest("GET", "/test", nil)
	headers := generateTenantHeaders(secret, tenantID, timestamp)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	
	validated, err := ValidateTenantHeaders(req, secret, 300)
	if err != nil {
		t.Fatalf("Expected successful validation, got error: %v", err)
	}
	
	if validated.TenantID != tenantID {
		t.Errorf("Expected tenant_id=%s, got %s", tenantID, validated.TenantID)
	}
	
	// Check timestamp is approximately correct (within 1 second)
	timeDiff := validated.Timestamp.Sub(timestamp).Abs()
	if timeDiff > time.Second {
		t.Errorf("Timestamp mismatch: expected %v, got %v (diff: %v)", 
			timestamp, validated.Timestamp, timeDiff)
	}
}

func TestValidateTenantHeaders_MissingHeaders(t *testing.T) {
	secret := "test-secret-key"
	
	tests := []struct {
		name          string
		headers       map[string]string
		expectedError error
	}{
		{
			name:          "missing tenant ID",
			headers:       map[string]string{
				"X-TB-Timestamp": "1234567890000",
				"X-TB-Signature": "abc123",
			},
			expectedError: ErrMissingTenantID,
		},
		{
			name:          "missing timestamp",
			headers:       map[string]string{
				"X-TB-Tenant-ID": "tenant-123",
				"X-TB-Signature": "abc123",
			},
			expectedError: ErrMissingTimestamp,
		},
		{
			name:          "missing signature",
			headers:       map[string]string{
				"X-TB-Tenant-ID": "tenant-123",
				"X-TB-Timestamp": "1234567890000",
			},
			expectedError: ErrMissingSignature,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			
			_, err := ValidateTenantHeaders(req, secret, 300)
			if err != tt.expectedError {
				t.Errorf("Expected error %v, got %v", tt.expectedError, err)
			}
		})
	}
}

func TestValidateTenantHeaders_InvalidTimestamp(t *testing.T) {
	secret := "test-secret-key"
	
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-TB-Tenant-ID", "tenant-123")
	req.Header.Set("X-TB-Timestamp", "not-a-number")
	req.Header.Set("X-TB-Signature", "abc123")
	
	_, err := ValidateTenantHeaders(req, secret, 300)
	if err == nil {
		t.Fatal("Expected error for invalid timestamp, got nil")
	}
	if err != ErrInvalidTimestamp && !contains(err.Error(), "invalid timestamp") {
		t.Errorf("Expected ErrInvalidTimestamp, got: %v", err)
	}
}

func TestValidateTenantHeaders_TimestampSkew(t *testing.T) {
	secret := "test-secret-key"
	tenantID := "tenant-123"
	
	// Timestamp from 10 minutes ago (exceeds default 5 minute window)
	oldTimestamp := time.Now().Add(-10 * time.Minute)
	
	req := httptest.NewRequest("GET", "/test", nil)
	headers := generateTenantHeaders(secret, tenantID, oldTimestamp)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	
	_, err := ValidateTenantHeaders(req, secret, 300) // 300 seconds = 5 minutes
	if err != ErrTimestampSkew {
		t.Errorf("Expected ErrTimestampSkew for old timestamp, got: %v", err)
	}
}

func TestValidateTenantHeaders_InvalidSignature(t *testing.T) {
	secret := "test-secret-key"
	tenantID := "tenant-123"
	timestamp := time.Now()
	
	req := httptest.NewRequest("GET", "/test", nil)
	headers := generateTenantHeaders(secret, tenantID, timestamp)
	
	// Corrupt the signature
	headers["X-TB-Signature"] = "invalid-signature-xxx"
	
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	
	_, err := ValidateTenantHeaders(req, secret, 300)
	if err != ErrInvalidSignature {
		t.Errorf("Expected ErrInvalidSignature for corrupted signature, got: %v", err)
	}
}

func TestValidateTenantHeaders_WrongSecret(t *testing.T) {
	correctSecret := "correct-secret"
	wrongSecret := "wrong-secret"
	tenantID := "tenant-123"
	timestamp := time.Now()
	
	// Generate headers with correct secret
	req := httptest.NewRequest("GET", "/test", nil)
	headers := generateTenantHeaders(correctSecret, tenantID, timestamp)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	
	// Validate with wrong secret
	_, err := ValidateTenantHeaders(req, wrongSecret, 300)
	if err != ErrInvalidSignature {
		t.Errorf("Expected ErrInvalidSignature when using wrong secret, got: %v", err)
	}
}

func TestTenantHeaderMiddleware_Success(t *testing.T) {
	secret := "test-secret-key"
	tenantID := "tenant-123"
	timestamp := time.Now()
	
	// Handler to check tenant context
	handlerCalled := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		
		// Check tenant ID is in context
		ctxTenantID := TenantID(r.Context())
		if ctxTenantID != tenantID {
			t.Errorf("Expected tenant_id=%s in context, got %s", tenantID, ctxTenantID)
		}
		
		w.WriteHeader(http.StatusOK)
	})
	
	// Wrap with middleware
	middleware := TenantHeaderMiddleware(secret, 300)
	wrappedHandler := middleware(handler)
	
	// Create request with valid headers
	req := httptest.NewRequest("GET", "/test", nil)
	headers := generateTenantHeaders(secret, tenantID, timestamp)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	
	// Execute request
	rr := httptest.NewRecorder()
	wrappedHandler.ServeHTTP(rr, req)
	
	// Check handler was called
	if !handlerCalled {
		t.Error("Expected handler to be called")
	}
	
	// Check response status
	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}
}

func TestTenantHeaderMiddleware_InvalidHeaders(t *testing.T) {
	secret := "test-secret-key"
	
	// Handler should NOT be called
	handlerCalled := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})
	
	// Wrap with middleware
	middleware := TenantHeaderMiddleware(secret, 300)
	wrappedHandler := middleware(handler)
	
	// Create request with missing headers
	req := httptest.NewRequest("GET", "/test", nil)
	
	// Execute request
	rr := httptest.NewRecorder()
	wrappedHandler.ServeHTTP(rr, req)
	
	// Check handler was NOT called
	if handlerCalled {
		t.Error("Expected handler NOT to be called with invalid headers")
	}
	
	// Check response status is 401 Unauthorized
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", rr.Code)
	}
}

func TestTenantID_FromContext(t *testing.T) {
	tenantID := "tenant-xyz"
	
	req := httptest.NewRequest("GET", "/test", nil)
	ctx := req.Context()
	
	// Initially empty
	if gotID := TenantID(ctx); gotID != "" {
		t.Errorf("Expected empty tenant_id, got %s", gotID)
	}
	
	// Add tenant ID to context
	ctx = http.Request{}.WithContext(ctx).Context()
	ctx = http.Request{}.WithContext(ctx).Context()
	
	// Using the actual middleware pattern
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gotID := TenantID(r.Context()); gotID != tenantID {
			t.Errorf("Expected tenant_id=%s, got %s", tenantID, gotID)
		}
	})
	
	secret := "test-secret"
	timestamp := time.Now()
	middleware := TenantHeaderMiddleware(secret, 300)
	wrappedHandler := middleware(handler)
	
	req = httptest.NewRequest("GET", "/test", nil)
	headers := generateTenantHeaders(secret, tenantID, timestamp)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	
	rr := httptest.NewRecorder()
	wrappedHandler.ServeHTTP(rr, req)
}

// Helper function to check if error message contains substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[:len(substr)] == substr
}
