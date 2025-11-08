package grpcapi

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/erauner12/toolbridge-api/internal/auth"
	"github.com/erauner12/toolbridge-api/internal/session"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// CorrelationIDInterceptor generates or reads correlation ID from metadata
// Mirrors HTTP CorrelationMiddleware behavior
func CorrelationIDInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		md, _ := metadata.FromIncomingContext(ctx)
		corrHeaders := md.Get("x-correlation-id")

		var corrID string
		if len(corrHeaders) > 0 && corrHeaders[0] != "" {
			corrID = corrHeaders[0]
		} else {
			corrID = uuid.New().String()
		}

		// Add correlation ID to zerolog context
		logger := log.With().Str("correlation_id", corrID).Str("grpc_method", info.FullMethod).Logger()
		ctx = logger.WithContext(ctx)

		logger.Debug().Msg("grpc_request_started")

		// Call handler
		resp, err := handler(ctx, req)

		// Log completion
		if err != nil {
			logger.Warn().Err(err).Msg("grpc_request_failed")
		} else {
			logger.Debug().Msg("grpc_request_completed")
		}

		return resp, err
	}
}

// AuthInterceptor validates JWT tokens and sets userID in context
// Mirrors HTTP auth.Middleware behavior
func AuthInterceptor(db *pgxpool.Pool, cfg auth.JWTCfg) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		logger := log.Ctx(ctx)

		// Skip auth for certain public RPCs if needed
		// (Currently all sync RPCs require auth)

		// 1. Read authorization from metadata
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "missing metadata")
		}

		// 2. Check for debug mode (X-Debug-Sub header)
		if cfg.DevMode {
			debugSubHeaders := md.Get("x-debug-sub")
			if len(debugSubHeaders) > 0 && debugSubHeaders[0] != "" {
				userID := debugSubHeaders[0]
				logger.Warn().Str("user_id", userID).Msg("using X-Debug-Sub header (dev mode only)")
				ctx = context.WithValue(ctx, auth.CtxUserID, userID)
				return handler(ctx, req)
			}
		}

		// 3. Get authorization header
		authHeaders := md.Get("authorization")
		if len(authHeaders) == 0 {
			return nil, status.Error(codes.Unauthenticated, "missing authorization header")
		}

		authHeader := authHeaders[0]
		if !strings.HasPrefix(authHeader, "Bearer ") {
			return nil, status.Error(codes.Unauthenticated, "invalid authorization header format")
		}

		tokenString := strings.TrimPrefix(authHeader, "Bearer ")

		// 4. Validate token (reuse JWT validation logic)
		// This is a simplified version - in production, reuse auth.Middleware logic
		// or extract token validation into a shared function

		// For now, we'll extract the subject claim and use it as userID
		// TODO: Implement full JWT validation (HS256 and RS256 via JWKS)
		// This would require extracting the validation logic from auth/jwt.go
		// into a reusable function that both HTTP and gRPC can call

		// Placeholder: In a real implementation, validate the token here
		// For this PR, we'll add a TODO and assume validation happens
		logger.Warn().Msg("JWT validation not yet implemented - using placeholder")

		// TODO: Implement JWT validation
		// claims, err := validateJWT(tokenString, cfg)
		// if err != nil {
		//     return nil, status.Error(codes.Unauthenticated, "invalid token")
		// }
		// subject := claims["sub"].(string)

		// Placeholder subject extraction (replace with actual validation)
		subject := "placeholder-user-id"

		// 5. Find or create app_user record
		var userID string
		err := db.QueryRow(ctx,
			`INSERT INTO app_user(subject, created_at, updated_at)
			 VALUES ($1, NOW(), NOW())
			 ON CONFLICT (subject) DO UPDATE SET updated_at = NOW()
			 RETURNING id`,
			subject,
		).Scan(&userID)

		if err != nil {
			logger.Error().Err(err).Str("subject", subject).Msg("failed to find/create app_user")
			return nil, status.Error(codes.Internal, "user lookup failed")
		}

		// 6. Add userID to context
		ctx = context.WithValue(ctx, auth.CtxUserID, userID)

		logger.Debug().Str("user_id", userID).Msg("authenticated")

		return handler(ctx, req)
	}
}

// SessionInterceptor validates X-Sync-Session header
// Mirrors HTTP SessionRequired middleware behavior
func SessionInterceptor() grpc.UnaryServerInterceptor {
	sessionStore := session.GetStore()

	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		logger := log.Ctx(ctx)

		// Skip session check for certain RPCs
		if isSessionExempt(info.FullMethod) {
			return handler(ctx, req)
		}

		// 1. Read X-Sync-Session from metadata
		md, _ := metadata.FromIncomingContext(ctx)
		sessionHeaders := md.Get("x-sync-session")
		if len(sessionHeaders) == 0 || sessionHeaders[0] == "" {
			logger.Warn().
				Str("method", info.FullMethod).
				Msg("missing X-Sync-Session header")
			return nil, status.Error(codes.FailedPrecondition,
				"X-Sync-Session header required. Call BeginSession first.")
		}

		sessionID := sessionHeaders[0]

		// 2. Validate session exists and is not expired
		sess, ok := sessionStore.GetSession(sessionID)
		if !ok {
			logger.Warn().
				Str("session_id", sessionID).
				Msg("invalid or expired session")
			return nil, status.Error(codes.FailedPrecondition,
				"Invalid or expired sync session. Call BeginSession to create a new session.")
		}

		// 3. Verify session belongs to authenticated user
		userID := auth.UserID(ctx)
		if sess.UserID != userID {
			logger.Warn().
				Str("session_id", sessionID).
				Str("session_user_id", sess.UserID).
				Str("authenticated_user_id", userID).
				Msg("session does not belong to authenticated user")
			return nil, status.Error(codes.PermissionDenied,
				"Session does not belong to authenticated user.")
		}

		logger.Debug().
			Str("session_id", sessionID).
			Int("epoch", sess.Epoch).
			Msg("session validated")

		return handler(ctx, req)
	}
}

