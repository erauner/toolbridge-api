package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/erauner12/toolbridge-api/internal/auth"
	"github.com/erauner12/toolbridge-api/internal/db"
	"github.com/erauner12/toolbridge-api/internal/httpapi"
	"github.com/erauner12/toolbridge-api/internal/service/syncservice"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	// Configure structured logging
	zerolog.TimeFieldFormat = time.RFC3339Nano
	log.Logger = log.With().Str("service", "toolbridge-api").Logger()

	// Pretty logging for local dev (only when explicitly set to "dev")
	if env("ENV", "") == "dev" {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: "15:04:05"})
	}

	ctx := context.Background()

	// Database connection
	pgURL := env("DATABASE_URL", "")
	if pgURL == "" {
		log.Fatal().Msg("DATABASE_URL is required")
	}

	pool, err := db.Open(ctx, pgURL)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to postgres")
	}
	defer pool.Close()

	// JWT configuration
	// DevMode ONLY enabled when ENV is explicitly set to "dev" (allows X-Debug-Sub header)
	// Secure by default: if ENV is unset or misspelled, DevMode stays false
	jwtSecret := env("JWT_HS256_SECRET", "dev-secret-change-in-production")
	isDevMode := env("ENV", "") == "dev"

	// Generic OIDC provider configuration for production RS256 tokens
	// Supports any OIDC provider (WorkOS AuthKit, Auth0, Okta, etc.)
	jwtIssuer := env("JWT_ISSUER", "")
	jwksURL := env("JWT_JWKS_URL", "")
	jwtAudience := env("JWT_AUDIENCE", "")

	// Security validation: JWKS URL and issuer must be set together
	// If only JWKS is set, we'd accept tokens from any issuer using those keys (security risk)
	// If only issuer is set, we'd have no JWKS to validate signatures against
	if (jwksURL != "" && jwtIssuer == "") || (jwksURL == "" && jwtIssuer != "") {
		log.Fatal().
			Str("issuer", jwtIssuer).
			Str("jwks_url", jwksURL).
			Msg("FATAL: JWT_ISSUER and JWT_JWKS_URL must both be set or both be empty. " +
				"Setting only JWKS would accept tokens from any issuer. " +
				"Setting only issuer would have no JWKS to validate signatures.")
	}

	// Additional accepted audiences (for MCP OAuth tokens, token exchange, etc.)
	// These are in addition to the primary JWT_AUDIENCE
	//
	// WorkOS AuthKit with DCR: Leave empty (MCP_OAUTH_AUDIENCE="") to skip audience validation
	// Static registration: MUST equal the `resource` value from the MCP server's
	// /.well-known/oauth-protected-resource metadata endpoint (path-based discovery).
	// Example: https://toolbridge-mcp-staging.fly.dev/mcp
	acceptedAudiences := []string{}
	if mcpAudience := strings.TrimSpace(env("MCP_OAUTH_AUDIENCE", "")); mcpAudience != "" {
		acceptedAudiences = append(acceptedAudiences, mcpAudience)
		log.Info().Str("mcp_audience", mcpAudience).Msg("MCP OAuth audience accepted")
	}

	jwtCfg := auth.JWTCfg{
		HS256Secret:       jwtSecret,
		DevMode:           isDevMode,
		Issuer:            jwtIssuer,
		JWKSURL:           jwksURL,
		Audience:          jwtAudience,
		AcceptedAudiences: acceptedAudiences,
	}

	// HTTP server setup
	srv := &httpapi.Server{
		DB:              pool,
		RateLimitConfig: httpapi.DefaultRateLimitConfig,
		JWTCfg:          jwtCfg,
		// Initialize services
		NoteSvc:        syncservice.NewNoteService(pool),
		TaskSvc:        syncservice.NewTaskService(pool),
		CommentSvc:     syncservice.NewCommentService(pool),
		ChatSvc:        syncservice.NewChatService(pool),
		ChatMessageSvc: syncservice.NewChatMessageService(pool),
	}

	// Security validation: Always require a strong HS256 secret in production mode
	// This provides defense-in-depth even when upstream OIDC is configured, since the middleware
	// still accepts HS256 tokens. Without this check, an attacker could forge HS256 tokens
	// using the default secret and bypass upstream validation entirely.
	if !isDevMode {
		if jwtSecret == "" || jwtSecret == "dev-secret-change-in-production" {
			log.Fatal().
				Str("secret", jwtSecret).
				Bool("oidc_enabled", jwtIssuer != "" && jwksURL != "").
				Msg("FATAL: Cannot start in production mode with default or missing JWT_HS256_SECRET. " +
					"Even with upstream OIDC configured, a strong HS256 secret is required for defense-in-depth " +
					"since the middleware still accepts HS256 tokens. " +
					"Set JWT_HS256_SECRET to a secure random value (e.g., openssl rand -base64 32)")
		}
	}

	// Initialize upstream IdP JWKS cache (shared by both HTTP and gRPC)
	// Must be called before starting servers to ensure gRPC interceptors can validate tokens
	if err := auth.InitJWKSCache(jwtCfg); err != nil {
		log.Warn().Err(err).Msg("failed to pre-fetch JWKS (will retry on first request)")
	}

	// Log authentication mode
	if jwtIssuer != "" && jwksURL != "" {
		log.Info().
			Str("issuer", jwtIssuer).
			Str("jwks_url", jwksURL).
			Str("audience", jwtAudience).
			Msg("Upstream OIDC RS256 authentication enabled")

		// Security warning if audience is not configured
		if jwtAudience == "" && len(acceptedAudiences) == 0 {
			log.Warn().
				Msg("SECURITY WARNING: Upstream OIDC configured without audience validation. " +
					"This accepts tokens from ANY client in the issuer's tenant. " +
					"Set JWT_AUDIENCE or MCP_OAUTH_AUDIENCE to restrict token acceptance.")
		}

		// Informational warning if MCP audience might be needed
		if len(acceptedAudiences) == 0 && jwtAudience != "" {
			log.Info().
				Msg("MCP_OAUTH_AUDIENCE not set; MCP-issued tokens will only be accepted " +
					"if their audience matches JWT_AUDIENCE. " +
					"Set MCP_OAUTH_AUDIENCE to the MCP server's resource URL " +
					"(from /.well-known/oauth-protected-resource) if using MCP integration.")
		}
	} else if !isDevMode {
		log.Info().Msg("HS256-only authentication enabled (no upstream OIDC configured)")
	}

	httpAddr := env("HTTP_ADDR", ":8080")
	httpServer := &http.Server{
		Addr:         httpAddr,
		Handler:      srv.Routes(jwtCfg),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start server in goroutine
	go func() {
		log.Info().Str("addr", httpAddr).Msg("starting HTTP server")
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("HTTP server failed")
		}
	}()

	// ===================================================================
	// gRPC Server Setup (Conditionally compiled with -tags grpc)
	// ===================================================================
	// gRPC server is started in grpc_setup.go when building with -tags grpc
	startGRPCServer(pool, srv, jwtCfg) // No-op without grpc tag
	// ===================================================================

	// Graceful shutdown on SIGINT/SIGTERM
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Info().Msg("shutting down gracefully...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Shutdown HTTP server
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("HTTP server shutdown error")
	}

	// Shutdown gRPC server (no-op without grpc tag)
	stopGRPCServer()

	log.Info().Msg("server stopped")
}
