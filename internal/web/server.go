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
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"dayzmanager/internal/app"
	"dayzmanager/internal/rcon"
	"dayzmanager/internal/util"
)

type Server struct {
	app   *app.App   // the primary server: panel-wide settings live in its config
	fleet *app.Fleet // every server of the panel (multi-server)
	http  *http.Server
	h     *handlers // the primary's handlers — they also serve the panel-wide routes

	mu    sync.RWMutex
	insts map[string]*instance // by server id
	ctx   context.Context      // the serve context; servers added later run their loops in it

	httpsOn bool // the listener also speaks TLS (Settings → HTTPS); set by Serve
	port    int

	// OnServerAdded is called (by the Servers page) for a server added to the
	// panel while it runs — the launcher marks it as owned and wires its exit.
	OnServerAdded func(a *app.App)

	clone cloneJob // Servers page: "create a copy" in progress (servers.go)
}

// instance is one DayZ server's API: its handlers, routes and background
// loops (cancel stops them when the server is removed from the panel).
type instance struct {
	h      *handlers
	mux    *http.ServeMux
	cancel context.CancelFunc
}

// ServerHeader names the server a request is for (the server switcher in the
// panel); URLs a browser loads by itself (images, downloads, the log stream)
// carry it as ?srv= instead. No header = the primary server.
const ServerHeader = "X-DZM-Server"