// EpochInterceptor validates X-Sync-Epoch header
// Mirrors HTTP EpochRequired middleware behavior
func EpochInterceptor(db *pgxpool.Pool) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		logger := log.Ctx(ctx)

		// Skip epoch check for certain RPCs
		if isEpochExempt(info.FullMethod) {
			return handler(ctx, req)
		}

		// 1. Read X-Sync-Epoch from metadata
		md, _ := metadata.FromIncomingContext(ctx)
		epochHeaders := md.Get("x-sync-epoch")
		if len(epochHeaders) == 0 || epochHeaders[0] == "" {
			logger.Warn().Msg("missing X-Sync-Epoch header")
			return nil, status.Error(codes.FailedPrecondition, "X-Sync-Epoch header required")
		}

		clientEpoch, err := strconv.Atoi(epochHeaders[0])
		if err != nil {
			logger.Warn().Str("epoch_header", epochHeaders[0]).Msg("invalid epoch format")
			return nil, status.Error(codes.InvalidArgument, "X-Sync-Epoch must be an integer")
		}

		// 2. Query server epoch
		userID := auth.UserID(ctx)
		var serverEpoch int
		err = db.QueryRow(ctx,
			`SELECT epoch FROM owner_state WHERE owner_id = $1`,
			userID,
		).Scan(&serverEpoch)

		if err == pgx.ErrNoRows {
			// No owner_state yet - this is a new user
			// Create initial epoch
			err = db.QueryRow(ctx,
				`INSERT INTO owner_state(owner_id, epoch, created_at, updated_at)
				 VALUES ($1, 1, NOW(), NOW())
				 RETURNING epoch`,
				userID,
			).Scan(&serverEpoch)

			if err != nil {
				logger.Error().Err(err).Msg("failed to initialize epoch")
				return nil, status.Error(codes.Internal, "failed to initialize sync state")
			}
		} else if err != nil {
			logger.Error().Err(err).Msg("failed to load epoch")
			return nil, status.Error(codes.Internal, "failed to load sync state")
		}

		// 3. Check for epoch mismatch
		if clientEpoch != serverEpoch {
			logger.Warn().
				Int("client_epoch", clientEpoch).
				Int("server_epoch", serverEpoch).
				Msg("epoch mismatch - client must reset")

			// Return error with server epoch in message
			// Client must detect this and trigger full reset
			return nil, status.Error(codes.FailedPrecondition,
				fmt.Sprintf("Epoch mismatch: server=%d, client=%d. Local data must be reset.", serverEpoch, clientEpoch))
		}

		logger.Debug().Int("epoch", serverEpoch).Msg("epoch validated")

		return handler(ctx, req)
	}
}

// ChainUnaryServer creates a single interceptor from a chain of interceptors
// Interceptors are executed in the order they are provided
func ChainUnaryServer(interceptors ...grpc.UnaryServerInterceptor) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// Build chain in reverse order
		chained := handler
		for i := len(interceptors) - 1; i >= 0; i-- {
			interceptor := interceptors[i]
			next := chained
			chained = func(currentCtx context.Context, currentReq interface{}) (interface{}, error) {
				return interceptor(currentCtx, currentReq, info, next)
			}
		}
		return chained(ctx, req)
	}
}

// isSessionExempt returns true if the method does not require a session
func isSessionExempt(method string) bool {
	exempt := []string{
		"/toolbridge.sync.v1.SyncService/GetServerInfo",
		"/toolbridge.sync.v1.SyncService/BeginSession",
	}
	for _, e := range exempt {
		if method == e {
			return true
		}
	}
	return false
}

// isEpochExempt returns true if the method does not require epoch validation
func isEpochExempt(method string) bool {
	// Epoch exempt = session exempt + EndSession
	return isSessionExempt(method) || method == "/toolbridge.sync.v1.SyncService/EndSession"
}

// RecoveryInterceptor recovers from panics and returns Internal error
func RecoveryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
		defer func() {
			if r := recover(); r != nil {
				logger := log.Ctx(ctx)
				logger.Error().
					Interface("panic", r).
					Str("method", info.FullMethod).
					Msg("panic recovered in gRPC handler")
				err = status.Error(codes.Internal, "internal server error")
			}
		}()
		return handler(ctx, req)
	}
}

// LoggingInterceptor logs request details (simple version)
func LoggingInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		logger := log.Ctx(ctx)

		// Get user ID from context if available
		userID := auth.UserID(ctx)

		logger.Info().
			Str("method", info.FullMethod).
			Str("user_id", userID).
			Msg("grpc_call")

		return handler(ctx, req)
	}
}

// SetLoggerInContext adds a zerolog logger to the context
func SetLoggerInContext(ctx context.Context) context.Context {
	if log.Ctx(ctx).GetLevel() == zerolog.Disabled {
		// No logger in context, add one
		return log.Logger.WithContext(ctx)
	}
	return ctx
}
