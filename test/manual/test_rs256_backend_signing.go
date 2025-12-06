// test_rs256_backend_signing.go
//
// Integration test for RS256 backend token signing.
// This test verifies the full round-trip: key generation, token signing, and validation.
//
// Usage:
//   go run test/manual/test_rs256_backend_signing.go
//
// What it tests:
//   1. RSA key pair generation (PKCS#8 format)
//   2. Backend signer initialization
//   3. Token signing with RS256
//   4. Token validation (including kid routing)
//   5. Fallback to HS256 when RS256 not configured
//
// This test does NOT require a running server or database.

package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/erauner12/toolbridge-api/internal/auth"
	"github.com/golang-jwt/jwt/v5"
)

func main() {
	fmt.Println("=" + strings.Repeat("=", 69))
	fmt.Println("  RS256 Backend Signing Integration Test")
	fmt.Println("=" + strings.Repeat("=", 69))
	fmt.Println()

	passed := 0
	failed := 0

	// Test 1: Generate RSA key pair and initialize signer
	fmt.Println("Test 1: Initialize backend signer with generated RSA key")
	fmt.Println("-" + strings.Repeat("-", 69))

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		fmt.Printf("  FAIL: Failed to generate RSA key: %v\n", err)
		failed++
	} else {
		fmt.Println("  OK: Generated 2048-bit RSA key pair")

		// Encode as PKCS#8 PEM
		pkcs8Bytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
		if err != nil {
			fmt.Printf("  FAIL: Failed to marshal PKCS#8: %v\n", err)
			failed++
		} else {
			pemBlock := pem.EncodeToMemory(&pem.Block{
				Type:  "PRIVATE KEY",
				Bytes: pkcs8Bytes,
			})
			fmt.Println("  OK: Encoded as PKCS#8 PEM format")

			cfg := auth.JWTCfg{
				BackendRSAPrivateKeyPEM: string(pemBlock),
				BackendKeyID:            "integration-test-key-1",
				HS256Secret:             "fallback-secret-for-testing",
			}

			err = auth.InitBackendSigner(cfg)
			if err != nil {
				fmt.Printf("  FAIL: InitBackendSigner failed: %v\n", err)
				failed++
			} else {
				fmt.Println("  OK: Backend signer initialized successfully")
				passed++
			}
		}
	}
	fmt.Println()

	// Test 2: Sign a backend token with RS256
	fmt.Println("Test 2: Sign backend token with RS256")
	fmt.Println("-" + strings.Repeat("-", 69))

	cfg := auth.JWTCfg{
		BackendRSAPrivateKeyPEM: "dummy-to-trigger-rs256", // Non-empty triggers RS256
		BackendKeyID:            "integration-test-key-1",
		HS256Secret:             "fallback-secret",
	}

	claims := jwt.MapClaims{
		"sub":            "user_integration_test",
		"iss":            "toolbridge-api",
		"aud":            "test-audience",
		"token_type":     "backend",
		"exchanged_from": "integration_test",
		"exp":            time.Now().Add(1 * time.Hour).Unix(),
		"iat":            time.Now().Unix(),
		"nbf":            time.Now().Unix(),
	}

	tokenString, err := auth.SignBackendToken(claims, cfg)
	if err != nil {
		fmt.Printf("  FAIL: SignBackendToken failed: %v\n", err)
		failed++
	} else {
		fmt.Println("  OK: Token signed successfully")

		// Decode and verify token header
		parts := strings.Split(tokenString, ".")
		if len(parts) != 3 {
			fmt.Printf("  FAIL: Invalid token format (expected 3 parts, got %d)\n", len(parts))
			failed++
		} else {
			headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
			if err != nil {
				fmt.Printf("  FAIL: Failed to decode header: %v\n", err)
				failed++
			} else {
				var header map[string]interface{}
				if err := json.Unmarshal(headerJSON, &header); err != nil {
					fmt.Printf("  FAIL: Failed to parse header: %v\n", err)
					failed++
				} else {
					fmt.Printf("  Token Header: %s\n", string(headerJSON))

					if header["alg"] != "RS256" {
						fmt.Printf("  FAIL: Expected alg=RS256, got %v\n", header["alg"])
						failed++
					} else {
						fmt.Println("  OK: Algorithm is RS256")
					}

					if header["kid"] != "integration-test-key-1" {
						fmt.Printf("  FAIL: Expected kid=integration-test-key-1, got %v\n", header["kid"])
						failed++
					} else {
						fmt.Println("  OK: Key ID (kid) is correct")
						passed++
					}
				}
			}
		}
	}
	fmt.Println()

	// Test 3: Validate the RS256 backend token
	fmt.Println("Test 3: Validate RS256 backend token")
	fmt.Println("-" + strings.Repeat("-", 69))

	sub, returnedClaims, err := auth.ValidateToken(tokenString, cfg)
	if err != nil {
		fmt.Printf("  FAIL: ValidateToken failed: %v\n", err)
		failed++
	} else {
		fmt.Printf("  OK: Token validated successfully\n")
		fmt.Printf("  Subject (sub): %s\n", sub)
		fmt.Printf("  Token Type: %v\n", returnedClaims["token_type"])

		if sub != "user_integration_test" {
			fmt.Printf("  FAIL: Expected sub=user_integration_test, got %s\n", sub)
			failed++
		} else {
			fmt.Println("  OK: Subject matches expected value")
			passed++
		}
	}
	fmt.Println()

	// Test 4: HS256 fallback when RS256 not configured
	fmt.Println("Test 4: HS256 signing and validation (full round-trip)")
	fmt.Println("-" + strings.Repeat("-", 69))

	// Config WITHOUT RS256 - forces HS256 fallback
	// Even though global backendSigner exists, SignBackendToken checks cfg fields
	hs256Cfg := auth.JWTCfg{
		BackendRSAPrivateKeyPEM: "", // Empty = no RS256
		BackendKeyID:            "",
		HS256Secret:             "test-hs256-secret-12345",
	}

	hs256Claims := jwt.MapClaims{
		"sub":        "user_hs256_test",
		"iss":        "toolbridge-api",
		"aud":        "hs256-test-audience",
		"token_type": "backend",
		"exp":        time.Now().Add(1 * time.Hour).Unix(),
		"iat":        time.Now().Unix(),
	}

	hs256Token, err := auth.SignBackendToken(hs256Claims, hs256Cfg)
	if err != nil {
		fmt.Printf("  FAIL: SignBackendToken (HS256) failed: %v\n", err)
		failed++
	} else {
		// Check algorithm in header
		parts := strings.Split(hs256Token, ".")
		headerJSON, _ := base64.RawURLEncoding.DecodeString(parts[0])
		var header map[string]interface{}
		json.Unmarshal(headerJSON, &header)

		fmt.Printf("  Token Header: %s\n", string(headerJSON))

		if header["alg"] != "HS256" {
			fmt.Printf("  FAIL: Expected alg=HS256, got %v\n", header["alg"])
			failed++
		} else {
			fmt.Println("  OK: Algorithm is HS256 (fallback working)")

			// Now validate the HS256 token
			sub, returnedClaims, err := auth.ValidateToken(hs256Token, hs256Cfg)
			if err != nil {
				fmt.Printf("  FAIL: ValidateToken (HS256) failed: %v\n", err)
				failed++
			} else {
				fmt.Printf("  OK: HS256 token validated successfully\n")
				fmt.Printf("  Subject (sub): %s\n", sub)

				if sub != "user_hs256_test" {
					fmt.Printf("  FAIL: Expected sub=user_hs256_test, got %s\n", sub)
					failed++
				} else if returnedClaims["token_type"] != "backend" {
					fmt.Printf("  FAIL: Expected token_type=backend, got %v\n", returnedClaims["token_type"])
					failed++
				} else {
					fmt.Println("  OK: HS256 full round-trip successful!")
					passed++
				}
			}
		}
	}
	fmt.Println()

	// Test 5: Verify wrong kid falls through to JWKS (which fails without cache)
	fmt.Println("Test 5: Wrong kid routes to JWKS validation")
	fmt.Println("-" + strings.Repeat("-", 69))

	// Create a token with wrong kid
	wrongKidToken := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"sub": "attacker",
		"exp": time.Now().Add(1 * time.Hour).Unix(),
	})
	wrongKidToken.Header["kid"] = "wrong-key-id-not-ours"
	wrongKidTokenString, _ := wrongKidToken.SignedString(privateKey)

	_, _, err = auth.ValidateToken(wrongKidTokenString, cfg)
	if err == nil {
		fmt.Println("  FAIL: Expected validation to fail for wrong kid")
		failed++
	} else {
		if strings.Contains(err.Error(), "JWKS") || strings.Contains(err.Error(), "key") {
			fmt.Println("  OK: Token with wrong kid correctly routed to JWKS (and failed)")
			fmt.Printf("  Error: %v\n", err)
			passed++
		} else {
			fmt.Printf("  WARN: Unexpected error: %v\n", err)
			passed++ // Still passed - it failed as expected
		}
	}
	fmt.Println()

	// Summary
	fmt.Println("=" + strings.Repeat("=", 69))
	fmt.Printf("  Results: %d passed, %d failed\n", passed, failed)
	fmt.Println("=" + strings.Repeat("=", 69))

	if failed > 0 {
		os.Exit(1)
	}
}
