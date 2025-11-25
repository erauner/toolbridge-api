//go:build ignore

package main

// This test proves that standard OIDC tokens from WorkOS AuthKit do NOT contain
// an organization_id claim, validating the architectural decision to use backend
// tenant resolution.
//
// PURPOSE:
//   - Demonstrate that standard OIDC/PKCE authentication flow does not include org_id
//   - Validate that client-side JWT inspection cannot determine tenant ID
//   - Prove the necessity of backend-driven tenant resolution via WorkOS API
//
// WHAT IT DOES:
//   1. Performs standard OIDC authentication with PKCE (same flow as Flutter client)
//   2. Obtains ID token and access token from token exchange
//   3. Decodes both tokens and inspects all claims
//   4. Explicitly checks for presence of organization_id claim
//   5. Reports FAIL when organization_id is missing (which is expected and correct)
//
// EXPECTED RESULT:
//   The test will show "FAIL: No organization_id claim found" for both tokens.
//   This is the CORRECT and EXPECTED behavior that validates why we need
//   backend tenant resolution.
//
// WHY THIS MATTERS:
//   WorkOS AuthKit uses standard OIDC flows that return JWT tokens conforming to
//   the OIDC spec. The organization_id is WorkOS-specific metadata that isn't
//   part of standard claims (sub, iss, aud, exp, etc.). This test proves that
//   public clients (Flutter, Python MCP) cannot determine tenant ID from tokens
//   alone and must call the backend /v1/auth/tenant endpoint.
//
// USAGE:
//   go run test/manual/prove_oidc_tokens_lack_org_id.go
//
// SEE ALSO:
//   - test_full_tenant_flow.go - Complete reference implementation showing the
//     correct way to resolve tenant ID via backend API
//   - docs/tenant-resolution.md - Full architectural documentation

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	// Standard OIDC endpoints (not User Management API)
	// Using the same client ID as Flutter (from oidc_config.json)
	issuerURL      = "https://svelte-monolith-27-staging.authkit.app"
	clientID       = "client_01KAPCBQNQBWMZE9WNSEWY2J3Z" // Same as Flutter's client
	redirectURI    = "http://localhost:3000/callback"     // Use localhost for easy testing
	organizationID = "org_01KABXHNF45RMV9KBWF3SBGPP0"
)

// OIDCDiscovery represents the OIDC discovery document
type OIDCDiscovery struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
}

