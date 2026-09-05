package httpapi

import (
	"net/http"

	"github.com/ayitas/shardrive/apps/backend/internal/account"
	"github.com/ayitas/shardrive/apps/backend/internal/auth"
	"github.com/ayitas/shardrive/apps/backend/internal/directory"
	"github.com/ayitas/shardrive/apps/backend/internal/download"
	filedomain "github.com/ayitas/shardrive/apps/backend/internal/file"
	"github.com/ayitas/shardrive/apps/backend/internal/health"
	"github.com/ayitas/shardrive/apps/backend/internal/upload"
)

func NewRouter(healthHandler *health.Handler, uploadHandler *upload.Handler, downloadHandler *download.Handler, extras ...any) http.Handler {
	mux := http.NewServeMux()
	var sessionAuth *auth.Handler
	mux.HandleFunc("GET /health/live", healthHandler.Live)
	mux.HandleFunc("GET /health/ready", healthHandler.Ready)
	mux.HandleFunc("GET /health", healthHandler.Ready)
	if uploadHandler != nil {
		mux.HandleFunc("POST /api/v1/uploads", uploadHandler.Create)
		mux.HandleFunc("GET /api/v1/uploads/{id}", uploadHandler.Get)
		mux.HandleFunc("GET /api/v1/uploads/{id}/chunks", uploadHandler.Chunks)
		mux.HandleFunc("PUT /api/v1/uploads/{id}/chunks/{index}", uploadHandler.UploadChunk)
		mux.HandleFunc("POST /api/v1/uploads/{id}/complete", uploadHandler.Complete)
		mux.HandleFunc("DELETE /api/v1/uploads/{id}", uploadHandler.Cancel)
	}
	if downloadHandler != nil {
		mux.HandleFunc("GET /api/v1/files/{id}/download", downloadHandler.Download)
	}
	for _, extra := range extras {
		switch handler := extra.(type) {
		case *filedomain.Handler:
			if handler != nil {
				mux.HandleFunc("GET /api/v1/files", handler.List)
				mux.HandleFunc("DELETE /api/v1/files/{id}", handler.Delete)
			}
		case *directory.Handler:
			if handler != nil {
				mux.HandleFunc("GET /api/v1/directories", handler.List)
			}
		case *auth.Handler:
			if handler != nil {
				sessionAuth = handler
				mux.HandleFunc("POST /api/v1/auth/login", handler.Login)
				mux.HandleFunc("POST /api/v1/auth/logout", handler.Logout)
				mux.HandleFunc("GET /api/v1/auth/me", handler.Me)
			}
		case *account.Handler:
			if handler != nil {
				mux.HandleFunc("GET /api/v1/accounts", handler.List)
			}
		}
	}
	if sessionAuth != nil {
		return cors(sessionAuth.RequireSession(mux))
	}
	return cors(mux)
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "http://localhost:3000" || origin == "http://127.0.0.1:3000" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Add("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
