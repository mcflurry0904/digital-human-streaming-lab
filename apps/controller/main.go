package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"
)

type server struct {
	kind, dir, rtsp string
	limit           int64
	logger          *slog.Logger
	actions         sync.Mutex
	mu              sync.Mutex
	status          Status
	active          *job
	subs            map[chan packet]struct{}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func writeJSON(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}

func fail(w http.ResponseWriter, code int, message string) {
	writeJSON(w, code, map[string]string{"error": message})
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /media", s.list)
	mux.HandleFunc("POST /media", s.upload)
	mux.HandleFunc("GET /media/{id}", s.download)
	mux.HandleFunc("DELETE /media/{id}", s.remove)
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, s.snapshot()) })
	mux.HandleFunc("POST /control/{action}", s.control)
	if s.kind == "audio" {
		mux.HandleFunc("GET /ws", s.websocket)
	}
	return mux
}

func main() {
	kind := env("LAB_KIND", "audio")
	if kind != "audio" && kind != "video" {
		slog.Error("invalid LAB_KIND")
		os.Exit(1)
	}
	for _, binary := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(binary); err != nil {
			slog.Error("missing executable", "name", binary)
			os.Exit(1)
		}
	}
	dir, err := filepath.Abs(env("MEDIA_DIR", filepath.Join("storage", kind)))
	if err != nil {
		slog.Error("invalid storage path", "error", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		slog.Error("media storage", "error", err)
		os.Exit(1)
	}
	limit, err := strconv.ParseInt(env("MAX_UPLOAD_BYTES", "536870912"), 10, 64)
	if err != nil || limit <= 0 {
		slog.Error("invalid MAX_UPLOAD_BYTES")
		os.Exit(1)
	}
	s := &server{kind: kind, dir: dir, rtsp: env("RTSP_URL", "rtsp://mediamtx:8554/live"), limit: limit,
		logger: slog.Default(), status: Status{State: "idle"}, subs: make(map[chan packet]struct{})}
	httpServer := &http.Server{Addr: env("HTTP_ADDR", ":8080"), Handler: s.routes(),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Minute, IdleTimeout: 60 * time.Second}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-ctx.Done()
		shutdown, finish := context.WithTimeout(context.Background(), 5*time.Second)
		defer finish()
		if httpServer.Shutdown(shutdown) != nil {
			_ = httpServer.Close()
		}
	}()
	s.logger.Info("media controller listening", "kind", kind, "addr", httpServer.Addr)
	err = httpServer.ListenAndServe()
	cancel()
	<-done
	s.actions.Lock()
	s.stop()
	s.actions.Unlock()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("HTTP server", "error", err)
		os.Exit(1)
	}
}
