package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTenantID_FromContext(t *testing.T) {
	tenantID := "tenant-xyz"

	// Test empty context returns empty string
	ctx := context.Background()
	if gotID := TenantID(ctx); gotID != "" {
		t.Errorf("Expected empty tenant_id from empty context, got %s", gotID)
	}

	// Test context with tenant ID
	ctx = context.WithValue(ctx, TenantIDKey, tenantID)
	if gotID := TenantID(ctx); gotID != tenantID {
		t.Errorf("Expected tenant_id=%s, got %s", tenantID, gotID)
	}
}

func TestTenantID_FromRequest(t *testing.T) {
	tenantID := "tenant-abc"

	// Handler to check tenant context
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID := TenantID(r.Context())
		if gotID != tenantID {
			t.Errorf("Expected tenant_id=%s in request context, got %s", tenantID, gotID)
		}
		w.WriteHeader(http.StatusOK)
	})

	// Create request with tenant context manually set (simulating middleware)
	req := httptest.NewRequest("GET", "/test", nil)
	ctx := context.WithValue(req.Context(), TenantIDKey, tenantID)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}
}

func TestTenantAuthCache(t *testing.T) {
	cache := NewTenantAuthCache()

	subject := "user_123"
	tenantID := "org_456"

	// Initially not cached
	if cache.Get(subject, tenantID) {
		t.Error("Expected cache miss for new entry")
	}

	// Set and verify cached
	cache.Set(subject, tenantID)
	if !cache.Get(subject, tenantID) {
		t.Error("Expected cache hit after Set")
	}

	// Different subject should miss
	if cache.Get("other_user", tenantID) {
		t.Error("Expected cache miss for different subject")
	}

	// Different tenant should miss
	if cache.Get(subject, "other_tenant") {
		t.Error("Expected cache miss for different tenant")
	}
}

// contains checks if a string contains a substring (helper for tests)
func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[:len(substr)] == substr
}
