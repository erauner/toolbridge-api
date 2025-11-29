//go:build grpc
// +build grpc

package main

import (
	"net"

	"github.com/erauner12/toolbridge-api/internal/auth"
	"github.com/erauner12/toolbridge-api/internal/grpcapi"
	"github.com/erauner12/toolbridge-api/internal/httpapi"
	syncv1 "github.com/erauner12/toolbridge-api/gen/go/sync/v1"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var grpcServerInstance *grpc.Server

// startGRPCServer initializes and starts the gRPC server
func startGRPCServer(pool *pgxpool.Pool, srv *httpapi.Server, jwtCfg auth.JWTCfg) {
	grpcAddr := env("GRPC_ADDR", ":8082")
	lis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to listen for gRPC")
	}

	// Chain interceptors (executed in order)
	grpcServerInstance = grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			grpcapi.RecoveryInterceptor(),         // Recover from panics
			grpcapi.CorrelationIDInterceptor(),    // Add correlation ID
			grpcapi.LoggingInterceptor(),          // Log requests
			grpcapi.AuthInterceptor(pool, jwtCfg), // Validate JWT
			grpcapi.SessionInterceptor(),          // Validate session
			grpcapi.EpochInterceptor(pool),        // Validate epoch
		),
	)

	// Create main gRPC server with all services
	grpcApiServer := grpcapi.NewServer(
		pool,
		srv.NoteSvc,
		srv.TaskSvc,
		srv.CommentSvc,
		srv.ChatSvc,
		srv.ChatMessageSvc,
		srv.TaskListSvc,
		srv.TaskListCategorySvc,
	)

	// Register core sync service (sessions, info, wipe, state)
	syncv1.RegisterSyncServiceServer(grpcServerInstance, grpcApiServer)

	// Register entity sync services using wrappers
	syncv1.RegisterNoteSyncServiceServer(grpcServerInstance, grpcApiServer)
	syncv1.RegisterTaskSyncServiceServer(grpcServerInstance, &grpcapi.TaskServer{Server: grpcApiServer})
	syncv1.RegisterCommentSyncServiceServer(grpcServerInstance, &grpcapi.CommentServer{Server: grpcApiServer})
	syncv1.RegisterChatSyncServiceServer(grpcServerInstance, &grpcapi.ChatServer{Server: grpcApiServer})
	syncv1.RegisterChatMessageSyncServiceServer(grpcServerInstance, &grpcapi.ChatMessageServer{Server: grpcApiServer})
	syncv1.RegisterTaskListSyncServiceServer(grpcServerInstance, &grpcapi.TaskListServer{Server: grpcApiServer})
	syncv1.RegisterTaskListCategorySyncServiceServer(grpcServerInstance, &grpcapi.TaskListCategoryServer{Server: grpcApiServer})

	reflection.Register(grpcServerInstance) // Enable reflection for grpcurl testing

	// Start gRPC server in goroutine
	go func() {
		log.Info().Str("addr", grpcAddr).Msg("starting gRPC server")
		if err := grpcServerInstance.Serve(lis); err != nil {
			log.Fatal().Err(err).Msg("gRPC server failed")
		}
	}()

	log.Info().Msg("Both HTTP (REST) and gRPC servers running in parallel")
}

// stopGRPCServer gracefully stops the gRPC server
func stopGRPCServer() {
	if grpcServerInstance != nil {
		grpcServerInstance.GracefulStop()
		log.Info().Msg("gRPC server stopped")
	}
}
