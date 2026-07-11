// Copyright (c) 2026 Aristarh Ucolov.
package web

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"time"

	"dayzmanager/internal/app"
)

type Server struct {
	app  *app.App
	http *http.Server
	h    *handlers
}

func New(a *app.App, bind string, port int) *Server {
	mux := http.NewServeMux()
	h := &handlers{app: a}
	h.register(mux)

	sub, _ := fs.Sub(staticFS, "static")
	mux.Handle("/", etagStatic(sub, http.FileServer(http.FS(sub))))

	// v0.10.0 dropped panel auth — the manager is meant to be reachable
	// from a trusted local network only. Operators who need access control
	// in front of it should put a reverse proxy (Caddy, nginx) on top.
	apiHandler := bodyLimit(64<<20)(mux) // 64 MB cap on request bodies

	return &Server{
		app: a,
		h:   h,
		http: &http.Server{
			Addr:              fmt.Sprintf("%s:%d", bind, port),
			Handler:           recoverer(gzipper(apiNoCache(apiHandler))),
			ReadHeaderTimeout: 10 * time.Second,
		},
	}
}

func (s *Server) Start(ctx context.Context) error {
	// Scheduled config backups (Settings → Automatic backups). Lives with the
	// web server because the zip layout is defined by the handlers.
	go s.h.autoBackupLoop(ctx)
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

// apiNoCache marks only /api/ responses as uncacheable. Static assets get
// validators from etagStatic instead — no-store on app.js/app.css/CodeMirror
// (~700 KB) made every page load re-download everything, which is very
// noticeable on the phone-over-LAN use this panel targets.
func apiNoCache(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		h.ServeHTTP(w, r)
	})
}

// etagStatic serves embedded static files with a content-hash ETag so browsers
// revalidate with a cheap 304 instead of re-downloading. embed.FS files have a
// zero modtime, which disables Last-Modified — a hash validator is the only
// one available. Hashes are computed lazily once per path.
func etagStatic(files fs.FS, next http.Handler) http.Handler {
	var mu sync.Mutex
	etags := map[string]string{}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		mu.Lock()
		tag, ok := etags[path]
		mu.Unlock()
		if !ok {
			if data, err := fs.ReadFile(files, path); err == nil {
				sum := fnv.New64a()
				_, _ = sum.Write(data)
				tag = fmt.Sprintf(`"%x"`, sum.Sum64())
				mu.Lock()
				etags[path] = tag
				mu.Unlock()
			}
		}
		if tag != "" {
			w.Header().Set("ETag", tag)
			w.Header().Set("Cache-Control", "no-cache") // always revalidate, 304 when unchanged
			if r.Header.Get("If-None-Match") == tag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// gzipper compresses responses when the client accepts it. Types/events JSON
// (multi-MB on big files) and the i18n bundle shrink 5-10x. SSE streams are
// skipped — compression would buffer the live tail; the zip export is already
// compressed.
func gzipper(h http.Handler) http.Handler {
	pool := sync.Pool{New: func() interface{} { return gzip.NewWriter(io.Discard) }}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") ||
			strings.HasPrefix(r.URL.Path, "/api/logs/stream") ||
			strings.HasPrefix(r.URL.Path, "/api/backup/export") {
			h.ServeHTTP(w, r)
			return
		}
		gz := pool.Get().(*gzip.Writer)
		defer pool.Put(gz)
		gz.Reset(w)
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		gw := &gzipResponseWriter{ResponseWriter: w, gz: gz}
		h.ServeHTTP(gw, r)
		// Close (which writes the gzip footer) only when compressed data
		// actually flowed — a bare 304/204 must stay body-less.
		if gw.used {
			_ = gz.Close()
		}
	})
}

type gzipResponseWriter struct {
	http.ResponseWriter
	gz   *gzip.Writer
	skip bool // 204/304 — never compress
	used bool // at least one compressed Write happened
}

func (g *gzipResponseWriter) WriteHeader(code int) {
	if code == http.StatusNoContent || code == http.StatusNotModified {
		g.skip = true
		g.Header().Del("Content-Encoding")
	}
	g.ResponseWriter.WriteHeader(code)
}

func (g *gzipResponseWriter) Write(b []byte) (int, error) {
	if g.skip {
		return g.ResponseWriter.Write(b)
	}
	// Content-Length would be wrong after compression.
	g.Header().Del("Content-Length")
	g.used = true
	return g.gz.Write(b)
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
