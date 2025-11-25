package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/erauner12/toolbridge-api/internal/auth"
	"github.com/golang-jwt/jwt/v5"
	"github.com/rs/zerolog/log"
)

// TokenExchangeRequest represents RFC 8693 token exchange request
// https://datatracker.ietf.org/doc/html/rfc8693
type TokenExchangeRequest struct {
	GrantType        string `json:"grant_type"`
	SubjectToken     string `json:"subject_token,omitempty"`
	SubjectTokenType string `json:"subject_token_type,omitempty"`
	Audience         string `json:"audience"`
}

// TokenExchangeResponse represents RFC 8693 token exchange response
type TokenExchangeResponse struct {
	AccessToken     string `json:"access_token"`
	IssuedTokenType string `json:"issued_token_type"`
	TokenType       string `json:"token_type"`
	ExpiresIn       int    `json:"expires_in"`
}

// TokenExchange handles token exchange for MCP OAuth tokens → Backend JWTs
// This enables the MCP server to convert user OAuth tokens into backend API tokens
//
// WorkOS AuthKit OAuth 2.1 Flow:
// 1. MCP server receives user OAuth token from FastMCP AuthKitProvider
// 2. MCP calls this endpoint with the user token
// 3. Go API validates user token and issues backend JWT
// 4. Backend JWT used for subsequent API calls
//
// Implements RFC 8693 Token Exchange: https://datatracker.ietf.org/doc/html/rfc8693
func (s *Server) TokenExchange(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract incoming MCP OAuth token from Authorization header
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		writeError(w, r, http.StatusUnauthorized, "Missing Authorization header")
		return
	}

	if len(authHeader) < 8 || authHeader[:7] != "Bearer " {
		writeError(w, r, http.StatusUnauthorized, "Invalid Authorization header format")
		return
	}

	incomingToken := authHeader[7:]

	// Validate incoming MCP OAuth token
	// This extracts the user identity (sub claim) from the MCP token
	jwtCfg := s.getJWTConfig(r)
	userID, _, err := auth.ValidateToken(incomingToken, jwtCfg)
	if err != nil {
		log.Ctx(ctx).Warn().
			Err(err).
			Msg("token exchange: invalid incoming token")
		writeError(w, r, http.StatusUnauthorized, "Invalid token")
		return
	}

	// Parse request body
	var req TokenExchangeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate grant type (RFC 8693)
	expectedGrantType := "urn:ietf:params:oauth:grant-type:token-exchange"
	if req.GrantType != expectedGrantType {
		log.Ctx(ctx).Warn().
			Str("grant_type", req.GrantType).
			Msg("token exchange: invalid grant_type")
		writeError(w, r, http.StatusBadRequest,
			fmt.Sprintf("Invalid grant_type. Expected: %s", expectedGrantType))
		return
	}

	// Validate requested audience
	if req.Audience == "" {
		writeError(w, r, http.StatusBadRequest, "Missing audience")
		return
	}

	// Issue new backend JWT for requested audience
	// This JWT contains the user identity from the MCP token
	expiresIn := 3600 // 1 hour
	expiresAt := time.Now().Add(time.Duration(expiresIn) * time.Second)

	claims := jwt.MapClaims{
		"sub": userID,                        // User identity from MCP token
		"iss": "toolbridge-api",              // Backend API as issuer
		"aud": req.Audience,                  // Requested backend audience
		"exp": expiresAt.Unix(),              // Expiration time
		"iat": time.Now().Unix(),             // Issued at
		"nbf": time.Now().Unix(),             // Not before
		"token_type": "backend",              // Token type metadata
		"exchanged_from": "mcp_oauth",        // Exchange source metadata
	}

	// Sign JWT with HS256 (using shared secret)
	// In production, consider using RS256 with a dedicated signing key
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(jwtCfg.HS256Secret))
	if err != nil {
		log.Ctx(ctx).Error().
			Err(err).
			Msg("token exchange: failed to sign backend JWT")
		writeError(w, r, http.StatusInternalServerError, "Failed to issue token")
		return
	}

	log.Ctx(ctx).Info().
		Str("user_id", userID).
		Str("audience", req.Audience).
		Msg("token exchange: issued backend JWT")

	// Return RFC 8693 token exchange response
	response := TokenExchangeResponse{
		AccessToken:     tokenString,
		IssuedTokenType: "urn:ietf:params:oauth:token-type:access_token",
		TokenType:       "Bearer",
		ExpiresIn:       expiresIn,
	}

	writeJSON(w, http.StatusOK, response)
}

// getJWTConfig retrieves JWT configuration from Server struct
func (s *Server) getJWTConfig(r *http.Request) auth.JWTCfg {
	return s.JWTCfg
}
