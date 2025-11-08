package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/erauner12/toolbridge-api/internal/auth"
	"github.com/erauner12/toolbridge-api/internal/db"
	"github.com/erauner12/toolbridge-api/internal/httpapi"
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

	// HTTP server setup
	srv := &httpapi.Server{
		DB:              pool,
		RateLimitConfig: httpapi.DefaultRateLimitConfig,
	}

	// JWT configuration
	// DevMode ONLY enabled when ENV is explicitly set to "dev" (allows X-Debug-Sub header)
	// Secure by default: if ENV is unset or misspelled, DevMode stays false
	jwtSecret := env("JWT_HS256_SECRET", "dev-secret-change-in-production")
	isDevMode := env("ENV", "") == "dev"

	// Auth0 configuration for production RS256 tokens
	auth0Domain := env("AUTH0_DOMAIN", "")
	auth0Audience := env("AUTH0_AUDIENCE", "")

	// Security validation: Auth0 domain and audience must be set together
	// If only domain is set, we'd accept tokens for ANY API in the tenant (security risk)
	// If only audience is set, we'd have no JWKS to validate signatures against
	if (auth0Domain != "" && auth0Audience == "") || (auth0Domain == "" && auth0Audience != "") {
		log.Fatal().
			Str("domain", auth0Domain).
			Str("audience", auth0Audience).
			Msg("FATAL: AUTH0_DOMAIN and AUTH0_AUDIENCE must both be set or both be empty. " +
				"Setting only domain would accept tokens for any API in the tenant. " +
				"Setting only audience would have no JWKS to validate signatures.")
	}

	jwtCfg := auth.JWTCfg{
		HS256Secret:   jwtSecret,
		DevMode:       isDevMode,
		Auth0Domain:   auth0Domain,
		Auth0Audience: auth0Audience,
	}

	// Security validation: Always require a strong HS256 secret in production mode
	// This provides defense-in-depth even when Auth0 is configured, since the middleware
	// still accepts HS256 tokens. Without this check, an attacker could forge HS256 tokens
	// using the default secret and bypass Auth0 validation entirely.
	if !isDevMode {
		if jwtSecret == "" || jwtSecret == "dev-secret-change-in-production" {
			log.Fatal().
				Str("secret", jwtSecret).
				Bool("auth0_enabled", auth0Domain != "" && auth0Audience != "").
				Msg("FATAL: Cannot start in production mode with default or missing JWT_HS256_SECRET. " +
					"Even with Auth0 configured, a strong HS256 secret is required for defense-in-depth " +
					"since the middleware still accepts HS256 tokens. " +
					"Set JWT_HS256_SECRET to a secure random value (e.g., openssl rand -base64 32)")
		}
	}

	// Log authentication mode
	if auth0Domain != "" && auth0Audience != "" {
		log.Info().
			Str("domain", auth0Domain).
			Str("audience", auth0Audience).
			Msg("Auth0 RS256 authentication enabled")
	} else if !isDevMode {
		log.Info().Msg("HS256 authentication enabled (dev/testing mode)")
	}

	httpAddr := env("HTTP_ADDR", ":8081")
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

	// Graceful shutdown on SIGINT/SIGTERM
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Info().Msg("shutting down gracefully...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("HTTP server shutdown error")
	}

	log.Info().Msg("server stopped")
}
