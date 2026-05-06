// Copyright (c) 2026 Aristarh Ucolov.
package web

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"time"

	"dayzmanager/internal/app"
)

type Server struct {
	app  *app.App
	http *http.Server
}

func New(a *app.App, bind string, port int) *Server {
	mux := http.NewServeMux()
	h := &handlers{app: a}
	h.register(mux)

	sub, _ := fs.Sub(staticFS, "static")
	mux.Handle("/", http.FileServer(http.FS(sub)))

	// v0.10.0 dropped panel auth — the manager is meant to be reachable
	// from a trusted local network only. Operators who need access control
	// in front of it should put a reverse proxy (Caddy, nginx) on top.
	apiHandler := bodyLimit(64<<20)(mux) // 64 MB cap on request bodies

	return &Server{
		app: a,
		http: &http.Server{
			Addr:              fmt.Sprintf("%s:%d", bind, port),
			Handler:           recoverer(noCache(apiHandler)),
			ReadHeaderTimeout: 10 * time.Second,
		},
	}
}

func (s *Server) Start(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		sdCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.http.Shutdown(sdCtx)
	}()
	err := s.http.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) Stop(ctx context.Context) error { return s.http.Shutdown(ctx) }

// ---------------------------------------------------------------------------

func noCache(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		h.ServeHTTP(w, r)
	})
}

// bodyLimit caps request bodies. A misbehaving or hostile client can't send
// gigabytes to /api/files/write or /api/types/item. Applies to the whole API
// surface — 64 MB is far more than any sane types.xml or custom file.
func bodyLimit(max int64) func(http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > max {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, max)
			h.ServeHTTP(w, r)
		})
	}
}

func recoverer(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				http.Error(w, fmt.Sprintf("panic: %v", rec), http.StatusInternalServerError)
			}
		}()
		h.ServeHTTP(w, r)
	})
}
