// Package webapp serves the dayzctl web UI and its JSON API.
//
// The API is deliberately small: list instances, list an instance's log files,
// and analyse a selection of them. All parsing happens server-side in
// internal/logs, so the browser only renders the report it receives.
package webapp

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/dayzctl/dayzctl/internal/config"
	"github.com/dayzctl/dayzctl/internal/logger"
	"github.com/dayzctl/dayzctl/internal/logs"
	"github.com/dayzctl/dayzctl/internal/version"
	webassets "github.com/dayzctl/dayzctl/web"
)

// Server bundles the HTTP handler with its dependencies.
type Server struct {
	cfg     *config.ServerConfig
	service *logs.Service
	handler http.Handler
}

// New builds the handler tree for the given configuration.
func New(cfg *config.ServerConfig) (*Server, error) {
	if cfg == nil {
		return nil, errors.New("config not loaded")
	}

	s := &Server{
		cfg:     cfg,
		service: logs.NewService(cfg.GetInstallDir(), logs.DefaultOptions()),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/instances", s.handleInstances)
	mux.HandleFunc("GET /api/instances/{instance}/logs", s.handleLogs)
	mux.HandleFunc("POST /api/instances/{instance}/analyze", s.handleAnalyze)

	ui, err := fs.Sub(webassets.Assets, "log-analyzer")
	if err != nil {
		return nil, err
	}
	mux.Handle("GET /", noCacheHTML(http.FileServerFS(ui)))

	s.handler = withLogging(withGzip(mux))
	return s, nil
}

// Handler returns the root http.Handler.
func (s *Server) Handler() http.Handler { return s.handler }

// Service exposes the log analysis service (used by tests and the CLI).
func (s *Server) Service() *logs.Service { return s.service }

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

type healthResponse struct {
	Status    string `json:"status"`
	Version   string `json:"version"`
	Time      string `json:"time"`
	Instances int    `json:"instances"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{
		Status:    "ok",
		Version:   version.Version,
		Time:      time.Now().UTC().Format(time.RFC3339),
		Instances: len(s.cfg.Instances),
	})
}

type instanceView struct {
	Name        string     `json:"name"`
	Port        int        `json:"port"`
	Enabled     bool       `json:"enabled"`
	ProfilesDir string     `json:"profilesDir"`
	LogFiles    int        `json:"logFiles"`
	Latest      *time.Time `json:"latest,omitempty"`
	Error       string     `json:"error,omitempty"`
}

func (s *Server) handleInstances(w http.ResponseWriter, r *http.Request) {
	views := make([]instanceView, 0, len(s.cfg.Instances))
	for i := range s.cfg.Instances {
		inst := &s.cfg.Instances[i]
		view := instanceView{
			Name:        inst.Name,
			Port:        inst.Port,
			Enabled:     inst.Enabled,
			ProfilesDir: s.service.ProfilesDir(inst.Name),
		}
		if files, err := s.service.ListFiles(inst.Name); err == nil {
			view.LogFiles = len(files)
			if len(files) > 0 {
				latest := files[0].ModTime
				view.Latest = &latest
			}
		} else {
			view.Error = err.Error()
		}
		views = append(views, view)
	}

	writeJSON(w, http.StatusOK, map[string]any{"instances": views})
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	instance := r.PathValue("instance")
	if !s.instanceExists(instance) {
		writeError(w, http.StatusNotFound, "instance not found: "+instance)
		return
	}

	files, err := s.service.ListFiles(instance)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"instance":    instance,
		"profilesDir": s.service.ProfilesDir(instance),
		"files":       files,
	})
}

type analyzeRequest struct {
	Files []string `json:"files"`
}

func (s *Server) handleAnalyze(w http.ResponseWriter, r *http.Request) {
	instance := r.PathValue("instance")
	if !s.instanceExists(instance) {
		writeError(w, http.StatusNotFound, "instance not found: "+instance)
		return
	}

	var req analyzeRequest
	if r.Body != nil {
		err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req)
		// An absent body simply means "analyse everything".
		if err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}
	}

	report, err := s.service.Analyze(instance, req.Files)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	logger.Debug("Analysis served",
		"instance", instance,
		"files", len(report.Files),
		"cached", report.FromCache)

	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) instanceExists(name string) bool {
	for i := range s.cfg.Instances {
		if s.cfg.Instances[i].Name == name {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		logger.Error("Failed to write JSON response", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// noCacheHTML stops browsers from serving a stale index.html after an upgrade.
func noCacheHTML(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/") || strings.HasSuffix(r.URL.Path, ".html") {
			w.Header().Set("Cache-Control", "no-cache")
		}
		next.ServeHTTP(w, r)
	})
}

// withGzip compresses responses; a full analysis can be several megabytes.
func withGzip(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		gz := gzip.NewWriter(w)
		defer gz.Close()
		next.ServeHTTP(&gzipResponseWriter{ResponseWriter: w, Writer: gz}, r)
	})
}

type gzipResponseWriter struct {
	http.ResponseWriter
	io.Writer
}

func (g *gzipResponseWriter) Write(b []byte) (int, error) { return g.Writer.Write(b) }

func (g *gzipResponseWriter) WriteHeader(status int) {
	g.Header().Del("Content-Length")
	g.ResponseWriter.WriteHeader(status)
}

// withLogging records request outcomes at debug level and failures at error.
func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		entry := logger.Debug
		if rec.status >= http.StatusInternalServerError {
			entry = logger.Error
		}
		entry("HTTP request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration", time.Since(started).String())
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(status int) {
	s.status = status
	s.ResponseWriter.WriteHeader(status)
}

func (s *statusRecorder) Write(b []byte) (int, error) { return s.ResponseWriter.Write(b) }
