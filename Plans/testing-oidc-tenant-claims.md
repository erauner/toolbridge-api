# Testing Plan: OIDC-Derived Tenant Support

**PRs Under Test:**
- Go Backend: erauner12/toolbridge-api#55
- Flutter Client: erauner/ToolBridge#190

**Goal:** Verify end-to-end tenant extraction from OIDC claims works correctly before merging.

---

## Prerequisites

### 1. WorkOS Configuration

**Update JWT Template** (if not already done):
```json
{
  "sub": "{{user.id}}",
  "email": "{{user.email}}",
  "email_verified": {{user.emailVerified}},
  "organization_id": "{{org.id}}"  // ← ADD THIS LINE
}
```

**Where:** WorkOS Dashboard → Configuration → JWT Template

### 2. Deployment Environment

You'll need a test environment where you can deploy BOTH:
- **Go API** from the `feat/oidc-tenant-claims` branch
- **Flutter app** from the `feat/oidc-tenant-claims` branch

Options:
- **Local development**: Run Go API locally, Flutter on device/simulator
- **Staging**: Deploy both to your staging environment
- **Kubernetes dev cluster**: Deploy Helm chart with test values

---

## Test Scenarios

### Test 1: Verify Organization ID in ID Token

**Purpose:** Confirm WorkOS is including `organization_id` in ID tokens.

**Steps:**
1. Sign in to Flutter app (any environment)
2. Use a JWT decoder to inspect the ID token

**How to get the ID token:**

**Option A: Add debug logging** (temporary, for testing):
```dart
// In lib/state/auth/oidc_session_controller.dart, after sign-in:
final idToken = await _tokenBroker.tryGetIdTokenSilently();
if (idToken != null) {
  print('DEBUG ID TOKEN: $idToken');
}
```

**Option B: Use Flutter DevTools** to inspect `_tokenBroker` state

**Verify:**
```json
{
  "sub": "user_01KAHS4J1W6TT5390SR3918ZPF",
  "email": "user@example.com",
  "organization_id": "org_01KABCDEF123456789",  // ← THIS SHOULD EXIST
  "iss": "https://your-app.authkit.app",
  "aud": "client_01KABXHNQ09QGWEX4APPYG2AH5",
  "exp": 1700000000,
  "iat": 1699996400
}
```

**Expected Result:** ✅ `organization_id` claim is present in the token

**If it fails:**
- Check WorkOS JWT template is saved correctly
- Verify user is a member of an organization
- Check WorkOS logs for JWT issuance

---

### Test 2: Flutter Extracts Tenant from Claims

**Purpose:** Verify `TenantResolver` extracts `organization_id` from the ID token.

**Setup:**

1. **Deploy Flutter app** from `feat/oidc-tenant-claims` branch

2. **Configure OIDC** in `assets/oidc_config.json` (or environment variables):
```json
{
  "backendApi": {
    "baseUrl": "https://your-api.example.com",
    "audience": "https://your-api.example.com",
    "tenantClaim": "organization_id",  // ← REQUIRED
    "tenantSource": "id_token",
    "tenantHeaderSecret": "your-test-secret"  // Only for testing HMAC
  }
}
```

3. **Add temporary debug logging** to see tenant resolution:
```dart
// In lib/services/tenant_resolver.dart, after tenant extraction:
if (tenantId != null) {
  print('DEBUG TENANT RESOLVED: $tenantId (source: $source)');
}
```

**Steps:**
1. Launch Flutter app
2. Sign in with WorkOS
3. Check console logs

**Expected Result:**
```
DEBUG TENANT RESOLVED: org_01KABCDEF123456789 (source: id_token)
```

**If it fails:**
- Check `backendApi.tenantClaim` is set to `"organization_id"`
- Verify ID token contains the claim (Test 1)
- Check Flutter logs for errors in `TenantResolver`

---

### Test 3: Go API Receives and Validates Tenant

**Purpose:** Verify the Go API extracts tenant from JWT claims and sets it in context.

**Setup:**

1. **Deploy Go API** from `feat/oidc-tenant-claims` branch