func New(f *app.Fleet, bind string, port int) *Server {
	a := f.Primary
	s := &Server{app: a, fleet: f, port: port, insts: map[string]*instance{}}
	auth := newAuthManager(a.ManagerDir, a.Log)
	auth.cookie = cookieNameFor(port)
	s.h = &handlers{app: a, auth: auth, panelPort: port, upnp: &upnpState{}, upnpKick: make(chan struct{}, 1), server: s}
	for _, srv := range f.Apps() {
		s.attach(srv)
	}

	// 64 MB cap on request bodies; mission .zip imports get 512 MB (big
	// mapgroup files) and enforce it themselves.
	apiHandler := bodyLimit(64<<20, map[string]int64{"/api/missions/import": 512 << 20, "/api/missions/import-folder": 512 << 20})(http.HandlerFunc(s.route))

	// guard (auth_http.go) runs first: exposure, Host and cross-site checks,
	// the HTTPS redirect and the optional login.
	s.http = &http.Server{
		Addr:              fmt.Sprintf("%s:%d", bind, port),
		Handler:           recoverer(s.guard(gzipper(apiNoCache(apiHandler)))),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	return s
}

// attach builds the API of one server of the panel (and starts its loops when
// the panel is already serving).
func (s *Server) attach(a *app.App) *instance {
	h := s.h
	if a != s.app {
		h = &handlers{app: a, auth: s.h.auth, panelPort: s.port, upnp: s.h.upnp, upnpKick: s.h.upnpKick, server: s}
	}
	h.chat = newChatArchive(a.ManagerDir, func() int { return a.Cfg().ChatKeepDays }, a.Log)
	// The live chat feed survives a manager restart: refill it from the
	// archive, then archive every new line.
	a.RCon.SeedChat(h.chat.recent(200))
	a.RCon.SetChatSink(func(m rcon.ChatMsg) {
		h.chat.push(m)
		h.onChat(m)
	})
	// Connects and disconnects: Discord joins/leaves, GUID linking.
	a.RCon.SetEventSink(h.onRConEvent)
	// Let the scheduler apply a config profile during a restart's down-window.
	a.Server.ApplyProfile = func(slug string) error {
		_, _, err := h.applyProfileBySlug(slug)
		return err
	}
	mux := http.NewServeMux()
	h.register(mux)
	if a == s.app {
		sub, _ := fs.Sub(staticFS, "static")
		mux.Handle("/", etagStatic(sub, http.FileServer(http.FS(sub))))
	}
	inst := &instance{h: h, mux: mux}
	s.mu.Lock()
	s.insts[serverKey(a.ID)] = inst
	ctx := s.ctx
	s.mu.Unlock()
	if ctx != nil {
		s.startInstance(ctx, inst)
	}
	return inst
}

// detach stops serving a server removed from the panel.
func (s *Server) detach(id string) {
	s.mu.Lock()
	inst := s.insts[id]
	delete(s.insts, id)
	s.mu.Unlock()
	if inst == nil {
		return
	}
	if inst.cancel != nil {
		inst.cancel()
	}
	inst.h.app.RCon.SetChatSink(nil)
	inst.h.app.RCon.SetEventSink(nil)
}

func serverKey(id string) string {
	if id == "" {
		return app.PrimaryID
	}
	return id
}

// instanceFor picks the server a request is for.
func (s *Server) instanceFor(r *http.Request) (*instance, bool) {
	id := r.Header.Get(ServerHeader)
	if id == "" {
		id = r.URL.Query().Get("srv")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	inst, ok := s.insts[serverKey(id)]
	return inst, ok
}

// route sends a request to the API of the server it is for. Static files and
// the panel-wide endpoints (login, remote access, the server list, language
// bundles, the guide, updates, exit) always go to the primary.
func (s *Server) route(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/api/") || panelWideRoute(r.URL.Path) {
		s.primary().mux.ServeHTTP(w, r)
		return
	}
	inst, ok := s.instanceFor(r)
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"servers.unknown"}`))
		return
	}
	inst.mux.ServeHTTP(w, r)
}

func (s *Server) primary() *instance {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.insts[app.PrimaryID]
}

// panelWideRoute lists the API that belongs to the whole panel, not to one
// server.
func panelWideRoute(p string) bool {
	for _, pre := range []string{"/api/auth/", "/api/remote/", "/api/servers"} {
		if strings.HasPrefix(p, pre) {
			return true
		}
	}
	switch p {
	case "/api/i18n", "/api/changelog", "/api/guide", "/api/sponsors", "/api/update/check",
		"/api/app/exit", "/api/app/show", "/api/app/shortcuts", "/api/network/addresses", "/api/steam/detect",
		"/api/config/finish-first-run":
		return true
	}
	return false
}

// apps lists every opened server's handlers.
func (s *Server) handlersList() []*handlers {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*handlers, 0, len(s.insts))
	for _, a := range s.fleet.Apps() {
		if inst := s.insts[serverKey(a.ID)]; inst != nil {
			out = append(out, inst.h)
		}
	}
	return out
}

// Serve runs the panel on an already-open listener (the launcher opens it
// itself to detect a busy port) until ctx is cancelled.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	if s.app.Cfg().PanelHTTPS {
		if tlsConf, err := s.panelTLSConfig(); err != nil {
			s.app.Log.Printf("HTTPS disabled: %v", err)
		} else {
			ln = newSniffListener(ln, tlsConf)
			s.httpsOn = true
			s.app.Log.Printf("panel HTTPS enabled on the same port (https://…:%d/)", s.port)
		}
	}
	s.startBackground(ctx)
	err := s.http.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// HTTPS reports whether the panel listener also serves TLS.
func (s *Server) HTTPS() bool { return s.httpsOn }

// LoginEnabled reports whether the panel requires a login.
func (s *Server) LoginEnabled() bool { return s.h.auth.enabled() }

func (s *Server) startBackground(ctx context.Context) {
	s.mu.Lock()
	s.ctx = ctx
	insts := make([]*instance, 0, len(s.insts))
	for _, inst := range s.insts {
		insts = append(insts, inst)
	}
	s.mu.Unlock()
	for _, inst := range insts {
		s.startInstance(ctx, inst)
	}
	lg := s.app.Log
	go util.SafeLoop(lg, "session flush loop", func() { s.h.sessionFlushLoop(ctx) })
	// Router port forwards (Settings → Remote access), renewed while running.
	go util.SafeLoop(lg, "UPnP loop", func() { s.h.upnpLoop(ctx) })
	go func() {
		<-ctx.Done()
		sdCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.http.Shutdown(sdCtx)
	}()
}

// startInstance runs one server's background loops until ctx ends or the
// server is removed from the panel.
func (s *Server) startInstance(ctx context.Context, inst *instance) {
	ictx, cancel := context.WithCancel(ctx)
	inst.cancel = cancel
	h := inst.h
	lg := h.app.Log
	// Scheduled config backups (Settings → Automatic backups). Lives with the
	// web server because the zip layout is defined by the handlers.
	go util.SafeLoop(lg, "auto backup loop", func() { h.autoBackupLoop(ictx) })
	// 30s CPU/RAM/players sampler for the dashboard performance charts.
	go util.SafeLoop(lg, "metrics sampler", func() { h.metricsSampler(ictx) })
	// Keep the player DB ingesting ADM lines even with no page open.
	go util.SafeLoop(lg, "players ingest loop", func() { h.playersIngestLoop(ictx) })
	// Chat archive writer.
	go util.SafeLoop(lg, "chat archive writer", func() { h.chat.run(ictx) })
	// Timed bans end on time (DayZ's ban.txt has no expiry of its own).
	go util.SafeLoop(lg, "ban expiry loop", func() { h.bansExpiryLoop(ictx) })
}

func (s *Server) Start(ctx context.Context) error {
	s.startBackground(ctx)
	err := s.http.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// sessionFlushLoop persists session last-seen updates once a minute (and on
// exit) instead of on every request.
func (h *handlers) sessionFlushLoop(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			h.auth.flush()
			return
		case <-t.C:
			h.auth.flush()
		}
	}
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
func bodyLimit(max int64, overrides map[string]int64) func(http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			max := max
			if o, ok := overrides[r.URL.Path]; ok {
				max = o
			}
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
