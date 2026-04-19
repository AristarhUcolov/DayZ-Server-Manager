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

	return &Server{
		app: a,
		http: &http.Server{
			Addr:              fmt.Sprintf("%s:%d", bind, port),
			Handler:           recoverer(noCache(mux)),
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