2. **Configure environment variables**:
```bash
# Kubernetes ConfigMap or .env file
TENANT_CLAIM=organization_id
JWT_ISSUER=https://your-app.authkit.app
JWT_JWKS_URL=https://your-app.authkit.app/oauth2/jwks
JWT_AUDIENCE=https://your-api.example.com  # Optional but recommended
```

3. **Add temporary debug logging** to verify tenant in context:
```go
// In internal/httpapi/server.go or a test handler
func (s *Server) handleDebugTenant(w http.ResponseWriter, r *http.Request) {
    tenantID := auth.TenantID(r.Context())
    userID := auth.UserID(r.Context())

    log.Info().
        Str("tenant_id", tenantID).
        Str("user_id", userID).
        Msg("DEBUG: Tenant context")

    json.NewEncoder(w).Encode(map[string]string{
        "tenant_id": tenantID,
        "user_id": userID,
    })
}
```

**Steps:**
1. Sign in to Flutter app
2. Make a sync request (e.g., create a note)
3. Check Go API logs

**Expected Logs:**
```json
{
  "level": "debug",
  "tenant_id": "org_01KABCDEF123456789",
  "claim": "organization_id",
  "message": "tenant derived from JWT claim"
}
```

**Expected Response** (if you added the debug endpoint):
```json
{
  "tenant_id": "org_01KABCDEF123456789",
  "user_id": "user_01KAHS4J1W6TT5390SR3918ZPF"
}
```

**If it fails:**
- Check `TENANT_CLAIM` env var is set
- Verify JWT validation is passing (check for auth errors)
- Check Go logs for "tenant claim not found in JWT" debug message
- Verify Flutter is sending the ID token in `Authorization` header

---

### Test 4: Verify HMAC Headers Override JWT Claims

**Purpose:** Confirm HMAC tenant headers take precedence over JWT claims.

**This tests the contract:** HMAC headers → JWT claims → no tenant

**Setup:**

You need to test with BOTH:
1. **JWT with organization_id claim** (from WorkOS)
2. **HMAC tenant headers** (signed by Flutter)

**Steps:**

1. **Configure Flutter** with `tenantHeaderSecret`:
```json
{
  "backendApi": {
    "tenantId": "static_tenant_123",  // Different from org_id
    "tenantHeaderSecret": "test-secret-key",
    "tenantClaim": "organization_id"
  }
}
```

2. **Configure Go API**:
```bash
TENANT_CLAIM=organization_id
TENANT_HEADER_SECRET=test-secret-key
```

3. **Make a request** from Flutter app (e.g., create note)

4. **Check Go API logs** - you should see:
```json
{
  "level": "debug",
  "tenant_id": "static_tenant_123",  // ← HMAC value, NOT org_id
  "message": "tenant headers validated and context stored"
}
```

**Expected Result:**
- ✅ Tenant ID is `"static_tenant_123"` (from HMAC headers)
- ✅ NOT `"org_01KABCDEF123456789"` (from JWT claim)

**If it fails:**
- Middleware order might be wrong
- HMAC validation might be failing (check signature calculation)
- Check that Flutter is actually sending the headers

---

### Test 5: Verify Fallback to Static Config

**Purpose:** Confirm fallback works when JWT claim is missing.

**Setup:**

1. **Remove organization_id from JWT template** (temporarily):
   - Go to WorkOS Dashboard → JWT Template
   - Remove the `"organization_id": "{{org.id}}"` line
   - Save

2. **Configure Flutter** with static fallback:
```json
{
  "backendApi": {
    "tenantId": "fallback_tenant_456",
    "tenantClaim": "organization_id",
    "tenantHeaderSecret": null  // No HMAC headers
  }
}
```

**Steps:**
1. Sign in to Flutter app
2. Check TenantResolver logs

**Expected Result:**
```
DEBUG TENANT RESOLVED: fallback_tenant_456 (source: config)
```

**Cleanup:** Re-add `organization_id` to WorkOS JWT template after testing.

---

### Test 6: Multi-Org User Switching (Advanced)

**Purpose:** Verify tenant changes when user switches organizations.

**Prerequisites:**
- WorkOS account with multiple organizations
- User is a member of both orgs

**Steps:**

1. **Sign in to Org A**
   - Check tenant ID: should be `org_A_id`