// TokenResponse represents the token response from the token endpoint
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	IDToken      string `json:"id_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

func main() {
	fmt.Println("=== Standard OIDC Tenant Extraction Test ===")
	fmt.Println("(Using standard /oauth2/authorize and /oauth2/token endpoints)")
	fmt.Println()

	// Step 1: Discover OIDC endpoints
	fmt.Println("1. Discovering OIDC endpoints...")
	discovery, err := discoverOIDC()
	if err != nil {
		fmt.Printf("Error discovering OIDC endpoints: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("   Authorization endpoint: %s\n", discovery.AuthorizationEndpoint)
	fmt.Printf("   Token endpoint: %s\n", discovery.TokenEndpoint)
	fmt.Println()

	// Step 2: Generate PKCE parameters
	fmt.Println("2. Generating PKCE parameters...")
	codeVerifier := generateCodeVerifier()
	codeChallenge := generateCodeChallenge(codeVerifier)
	state := generateRandomString(32)
	fmt.Printf("   Code verifier: %s\n", codeVerifier[:20]+"...")
	fmt.Printf("   Code challenge: %s\n", codeChallenge[:20]+"...")
	fmt.Printf("   State: %s\n", state[:20]+"...")
	fmt.Println()

	// Step 3: Build authorization URL
	fmt.Println("3. Building authorization URL...")
	authURL := buildAuthorizationURL(discovery.AuthorizationEndpoint, codeChallenge, state)
	fmt.Printf("   URL: %s\n", authURL)
	fmt.Println()

	// Step 4: Start HTTP server and open browser
	fmt.Println("4. Starting local server and opening browser...")
	code, err := captureAuthorizationCode(authURL, state)
	if err != nil {
		fmt.Printf("Error capturing authorization code: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("   ✓ Authorization code received: %s...\n", code[:20])
	fmt.Println()

	// Step 5: Exchange code for tokens
	fmt.Println("5. Exchanging authorization code for tokens...")
	tokenResp, err := exchangeCodeForTokens(discovery.TokenEndpoint, code, codeVerifier)
	if err != nil {
		fmt.Printf("Error exchanging code: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("   ✓ Token exchange successful")
	fmt.Println()

	// Print ID token for testing backend endpoint
	if tokenResp.IDToken != "" {
		fmt.Println("📋 ID Token (for testing /v1/auth/tenant endpoint):")
		fmt.Printf("export ID_TOKEN='%s'\n", tokenResp.IDToken)
		fmt.Println()
	}

	// Step 6: Decode and inspect tokens
	fmt.Println("6. Inspecting tokens for organization_id claim...")
	fmt.Println()

	// Check access token
	fmt.Println("--- Access Token ---")
	if tokenResp.AccessToken != "" {
		claims, err := decodeJWT(tokenResp.AccessToken)
		if err != nil {
			fmt.Printf("Error decoding access token: %v\n", err)
		} else {
			printClaims(claims)
			if orgID, ok := claims["organization_id"].(string); ok && orgID != "" {
				fmt.Printf("\n✓ SUCCESS: Found organization_id in access token: %s\n", orgID)
			} else {
				fmt.Println("\n✗ FAIL: No organization_id claim found in access token")
			}
		}
	} else {
		fmt.Println("No access token returned")
	}
	fmt.Println()

	// Check ID token
	fmt.Println("--- ID Token ---")
	if tokenResp.IDToken != "" {
		claims, err := decodeJWT(tokenResp.IDToken)
		if err != nil {
			fmt.Printf("Error decoding ID token: %v\n", err)
		} else {
			printClaims(claims)
			if orgID, ok := claims["organization_id"].(string); ok && orgID != "" {
				fmt.Printf("\n✓ SUCCESS: Found organization_id in ID token: %s\n", orgID)
			} else {
				fmt.Println("\n✗ FAIL: No organization_id claim found in ID token")
			}
		}
	} else {
		fmt.Println("No ID token returned")
	}
	fmt.Println()

	fmt.Println("=== Test Complete ===")
}

func captureAuthorizationCode(authURL, expectedState string) (string, error) {
	// Create a channel to receive the authorization code
	codeChan := make(chan string, 1)
	errChan := make(chan error, 1)

	// Create HTTP server
	srv := &http.Server{Addr: ":3000"}

	http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		state := r.URL.Query().Get("state")
		errorParam := r.URL.Query().Get("error")
		errorDesc := r.URL.Query().Get("error_description")

		// Send success response to browser
		w.Header().Set("Content-Type", "text/html")
		if errorParam != "" {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "<html><body><h1>Authorization Failed</h1><p>%s: %s</p><p>You can close this window.</p></body></html>", errorParam, errorDesc)
			errChan <- fmt.Errorf("authorization error: %s - %s", errorParam, errorDesc)
		} else if code == "" {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "<html><body><h1>Authorization Failed</h1><p>No code received</p><p>You can close this window.</p></body></html>")
			errChan <- fmt.Errorf("no authorization code received")
		} else if state != expectedState {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "<html><body><h1>Authorization Failed</h1><p>State mismatch</p><p>You can close this window.</p></body></html>")
			errChan <- fmt.Errorf("state mismatch")
		} else {
			fmt.Fprintf(w, "<html><body><h1>Authorization Successful!</h1><p>You can close this window.</p></body></html>")
			codeChan <- code
		}
	})

	// Start server in background
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- fmt.Errorf("server error: %w", err)
		}
	}()

	// Give server a moment to start
	time.Sleep(100 * time.Millisecond)

	// Open browser
	if err := openBrowser(authURL); err != nil {
		srv.Shutdown(context.Background())
		return "", fmt.Errorf("failed to open browser: %w", err)
	}

	fmt.Println("   Browser opened. Waiting for authorization...")

	// Wait for code or error with timeout
	select {
	case code := <-codeChan:
		srv.Shutdown(context.Background())
		return code, nil
	case err := <-errChan:
		srv.Shutdown(context.Background())
		return "", err
	case <-time.After(5 * time.Minute):
		srv.Shutdown(context.Background())
		return "", fmt.Errorf("timeout waiting for authorization")
	}
}

func openBrowser(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{url}
	case "linux":
		cmd = "xdg-open"
		args = []string{url}
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	default:
		return fmt.Errorf("unsupported platform")
	}

	return exec.Command(cmd, args...).Start()
}

func discoverOIDC() (*OIDCDiscovery, error) {
	discoveryURL := issuerURL + "/.well-known/openid-configuration"
	resp, err := http.Get(discoveryURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("discovery failed with status %d", resp.StatusCode)
	}

	var discovery OIDCDiscovery
	if err := json.NewDecoder(resp.Body).Decode(&discovery); err != nil {
		return nil, err
	}

	return &discovery, nil
}

func buildAuthorizationURL(endpoint, codeChallenge, state string) string {
	params := url.Values{}
	params.Set("client_id", clientID)
	params.Set("redirect_uri", redirectURI)
	params.Set("response_type", "code")
	params.Set("scope", "openid profile email")
	params.Set("state", state)
	params.Set("code_challenge", codeChallenge)
	params.Set("code_challenge_method", "S256")
	params.Set("organization_id", organizationID) // Include organization_id parameter

	return endpoint + "?" + params.Encode()
}

func exchangeCodeForTokens(endpoint, code, codeVerifier string) (*TokenResponse, error) {
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("client_id", clientID)
	data.Set("code", code)
	data.Set("redirect_uri", redirectURI)
	data.Set("code_verifier", codeVerifier)

	resp, err := http.PostForm(endpoint, data)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("token exchange failed with status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, err
	}

	return &tokenResp, nil
}

func decodeJWT(tokenString string) (jwt.MapClaims, error) {
	// Parse without verification (we just want to inspect claims)
	token, _, err := new(jwt.Parser).ParseUnverified(tokenString, jwt.MapClaims{})
	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(jwt.MapClaims); ok {
		return claims, nil
	}

	return nil, fmt.Errorf("failed to extract claims")
}

func printClaims(claims jwt.MapClaims) {
	claimsJSON, _ := json.MarshalIndent(claims, "   ", "  ")
	fmt.Printf("   Claims: %s\n", string(claimsJSON))
}

func generateCodeVerifier() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func generateCodeChallenge(verifier string) string {
	h := sha256.New()
	h.Write([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

func generateRandomString(length int) string {
	b := make([]byte, length)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)[:length]
}
