//go:build !grpc
// +build !grpc

package main

import (
	"net/http"

	"github.com/erauner12/toolbridge-api/internal/auth"
	"github.com/erauner12/toolbridge-api/internal/httpapi"
	"github.com/jackc/pgx/v5/pgxpool"
)

// startGRPCServer is a no-op when building without the grpc tag
func startGRPCServer(pool *pgxpool.Pool, srv *httpapi.Server, jwtCfg auth.JWTCfg) {
	// No-op: gRPC server not enabled
}

// stopGRPCServer is a no-op when building without the grpc tag
func stopGRPCServer() {
	// No-op: gRPC server not enabled
}

// ServeGRPCWeb is a no-op when building without the grpc tag
func ServeGRPCWeb(w http.ResponseWriter, r *http.Request) bool {
	// No-op: gRPC server not enabled
	return false
}