2. **Switch to Org B** (if your app supports org switching)
   - Check tenant ID: should be `org_B_id`

3. **Verify data isolation**:
   - Create note in Org A
   - Switch to Org B
   - Verify note is NOT visible (tenant scoping works)

**Expected Result:**
- ✅ Tenant ID changes when switching orgs
- ✅ Data is scoped correctly per tenant

---

## Automated Test Script (Optional)

If you want to automate some of this, here's a simple test script:

```bash
#!/bin/bash
# test-tenant-extraction.sh

set -e

echo "🧪 Testing OIDC Tenant Extraction"
echo "=================================="

# 1. Sign in and get ID token
echo "Step 1: Sign in to WorkOS..."
# This would use your OIDC client to get a token
ID_TOKEN="<paste-your-id-token-here>"

# 2. Decode and verify organization_id claim
echo "Step 2: Decode ID token..."
PAYLOAD=$(echo $ID_TOKEN | cut -d'.' -f2 | base64 -d 2>/dev/null || echo $ID_TOKEN | cut -d'.' -f2 | base64 -D)
echo $PAYLOAD | jq .

ORG_ID=$(echo $PAYLOAD | jq -r '.organization_id')
if [ "$ORG_ID" == "null" ] || [ -z "$ORG_ID" ]; then
  echo "❌ FAIL: organization_id not found in ID token"
  exit 1
fi
echo "✅ PASS: organization_id found: $ORG_ID"

# 3. Test API endpoint
echo "Step 3: Test API with JWT..."
API_URL="https://your-api.example.com/debug/tenant"
RESPONSE=$(curl -s -H "Authorization: Bearer $ID_TOKEN" $API_URL)
echo $RESPONSE | jq .

API_TENANT=$(echo $RESPONSE | jq -r '.tenant_id')
if [ "$API_TENANT" == "$ORG_ID" ]; then
  echo "✅ PASS: API extracted correct tenant: $API_TENANT"
else
  echo "❌ FAIL: API tenant mismatch. Expected: $ORG_ID, Got: $API_TENANT"
  exit 1
fi

echo ""
echo "🎉 All tests passed!"
```

---

## Success Criteria

Before merging, verify:

- [ ] **Test 1**: WorkOS includes `organization_id` in ID tokens
- [ ] **Test 2**: Flutter `TenantResolver` extracts tenant from claims
- [ ] **Test 3**: Go API validates JWT and sets tenant in context
- [ ] **Test 4**: HMAC headers override JWT claims (precedence works)
- [ ] **Test 5**: Fallback to static config when claim missing
- [ ] **No errors** in Flutter or Go logs related to tenant resolution
- [ ] **Data scoping works**: Notes/tasks/etc are scoped per tenant

---

## Rollback Plan

If testing reveals issues:

1. **Don't merge the PRs** - keep working on the branch
2. **Revert WorkOS JWT template** if you modified it
3. **Keep existing deployments** on `main` branch
4. **Fix issues** in the feature branch
5. **Re-test** before attempting merge again

---

## Production Deployment Checklist

After successful testing, before merging to `main`:

- [ ] WorkOS JWT template includes `organization_id`
- [ ] Kubernetes ConfigMap has `TENANT_CLAIM: "organization_id"`
- [ ] Helm values have `api.jwt.tenantClaim: "organization_id"`
- [ ] Flutter `oidc_config.json` has `tenantClaim: "organization_id"`
- [ ] All tests passed in staging environment
- [ ] Documented in runbook/operations guide
- [ ] Team notified of new tenant extraction behavior

---

## Debugging Tips

**If tenant is always empty:**
- Check `TENANT_CLAIM` env var is set in Go
- Check `tenantClaim` is set in Flutter config
- Verify claim exists in ID token (Test 1)
- Check for debug logs: "tenant claim not found in JWT"

**If tenant is wrong value:**
- Check precedence order (HMAC vs JWT)
- Verify claim name matches exactly (case-sensitive)
- Check for typos in config

**If HMAC headers fail:**
- Verify secret matches on both sides
- Check timestamp skew (< 5 minutes)
- Verify HMAC signature calculation

**If tests pass but production fails:**
- Environment variables might not be set
- ConfigMap might not be updated
- Pods might not have restarted after config change
